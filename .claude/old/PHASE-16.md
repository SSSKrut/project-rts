# Phase 16 - рабочий план

Большая фаза, три tracks:

- **16.A - Asset pipeline + GLB import.** AssetRegistry foundation, .glb loader, .bplan binary cache, BuildingPlan source switch от хардкода к файлам.
- **16.B - Building 2.0 runtime.** Multi-chunk buildings (снимаем Phase 5 P-deferred), per-floor / per-zone CoverMap + ShootingArc, OrderKindClearBuilding, zones (inside-floor subdivision).
- **16.C - Interior UX.** BuildingViewMode (Sims cutaway), per-building floor chips widget, FloorVisibility (per-floor fog-of-war).

Tracks независимы по коду. 16.B/16.C можно стартовать на текущих процедурных зданиях (walls / floors / stairs / doors / windows как отдельные entities уже есть). Когда 16.A landed, .glb-loaded buildings подменяют BuildingPlanList без изменений в 16.B/16.C readers.

Цель фазы: после Phase 15 тактический ИИ и UI готовы; здания становятся осмысленным игровым пространством, а не процедурными коробками. Игрок:
- Видит здания как загружаемые модели (не identkit boxes).
- Может зайти юнитами в дом, увидеть intereior, отдать ClearBuilding для зачистки.
- Понимает что внутри здания через cutaway + per-floor fog (что разведано, что нет).
- Размещение cover slots на каждом этаже корректно (стрелять из окна 3-го этажа = реальный slot с реальным fire arc).

Без 16.A любая дальнейшая работа с моделями (Phase 17 unit/prop models) тоже застрянет - asset pipeline foundation общий.

ROADMAP §16 - порядок фаз и зависимости. CLAUDE.md "Building lifecycle" - текущая архитектура (procedural). GAMEDESIGN §1 (buildings as Layer 0 entities), §3 (Garrison/ClearBuilding orders), §9 (interior cover slots). UI.md §4 (cutaway view).

---

## Track 16.A - Asset pipeline + GLB import

### Решения, которые лочим

**A-P1. AssetRegistry - ECS resource, hash-keyed.**

```go
type AssetID uint64  // FNV-1a hash of relative path

type AssetRegistry struct {
    Models   map[AssetID]*ModelRecord  // .glb meshes -> rl.Model
    Textures map[AssetID]*TextureRecord
    Audio    map[AssetID]*AudioRecord  // Phase 25 wires
    Root     string                    // "./assets"
}

type ModelRecord struct {
    Model      rl.Model
    LoadedAt   float32
    Refcount   int32
    SourcePath string
}
```

Lazy load: `Registry.LoadModel(path) -> AssetID` reads from disk on first call, caches; later calls return cached AssetID. Refcount tracked but unload is deferred to Phase 25 (long-running session memory polish).

**Why hash-keyed:** path strings are ergonomic to author (`assets/buildings/barracks_01.glb`), but slow to compare and copy in hot paths. Hash to `uint64` once at load; gameplay code passes `AssetID`.

**Sync first.** Phase 16 starts synchronous - `LoadModel` blocks until decode finishes. Mission start spawns 5-20 buildings, total .glb size ~few MB - sub-second budget acceptable for prototype. Async pipeline (background goroutine + main-thread upload) lands in Phase 25 when streaming chunks need it.

**A-P2. Building .glb import - prefix-based mesh classification.**

One .glb = one BuildingPlan. Parser walks the scene tree, classifies meshes by name prefix:

```
level_<name>           -> invisible volume marker (AABB3D from mesh bounds)
floor_<name>           -> visual walkable plane; auto-assoc to level by XZ containment
wallext_<name>         -> external WallSegment (load-bearing, hidden in CameraFacing cutaway)
wallint_<name>         -> internal partition WallSegment (always visible inside cutaway)
wall_<name>            -> alias to wallext_ (simple .glb)
door_<name>            -> opening on host wall (Door)
door_<A>-<B>_<name>    -> opening + LevelTransition between level A and level B
window_<name>          -> opening on host wall (Window)
stairs_<name>          -> visual stairs mesh; gameplay defined by annotations (A-P2.5)
furniture_<kind>_<name>-> Prop with PropMeta[kind] (sandbags / table / crate / ...)
roof_<name>            -> Roof (skipped in InteriorOpen cutaway)
marker_<kind>_<name>   -> point-of-interest (spawn / capture / sniperperch / ...)
```

**Normalisation rules** (parser pre-pass on every mesh name):
- Lowercase.
- Strip Blender duplicate suffix: trailing `.NNN` (three digits) → drop. `wall_north.001` ⇒ `wall_north`.
- Strip LOD suffix: `_LOD0` aliased to no suffix; `_LOD<N>` for `N > 0` → skip mesh entirely.
- Split on first `_` to extract category; subsequent `_` segments are the entity's name + (optional) annotation tail (A-P2.5).
- Annotation tail starts at first `.` after category: `stairs_main.wp0.lobby` ⇒ category `stairs`, name `main`, annotations `[wp0, lobby]`.

Output: a `BuildingPlan` with `Footprint`, `Yaw`, `WallSegments[]`, `Floors[]`, `Stairs[]`, `Doors[]`, `Windows[]`, `Furniture[]`, `Levels[]`, `LevelTransitions[]`, `Markers[]`. The existing `Floor.Story uint8` field is derived (avgY-sorted index over `Levels[]`); `Stories` count on `Building` becomes `len(Levels)`.

**Why prefix-based:** simple to author in Blender ("name the mesh wall_north", done), simple to parse (no .json sidecar), survives Blender re-export without metadata loss.

**A-P2.5. Annotations: waypoints, slots, level anchors.**

Geometry-only naming is not enough for multi-segment stairs (Г-образные, switchback, спираль), large windows with multiple shooting positions, или линии мешков с N cover slots. Annotation pattern adds typed POI points layered on top of the visual mesh.

Annotations are **1-cm placeholder cubes** in Blender — visible to the artist for editing, not rendered in-game (parser strips by suffix match, never emits as render entity). Position is what matters; geometry is ignored.

```
<category>_<name>                -> primary visual mesh
<category>_<name>.wp<N>          -> sequential waypoint N (stairs / passages)
<category>_<name>.wp<N>.<level>  -> waypoint N anchored to level <level>
<category>_<name>.slot<N>        -> non-sequential cover/shooting slot N (windows / furniture)
<category>_<name>.entry          -> default entry POI (doors, levels)
<category>_<name>.center         -> explicit centre override (default = bbox centre)
```

Annotations are scoped to their parent — `stairs_main.wp0` is part of `stairs_main`, not a separate stair. Parser groups by `<category>_<name>` first, then collects all `.<suffix>` children.

**Stairs / passages: waypoint chain.**
- `stairs_<name>.wp<N>` defines the sequential walking path, N starting at 0.
- `stairs_<name>.wp<N>.<level>` anchors a waypoint to a level — entry / exit points connecting the staircase to a LevelNavGrid via a TransitionEdge.
- Between adjacent waypoints NavService emits internal `NodeStairWp` edges (Cost = walkCost × dist).
- Horizontal passages between two levels at the same Y use the same mechanism (no Y-delta required) — `stairs_<name>` is a misnomer for tunnels / bridges, but the data shape is identical. Author can rename the visual mesh to `passage_<name>` and the parser treats it the same (alias).
- **Default fallback** (no `.wp` annotations) — parser takes BBox vertical span: bottom Y → wp0 anchored to level containing that point, top Y → wp1 anchored to level containing that point. Works for simple straight flights.

**Windows: shooting slot list.**
- `window_<name>.slot<N>` → one `CoverSlot` entity with `HostKind=Window`, shared `Host=window`, `OriginDir=window.normal`, position per slot.
- N slots = N independent firing positions along the window.
- **Default fallback** (no `.slot`) — 1 slot at window centre. Backwards-compatible with small placeholder windows.

**Furniture: cover slot list.**
- `furniture_<kind>_<name>.slot<N>` → one `CoverSlot` per slot, `HostKind=CoverHostProp`. OriginDir = (slotPos - furniture.bboxCenter) projected to XZ, normalised — "facing outward from cover".
- **Default fallback** (no `.slot`) — current 8-radial `propCoverSlots` generator (Phase 6) keyed on `PropTypeRegistry[kind]`. Preserves placeholder behaviour.

**Doors: optional entry / exit anchors.**
- `door_<A>-<B>_<name>.outside` / `.inside` — explicit rally points before / after the door for ClearBuilding / Garrison entry. Default — door centre on each side.

**A-P2.6. Level volumes - bbox parsing and auto-association.**

A `level_<name>` mesh is a marker volume. Parser reads its **axis-aligned bbox** (AABB3D from mesh bounds) and emits one `Level` entity with that bbox + the parsed name. Mesh geometry itself is discarded (artist can use any shape; bbox is what counts).

**Auto-association of children to levels.** Every classified entity is geometrically assigned to a level after parse:

| Entity        | Containment rule                                                |
|---------------|-----------------------------------------------------------------|
| `floor_*`     | XZ-bbox must lie fully inside ONE level's XZ-bbox; Y close to level.midY. |
| `wallext_*` / `wallint_*` | XZ-segment intersects level XZ; Y-range overlaps level Y-range. Multi-level allowed (wall spans floors). |
| `door_*` / `window_*` | Inherits assoc from host wall; if host wall spans multiple levels, opening's Y picks the level. |
| `furniture_*` | XZ-centre inside ONE level XZ; Y close to floor of that level.  |
| `stairs_*`    | Not assoc'd to a single level — waypoints carry per-wp level anchors. |
| `roof_*`      | Not assoc'd; floats above the level stack.                      |
| `marker_*`    | XZ-centre inside ONE level XZ.                                  |

**Multi-level entities allowed for walls only** (a 2-storey external wall belongs to both levels). All other categories must lie within ONE level; **straddle = parse error** with warning, artist must split or re-position.

**Floor → level inference order.** Inside the parser:
1. First pass — collect all `level_<name>` volumes.
2. Second pass — classify other meshes, run containment against levels, assign.
3. Third pass — annotations (waypoints, slots) attach to their parent by name prefix match, position is absolute world XYZ.
4. Validation — every floor / furniture / opening must resolve to exactly one level; walls to >= 1.

**Story index derivation.** For UI ordering (cutaway chip row, Inspector display), levels are sorted by avgY ascending. Tied levels (same Y, different XZ — like two wings of a ground floor) get the same story index but separate chips. Manifest.json may override sort with explicit `displayOrder`.

**A-P4. .bplan binary cache.**

`.glb` is heavy to parse (raylib's GLTF loader walks JSON + binary chunks per call). On second startup, the loader checks for a sibling `.bplan` (binary BuildingPlan dump, magic `BPLN`, version v1). Hit -> deserialize directly. Miss or version mismatch or .glb mtime newer -> re-parse .glb, write .bplan.

Format v1:
```
magic[4]="BPLN" | version uint16 | flags uint16 |
yaw float32 | footprint AABB2D |
nLevels uint16  | levels[]   (name + AABB3D + displayOrder)
nWalls uint16   | walls[]    (segment + opening + levelRefs)
nFloors uint16  | floors[]   (AABB2D + Y + levelRef)
nStairs uint16  | stairs[]   (waypoints[] + level anchors[])
nProps uint16   | props[]    (kind + pos + slots[])
nMarkers uint16 | markers[]  (kind + pos)
nTransitions uint16 | transitions[]  (levelA + levelB + via-door entity)
```

Sibling location: same dir as the .glb, `*.bplan`. Atomic write via `.tmp + rename`. Gitignored (derivable).

**A-P5. Asset directory + manifest.**

`./assets/buildings/` contains one or more .glb files. A `manifest.json` lists which buildings are spawnable (`Kind`, `DisplayName`, asset path). `main.go` reads the manifest at startup, replaces `makeStartingBuildings()` (hardcoded list) with manifest-driven spawn list per scene.

For Phase 16 we keep `makeStartingBuildings()` as fallback if `assets/` is missing - lets headless tests / CI run without art assets.

---

### Милстоуны 16.A

**M16.A.0 - AssetRegistry resource + ModelRecord lifecycle.**

`components/asset.go`: `AssetID`, `AssetRegistry`, `ModelRecord`. `LoadModel(path) -> AssetID` reads .glb via `rl.LoadModel`, caches. Unload deferred. Smoke: load one .glb in main.go startup, log hash + mesh count.

**M16.A.1 - .glb scene-tree walker + mesh classifier.**

`systems/glb_import.go`: `LoadBuildingPlan(path) -> *components.BuildingPlan`. Walks `rl.Model.Meshes` + names, classifies by prefix. Emits walls / floors / stairs / openings. One test .glb in `assets/buildings/test_barracks.glb` (placeholder authored in Blender).

**M16.A.2 - .bplan binary cache.**

`systems/bplan_persistence.go`: `WriteBuildingPlan(path, plan)`, `ReadBuildingPlan(path) -> (*BuildingPlan, error)`. Cache hit gate in `LoadBuildingPlan` (M16.A.1). mtime check vs source .glb.

**M16.A.3 - Level volumes + annotation parser.**

Extend M16.A.1: pick up `level_<name>` bbox markers, populate `BuildingPlan.Levels`. Run containment pass for auto-association of floors / walls / openings / furniture to levels (A-P2.6). Annotation harvester: collect `.wp` / `.slot` / level-anchor markers per parent entity (A-P2.5). `door_<A>-<B>` parser records `LevelTransition`. Validation pass — emit warning + skip on straddle / unresolved annotation parent.

**M16.A.4 - Manifest + fallback.**

`assets/buildings/manifest.json` reader. `main.go` integration: prefer manifest if present, fallback to `makeStartingBuildings()` for headless / missing-assets builds.

---

## Track 16.B - Building 2.0 runtime

### Решения, которые лочим

**B-P1. Multi-chunk buildings - сняли Phase 5 P-deferred.**

Phase 5 закрепил "footprint must lie inside one chunk". Phase 16.B снимает constraint. Реализация:

- `BuildingChildIndex` уже root-keyed (children группируются по building root, не по chunk).
- `TerrainStreamingSystem.evict` despawn'ает только тех children, чей `Pos.Chunk` совпадает с evicted chunk - остальные остаются живыми. Building root carries `AlwaysActive` (уже так).
- Re-spawn: per-chunk pass в `BuildingSystem` (existing `BuildingsProcessed` marker) расширяется на "spawn children whose Local position falls in this chunk only". Builder layout продолжает работать с world-space coords; вычисляем target chunk для каждой стены/двери/пола перед spawn.
- Bunker `RectCut` (`BuildingTerrainProcessed`) also per-chunk - cuts only the strip inside the current chunk's footprint.

Footprint can cross chunk boundary. Children, по факту, могут жить в любом из чанков, перекрытых footprint'ом. Eviction одного из них не уничтожает building.

**B-P2. Per-level CoverMap + ShootingArc.**

Phase 6 SpatialBakeSystem уже строит `NavGrid` + `FloorNavGrid` per floor. С переходом на levels (A-P2) FloorNavGrid становится `LevelNavGrid` — та же сетка, привязана к Level entity, размер = level XZ bbox, Y берётся от level.midY (или от первичного floor patch внутри level).

Phase 16.B расширяет:

- `LevelCoverMap` структура аналогично NavGrid — сетка cells level XZ bbox, каждая клетка несёт `CoverDistance` к ближайшему cover host на этом level.
- Cover slots для walls / windows / furniture per level: `CoverSlot.HostLevel ecs.Entity` field добавляется, slot generator берёт level Y.
- Multi-level walls (1 wall, 2 levels) — slot generation per level отдельная, slot Y соответствует level Y.
- ShootingArc per window: arc остаётся как Phase 5 wrote; reader (WeaponSystem sector gate из Phase 15 M15.A.4) extended на per-window slot check.

Cost: per-level map = SizeX*SizeZ uint8 cells (~1 KB на средний level). Здание с 6 levels — ~6 KB. Negligible.

**B-P3. OrderKindClearBuilding.**

New order kind. Spec:
```
Code     = OrderKindClearBuilding
Name     = "Clear"
DrivesMacroPath = true
ArrivalRadius   = 1.0  // entry-door arrival
CompletionRule  = ClearBuildingCompletion
OverridesHoldFire = true  // allows fire even when squad RoE is HoldFire
```

CompletionRule: enumerate building levels, sweep one at a time (enter level N, neutralize hostile units inside, mark level Cleared, proceed). Squad members fan out within level via formation; AttackTarget overrides on visible hostiles inside the building.

Phase 16.B simple: linear sweep по `Levels[]`, sorted by avgY ascending (ground first, then up). Reverse-engineered priority (rooms with hostiles first) - Phase 18+. Multi-wing topology (level graph with horizontal passages) — sweep всё равно по avgY, в пределах одного Y wing-by-wing alphabetically.

Completion: all levels marked `Cleared` AND no hostile alive within footprint. Failure: every squad member dead.

**B-P4. ClearBuilding via TransitionRegistry.**

`NavService.FindPath` already routes through `TransitionEdge` (Phase 6/7). ClearBuilding squadMacroPath replans per-level: each level has a "center" anchor (level bbox centre или explicit `.center` annotation), A* path from squad center to anchor. Resolver re-targets to next level on completion.

Per-level hostile check: walk units within level bbox + faction != self. Stable per-tick (cached via Vision Awareness).

**B-P5. Garrison не меняется.**

Garrison (existing OrderKindGarrison) ставит юнитов на window slots — покрывает full building, не один level. ClearBuilding — "сначала разобраться внутри", Garrison — "уже наш, расставить по окнам". Two distinct intents, two distinct kinds.

ClearBuilding can chain into Garrison via Shift+RMB (chained order): "Clear, then Garrison".

---

### Милстоуны 16.B

**M16.B.0 - Multi-chunk buildings.**

Снимаем Phase 5 constraint. `BuildingSystem` children-spawn pass extends на per-child target-chunk computation. `TerrainStreamingSystem.evict` despawn'ает только children в evicted chunk (root + cross-chunk children survive). Test: построить building с footprint крест-накрест двух чанков, anchor walk across boundary, evict оригинальный chunk, building стоит.

**M16.B.1 - Per-level CoverMap + LevelNavGrid rename.**

Rename `FloorNavGrid` → `LevelNavGrid` (component, marker, registry refs). Add `LevelCoverMap` keyed by Level entity. `SpatialBakeSystem` pass per level: rasterize walls / windows / furniture inside level bbox, fill CoverDistance. Marker `LevelCoverBaked`. Annotation-derived slots (`.slot<N>` from M16.A.3) already carry level reference; per-level CoverMap just consumes existing slot positions.

**M16.B.2 - OrderKindClearBuilding spec + resolver.**

Spec table entry (Phase 14.5 pattern). `OrderResolverSystem.evaluateCompletion` extended с `ClearBuildingCompletion` (level sweep). UI: pie-menu adds "Clear" option when hovering hostile building.

**M16.B.3 - Level navigation.**

`LevelTransition` resource readers в NavService — multi-graph topology с doors как edges между LevelNavGrids и stairs как waypoint chains соединяющими произвольные пары levels. Path planning per-level (squad walks level[0] center, then level[1] etc.). Hostile-inside-level detection via Faction filter against level bbox.

**M16.B.4 - ShootingArc reader.**

WeaponSystem sector gate (Phase 15 M15.A.4) extended: когда shooter стоит на window slot, prefer-target gate включает window's ShootingArc. Targets outside arc filtered. Skeleton: just the gate; full angle UI deferred.

---

## Track 16.C - Interior UX

### Решения, которые лочим

**C-P1. BuildingViewMode component.**

```go
type WallRenderMode uint8
const (
    WallRenderAll          WallRenderMode = iota // default - all walls solid
    WallRenderCameraFacing                       // walls facing camera become semi-transparent
    WallRenderWireframe                          // all walls -> wireframe outline
)

type BuildingViewMode struct {
    InteriorOpen bool        // cutaway active?
    CurrentLevel ecs.Entity  // which Level entity is "selected" for chip highlight + hide-above
    WallMode     WallRenderMode
}
```

Component lives on building root entity. Default: `InteriorOpen=false, CurrentLevel=ecs.Entity{} (resolves to lowest-Y level on render), WallMode=WallRenderAll`. Switched via the per-building widget (C-P2).

Renderer reads BuildingViewMode per building, applies:
- Hide every `Level` whose `avgY > CurrentLevel.avgY + epsilon` when `InteriorOpen` true (epsilon ~ 0.1 m to keep multi-wing levels at same Y visible).
- Hide the ceiling of `CurrentLevel` (= floor mesh of the level directly above on the same XZ column) so the player sees in.
- For external walls (`wallext_*`) of `CurrentLevel`: apply `WallMode` (alpha lerp / wireframe). Internal partitions (`wallint_*`) always render solid inside the cutaway.
- For walls of lower (still-visible) levels: render normally.

`epsilon` matters because multi-wing levels share avgY but stand in different XZ. Hiding "everything above CurrentLevel" would also hide the adjacent wing — epsilon keeps them visible.

**C-P2. Per-building floor-chip widget.**

Screen-projected widget rendered next to selected/hovered building. Layout:
```
[1] [2] [3]   <- floor chips (1 per story)
[W][C][X]     <- wall mode toggles (All / Camera / Wireframe)
[Inside]      <- toggle InteriorOpen
```

Chips clickable via panel-local cursor + LMB pressed (same pattern as Inspector chips). Widget shows only when player selected (or hovered with squad selected) a building; auto-hides otherwise.

Widget position: project building root WorldPos to screen, add fixed offset (top-right of building bbox).

**C-P3. FloorVisibility - per-floor / per-zone fog-of-war.**

```go
type LevelVisibility struct {
    Discovered bool    // friendly unit ever set foot in this level?
    LastSeenAt float32 // session-time of last friendly presence
}
```

Component lives on Level entity. Updated by VisionSystem extension: per-tick, for each friendly unit inside a level bbox (XZ + Y range), set Discovered + LastSeenAt.

Renderer policy:
- **Static layout** (walls, doors, windows, stairs, room shape) - always rendered for any building player can see (= within view distance). Player knows where rooms are even before entering.
- **Dynamic contents** (furniture, enemy units, door open/closed state) - rendered only when `Discovered=true` AND `now - LastSeenAt < FogVisibleDuration` (e.g. 30 s).
- **Hidden contents** when not yet discovered or last seen >30s ago - render as silhouette (50% alpha grayscale).

Furniture (props inside buildings) - hidden until first friendly visit. Enemy units inside undiscovered rooms - culled from rendering.

**C-P4. Cutaway auto-trigger on Garrison.**

When player issues Garrison or ClearBuilding to a building, `BuildingViewMode.InteriorOpen` auto-flips to true for that building, `CurrentLevel` snaps to the level containing the most squad members. Player can override via the widget.

Auto-clears when no friendly unit is inside the building anymore (timed: 5s grace after last unit leaves).

**C-P5. Interior selection.**

LMB-click on a unit inside an interior cutaway-mode building works as normal selection. Wall-cull doesn't break the unit-hit raycast because picking uses ECS WorldPos vs ray, not depth-buffer.

Marquee selection inside building — works (raycast against ground plane = floor.Y of the currently selected level).

---

### Милстоуны 16.C

**M16.C.0 - BuildingViewMode + render gate.**

Component added. Building render pass reads BuildingViewMode per building root, hides upper floors / ceilings when `InteriorOpen=true`. Default: no cutaway anywhere; gate just plumbed.

**M16.C.1 - Level-chip widget.**

`ui/building_widget.go`: screen-projected widget. Level chips (one per Level entity, sorted by avgY ascending; same-avgY levels share a chip row labelled with the level name suffix), wall-mode toggles, InteriorOpen toggle. Click handlers mutate building's `BuildingViewMode.CurrentLevel`. Shows only when building selected or hovered.

**M16.C.2 - LevelVisibility + fog render.**

Component on Level. VisionSystem pass: friendly unit inside level bbox -> Discovered=true + LastSeenAt=now. Renderer gates furniture / enemy units by LevelVisibility on their host level.

**M16.C.3 - Auto-trigger Garrison cutaway.**

Garrison / ClearBuilding order resolver flips `BuildingViewMode.InteriorOpen` on target building. Auto-revert 5s after last friendly leaves footprint.

**M16.C.4 - Wall-mode polish.**

CameraFacing wall mode: per-tick compute dot(wall.Normal, cameraForward); above threshold -> alpha=0.3. Wireframe mode: replace wall mesh with outline pass.

---

## Что считаем "закрытием Phase 16"

- **16.A** - Один тестовый .glb loaded as a building, BuildingPlan source switchable между manifest и hardcoded fallback, .bplan cache hit on second startup, level volumes + annotation parser populates Levels / waypoints / slots, LevelTransition resource live.
- **16.B** - Footprint спокойно пересекает chunk boundary без потери children. Per-level CoverMap baked (FloorNavGrid renamed to LevelNavGrid). ClearBuilding order работает на multi-level test scene (включая горизонтальный passage между levels на одном Y). ShootingArc filter живой для window slots.
- **16.C** - Building widget (level chips + wall-mode + cutaway toggle) функционирует на multi-wing здании. Per-level fog-of-war скрывает enemy units внутри неразведанных rooms. Garrison auto-flips InteriorOpen.
- ROADMAP updated. PHASE-16.md -> `.claude/old/`.
- Documentation: CLAUDE.md "Building lifecycle" obsolete статья переписана (Floor → Level, FloorNavGrid → LevelNavGrid, story integer → level graph); добавлен раздел "Asset pipeline" с AssetRegistry contract.

После - Phase 16.5 (Building generator + sandbox) → Phase 17 (Unit / prop models + texture pipeline + lighting + road/river splines).

---

## Заметки на полях

- **Track ordering.** 16.A foundation - чем раньше тем лучше для 16.B/16.C тестов на реальных моделях. Но 16.B/16.C могут стартовать на текущих процедурных зданиях; switch к .glb в конце фазы. Параллельная работа возможна, если есть placeholder .glb в репо.

- **Procedural buildings deprecated, не deleted.** `generateBuildingLayout` остаётся как fallback. Headless tests гарантируют что путь без assets/ работает. Если когда-нибудь захочется снова procedurally-generated buildings (e.g. random infinite city), функция жива.

- **GLTF / GLB choice.** raylib-go supports .glb через `rl.LoadModel`. .glb binary - один файл, проще shipping. Phase 16 sticks with .glb; .gltf + sidecar bin - не нужен (один файл = одно здание).

- **Asset hot-reload deferred.** Watching .glb mtime + auto-reload would be nice for iteration, but adds goroutine + file-system watcher. Phase 25 polish; until then restart game на .glb change.

- **Multi-chunk LOD.** Building root carries `AlwaysActive`. Children inherit LOD from their host chunk - if chunk evicted, children disappear. Phase 16.B keeps это поведение: children в evicted chunk gone, в loaded chunks alive. Building looks like "half of it disappeared" if player walks far enough. Acceptable - same trade-off as anywhere else with chunk eviction. Phase 25 polish может добавить full-LOD-keep для buildings (high LOD pass).

- **Cover slot generation per level.** Phase 6 SpatialBakeSystem already iterates Floor entities for FloorNavGrid bake; refactor that loop to iterate Level entities for LevelNavGrid + LevelCoverMap. Cost is linear in level count; building с 6 levels (3 floors x 2 wings) — 6 bakes. Acceptable.

- **OrderKindClearBuilding completion**: для skeleton, "no hostile alive within footprint" check может прямо использовать Faction filter + bbox test. Не нужен per-level hostile tracking в первой версии. Refinement к Phase 18 (proper room-by-room).

- **LevelVisibility и rendering pipeline.** Renderer уже знает per-entity LOD. LevelVisibility - new gating bit поверх. Hidden-but-static = render with `tintAlpha=0.5 + grayscale`, hidden-and-dynamic = skip entirely.

- **Annotations как placeholder cubes.** raylib-go `rl.LoadModel` skips GLTF nodes without mesh data (true empties). Annotations используют 1-cm placeholder cubes, чтобы попасть в `model.Meshes[]`. Renderer skip'ает любую mesh у которой name содержит `.wp` / `.slot` / `.center` / `.entry` / `.outside` / `.inside`. Trade-off — пара десятков лишних vertices в .glb. Альтернатива (qmuntal/gltf parallel parser для node tree) — Phase 25 polish.

- **Asset pipeline для другой geometry (props / units).** AssetRegistry в 16.A - общая foundation, не только buildings. Phase 17 Unit models используют тот же `LoadModel` API. .bplan специфичен для buildings; props / units нужен `.uplan` / `.pplan`? Скорее всего нет - props - один Mesh с registry-recorded AssetID, не сложная структура. Только buildings нужен binary cache из-за множества meshes + классификации.

- **CommitMove проблема пересечения чанков.** Multi-chunk building может крестить chunk boundary. Если building children spawn в разных чанках, и один из тех чанков evicted - children disappear. При re-load чанка children re-spawn через тот же BuildingSystem pass. Test scenario: player walks past building, walks back. Building should look identical.

- **Manifest format.** Простой JSON: `{"buildings": [{"kind": "barracks", "asset": "buildings/barracks_01.glb", "displayName": "Barracks"}]}`. Парсер на startup. Lobby scene picker (Phase 18+) reads тот же manifest.

---

## Открытые вопросы

1. **AssetID hash collision.** FNV-1a 64-bit на 1000 assets = collision probability ~10^-14. Acceptable. Если совпадёт - переименовать файл. Не блокирует фазу.

2. **GLTF coordinate convention.** raylib uses +Y up, +Z forward (right-handed). Blender export defaults match. Если кто-то экспортирует с +Z up - parser должен detect и rotate. Phase 16 skeleton: assume +Y up. Если поломается - добавить rotation flag в manifest.

3. **.bplan version migration.** Если v2 формат появится (новые поля, e.g. furniture orientation), readers v1 fail-safe: re-parse .glb. Без backward-compat в format - простой rebuild.

4. **Furniture как Prop или WallSegment?** Furniture стол - блокирует movement (как wall), даёт cover (как prop), не имеет ShootingArc (не window). Phase 16 simple: furniture = Prop with Cover>0 + BlocksMovement bit. Не отдельная категория. SpatialBakeSystem уже это поддерживает (PropMeta.BlocksMovement).

5. **ClearBuilding и civilians.** Если будут civilians (non-faction units) в зданиях - ClearBuilding должен их avoid'ить или нет? Phase 16 simple: no civilians yet. Phase 20+ (campaign content) добавит civilian faction; ClearBuilding ROE update by then.

6. **Per-level fog-of-war stale time.** 30s default. Слишком короткий - игрок постоянно теряет видимость; слишком долгий - нет смысла. 30s = "пара действий, потом нужно проверить". Tweakable.

7. **Cutaway widget vs Inspector.** Level chips - всегда attached к building в 3D scene, не в Inspector? Альтернатива - Inspector single-building view с chips. Phase 16 simple: 3D-attached widget (rendered рядом со зданием). Inspector single-building - read-only state, no widget. Complete reverse: Inspector hosts the chips, 3D widget shows current level as a glyph. TBD по first playtest.

8. **Procedural buildings retire date.** Phase 16 keeps как fallback. Когда retire? Phase 18 (UI L4) или позже когда хороший asset library есть. Не блокирует фазу.

9. **Asset pipeline для terrain textures.** Phase 17 нужно. AssetRegistry в 16.A покрывает Models + Textures (заглавно). Textures loader - Phase 17 responsibility, но slot в Registry уже есть.

10. **Inside-building unit speed/stance.** Внутри здания units должны crouch / slow? Phase 16 simple: no - units run at normal speed inside. Phase 17 polish может attach MovementProfile override per Level (Stealth doctrine inside).

11. **Level volume shape - AABB only?** Phase 16 lock: axis-aligned bbox from mesh bounds. L-shaped rooms split into two touching levels (`level_kitchen_main`, `level_kitchen_alcove`). Convex hull / oriented bbox - Phase 25 polish if real architecture demands it.

12. **Level display ordering rule.** avgY ascending + alphabetical tiebreak. Manifest may override via explicit `displayOrder`. Sufficient для multi-wing on single floor (alpha по wing name); недостаточно для зданий с visually irregular Y (mezzanines меж этажами). Phase 16 lives with the simple sort; manifest override covers edge cases.

13. **Multi-level wall slot generation.** Внешняя стена из 2 этажей даёт 2 set'а cover slots — один per level Y. Slot generator per level (Phase 6 SpatialBake refactor); wall.Y range пересечён с level.Y range определяет какой slot subset попадает в какой LevelCoverMap.

14. **Stairs vs Passage naming.** Phase 16 lock: только `stairs_<name>` категория для visual mesh; парсер не различает вертикальный staircase от горизонтального tunnel. Если артистам это режет глаз — добавим alias `passage_<name>` без поведенческих изменений (одинаковый parser path).
