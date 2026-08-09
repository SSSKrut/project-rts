# Phase 16.5 - рабочий план

Tooling-фаза между Phase 16 (Building 2.0 + Asset pipeline) и Phase 17 (Visual fidelity 1). Две задачи:

- **Track 16.5.A - Procedural building generator.** Программно эмитим `BuildingPlan` (Levels / Floors / Walls / Doors / Windows / Stairs / Furniture) с правильно расставленными annotations (waypoints, slots, level anchors, ShootingArc, CoverDirection). Тот же shape что выходит из Phase 16 .glb loader — generator подменяет artist-authored .glb для iteration без Blender'а.
- **Track 16.5.B - Standalone building sandbox.** Отдельный `cmd/building_sandbox/main.go` (по образцу `cmd/particle_sandbox`). Boots за <1 секунду, без terrain / units / orders. Пикeр generator-template + параметры (kind / seed / size / story count) + cutaway controls + annotation gizmos (waypoint spheres, slot flags, level bbox wireframes, ShootingArc cones).

Цели:

- **Test content без Blender.** Phase 16 loader работает, но настоящих .glb assets ещё нет. Generator даёт repeatable test scenes (multi-wing complex, switchback stairs, sandbag line) для отладки loader и runtime прямо сейчас.
- **Validation формата.** Generator пишет в тот же `BuildingPlan` shape что loader. Если loader корректен, generator-output и loader-output для эквивалентного .glb должны давать одинаковое поведение runtime. Round-trip опционально: generator → .bplan write → loader read → diff.
- **Iteration без перезагрузки игры.** Sandbox hot-swap generator parameters (resize, reseed, switch template) — артефакты, gameplay-effects (cover slots, ShootingArc cones, level cutaway) видны мгновенно. Полная игра грузится несколько секунд + нужен LOD + nav bake; sandbox пропускает всё, кроме того что относится к зданию.
- **Документация living.** Generator templates становятся примерами «как должно выглядеть» для будущих art assets. Артист открывает sandbox, видит как Compound выглядит, копирует структуру в Blender.

ROADMAP: phase inserted между 16 и 17, нумерация 16.5 (прецеденты 13.5, 13.6, 14.5-14.7). PHASE-16.md A-P2 (categories), A-P2.5 (annotations), A-P2.6 (level volumes) — генератор обязан соблюдать тот же контракт. CLAUDE.md "Building lifecycle" обновлённый после Phase 16 — генератор пишет в ту же модель.

---

## Track 16.5.A - Procedural building generator

### Решения, которые лочим

**A-P1. Templates and parameters.**

`systems/building_gen/` пакет (новый). Три initial templates:

- **House** (`HouseTemplate{Stories, FootprintXZ, RoofKind}`). 1-2 этажа, прямоугольный footprint, одна level-волюм per story, прямая лестница вдоль стены. Окна по периметру, дверь спереди. Test case: вертикальная многоэтажность с простой топологией.

- **Office** (`OfficeTemplate{Stories, FootprintXZ, RoomsPerFloor}`). 3-5 этажей. Каждый floor разбит на 2-4 levels (rooms / corridor). Лестница в центре, лифт-шахта (placeholder mesh, без gameplay). Cubicles внутри как furniture. Test case: multi-level on single Y + sequential floors.

- **Compound** (`CompoundTemplate{Wings, BasementDepth, ConnectorKind}`). Несколько корпусов (wings) одинаковой высоты, соединённых горизонтальным passage на ground level и подземным тоннелем. Test case: multi-connected level graph, horizontal stairs, basement.

Каждый template имеет:
- `Generate(seed uint64, params T) *components.BuildingPlan` — детерминированно из seed.
- `Validate(plan *BuildingPlan) []ValidationIssue` — sanity check после генерации (no straddling levels, every floor inside a level, doors connect existing levels, etc.).

Templates share helper toolkit: `gen.Wall(start, end, height, yaw)`, `gen.Door(wall, t, kind)`, `gen.Window(wall, t, w, h, bottom)`, `gen.LevelVolume(name, aabb)`, `gen.Stair(name, waypoints, anchors)`, `gen.Furniture(kind, pos, slots)`. Builder DSL не для размера; goal — каждый template читается как cookbook.

**A-P2. Annotation generation.**

Generator расставляет:
- `level_<name>` AABB3D — per level volume (kitchen / bedroom / corridor / wing_a).
- `floor_<name>` mesh placeholders (rendered как тонкая plate). Auto-assoc handled by parser-shape contract.
- Wall slots для cover (auto-derive: 8 slots radial per wall per level, like current procedural).
- Window slots: 1 default + 1 extra per 2.5m window width.
- Furniture slots: per-kind from `PropTypeRegistry` defaults; sandbags line spawns 3-5 slots along length.
- Stair waypoints: 3+ wp за прямую лестницу (start.level → mid → end.level); 5+ wp за switchback (start → landing → mid → landing → end).
- ShootingArc per window: arc=120 deg, yaw=wall normal.
- CoverDirection per wall: yaw=wall normal+180 (inward face provides cover).

Generator stays consistent with Phase 16 parser auto-defaults — если template не указал .slot, parser fills с радиальной generator'ом. Generator может явно эмитить slot positions если хочет особенного layout.

**A-P3. Output shapes.**

Generator emit'ит `*components.BuildingPlan`. Тот же путь что Phase 16 loader. main.go (полная игра) может использовать generator в `makeStartingBuildings()` fallback вместо хардкода — `gen.House(seed=42, ...)`.

Optional secondary output (M16.5.A.4) — write to `.bplan` binary. Round-trip test: generator → write bplan → loader read → diff. Validates Phase 16 format.

Optional tertiary output (defer to Phase 17 или manual artist task) — write to .glb so artist может open in Blender, edit, re-export. Phase 16.5 skeleton skips это.

**A-P4. Determinism.**

Generators must be **fully deterministic** given (template-type, seed, params). Random choices use `math/rand.New(rand.NewSource(seed))` локально, не global RNG. Two calls with same args yield identical plans. Test harness может snapshot a known plan and detect regressions.

**A-P5. Validation rules.**

`Validate(plan)` returns list of `ValidationIssue{Severity, Code, Message}`. Codes (initial):

- `LevelVolumeMissing` — entity без level assoc (straddling или вне всех levels).
- `LevelOverlap` — два level bbox пересекаются больше чем на epsilon (вероятно author bug).
- `OrphanDoor` — door without two-level reference.
- `StairWpUnanchored` — стartовая или конечная wp лестницы без `.level` anchor.
- `EmptyLevel` — level volume без floor mesh внутри.
- `SlotOutsideHost` — slot annotation за пределами host mesh bbox.

Severity: `Error` (loader rejects), `Warning` (loader accepts, logs). Sandbox displays issue list per loaded building.

---

### Милстоуны 16.5.A

**M16.5.A.0 - Builder DSL toolkit.**

`systems/building_gen/builder.go`: helper functions для wall / door / window / level / stair / furniture. Returns `*BuildingPlan` step-by-step. Smoke test: assemble 1-room building manually via builder calls, validate, confirm shape matches manual `BuildingPlan{...}` literal.

**M16.5.A.1 - HouseTemplate.**

`systems/building_gen/house.go`: `GenerateHouse(seed, HouseParams) *BuildingPlan`. 1-2 stories, 1 level per story, straight stair. Validator must pass.

**M16.5.A.2 - OfficeTemplate.**

`systems/building_gen/office.go`: `GenerateOffice(seed, OfficeParams) *BuildingPlan`. Multi-level per floor (rooms + corridor), central staircase. Test multi-level navigation through doors between rooms on same floor.

**M16.5.A.3 - CompoundTemplate.**

`systems/building_gen/compound.go`: `GenerateCompound(seed, CompoundParams) *BuildingPlan`. Multiple wings, horizontal passages between wings on ground level, optional basement tunnel. Test multi-connected level graph.

**M16.5.A.4 - .bplan round-trip.**

Hook generator output into Phase 16 .bplan persistence. `roundtrip := WriteBuildingPlan + ReadBuildingPlan`. Test: `assert.Equal(roundtrip(plan), plan)` for all three templates. Detects format coverage gaps.

**M16.5.A.5 - main.go integration.**

`makeStartingBuildings()` fallback recycled — when assets/manifest is missing, fallback может вызвать generator templates с фиксированными seeds. Дает working starting scene without art assets.

---

## Track 16.5.B - Standalone sandbox

### Решения, которые лочим

**B-P1. Sandbox scope.**

`cmd/building_sandbox/main.go` — отдельная `package main`, аналог `cmd/particle_sandbox`. Boots in <1s.

Не делает:
- Terrain (плоская grid plane вместо TerrainSystem).
- Units / squads / orders (нет ECS pipeline кроме того что строит building).
- LOD / streaming (одно здание в центре, всегда Active).

Делает:
- Load `*components.BuildingPlan` от одного из generator templates ИЛИ от .glb file (для loader testing).
- Spawn building через тот же `BuildingSystem` codepath что полная игра — same walls/floors/stairs/doors/windows entities.
- Render through `render_world` building draw routines (если возможно reuse без drag terrain) или встроенная sandbox-render copy.
- Per-building widget (BuildingViewMode from Phase 16 C-P1): cutaway, level chips, wall-mode.
- Annotation overlay (B-P2 ниже).
- Template picker UI (B-P3).

**B-P2. Annotation gizmos overlay.**

Hotkey toggles (similar to existing N/C/V/F/Y debug overlays in полная игра):

- **L** - level bbox wireframes (different colour per level, label = level name).
- **W** - waypoint chain dots + arrow lines along stair waypoints.
- **S** - cover/shooting slot flags (small triangle pointing along OriginDir).
- **A** - ShootingArc cones (semi-transparent fan from window outward).
- **C** - CoverDirection arrows per wall (outward normal).
- **D** - doors as green/red discs (open / closed).
- **F** - furniture bbox wireframes + kind label.
- **V** - LevelVisibility tint (discovered = full colour, undiscovered = grey).
- **G** - generator validation issues — overlay rect + text near offending entity.

All overlays cumulative; hotkey toggles per-overlay. Default state: levels + waypoints + slots ON, остальное OFF.

**B-P3. Generator picker UI.**

Simple immediate-mode UI (re-use Inspector chip rendering):

```
+-----------------------------------+
| Generator                         |
| [House] [Office] [Compound]       |
|                                   |
| Seed: [12345]   [-][+]            |
|                                   |
| Stories: [3]    [-][+]            |
| ... per-template params           |
|                                   |
| [Regenerate]   [Load .glb...]     |
+-----------------------------------+
```

Sits in top-left corner of sandbox. `[Regenerate]` re-runs template с current params + reseeds entities. `[Load .glb...]` opens file picker (raylib `rl.GuiFileDialog` или simple path text input — Phase 16.5 simple: hardcoded path with text edit).

**B-P4. Camera + scene controls.**

Same orbit camera shell as particle sandbox: RMB-drag orbit, wheel zoom, optional WASD pan. Camera target — building root.

Hotkey `R` resets camera to default view. `1`..`9` cycle through annotated waypoints (camera flies to selected wp) — debugging stairs.

**B-P5. Validation overlay.**

При regenerate / load, sandbox запускает `Validate(plan)`. Issues displayed:
- Bottom-right corner: scrollable list of issues with severity glyph.
- Click issue → camera flies to offending entity bbox center.
- Filter chips: All / Errors / Warnings.

Helps catch generator bugs immediately rather than discovering when running полная игра.

---

### Милстоуны 16.5.B

**M16.5.B.0 - Sandbox skeleton + plane.**

`cmd/building_sandbox/main.go`: window + orbit camera + flat ground plane. Compiles, runs. Empty scene.

**M16.5.B.1 - Spawn building from BuildingPlan.**

Wire `BuildingSystem` (or a slim sandbox-local copy of it) to spawn `BuildingPlan` produced by `gen.House(seed=42, defaultParams)` on startup. Render walls / floors / openings via existing render code.

**M16.5.B.2 - BuildingViewMode integration.**

Add `BuildingViewMode` component to spawned building. Wire C-P1 cutaway + C-P2 widget (если они landed в Phase 16) или sandbox-local stub if Phase 16 не landed. Sandbox runs ahead of Phase 16 C? Lock в decision: sandbox depends on Phase 16 C-P1 + C-P2 landing first. Phase 16.5 starts after Phase 16 closure.

**M16.5.B.3 - Annotation gizmos.**

Hotkey overlays (L / W / S / A / C / D / F / V / G). Render gizmos as inline rl.Draw calls (no shader pipeline).

**M16.5.B.4 - Generator picker UI.**

Immediate-mode UI chips for template selection + param sliders + Regenerate button. Wired to gen package.

**M16.5.B.5 - Validation overlay + flyTo.**

Validator output displayed; click-to-fly camera to issue location.

**M16.5.B.6 - .glb load path.**

Hardcoded path text edit + Load button. Loader runs (Phase 16 M16.A.1 LoadBuildingPlan), output displayed same way as generator output. Side-by-side comparison: generator output then loader output for an equivalent author-exported .glb.

---

## Что считаем "закрытием Phase 16.5"

- **16.5.A** — Три generator templates (House / Office / Compound) emit valid BuildingPlan'ы с full annotations. Determinism verified. .bplan round-trip without diff. main.go fallback использует generator вместо хардкода.
- **16.5.B** — Sandbox boots <1s, рендерит generated buildings, БТ widget + cutaway работает, annotation overlays toggle'аются hotkey'ями, validation issues отображаются + flyTo жмётся. .glb loader path тоже работает в sandbox.
- ROADMAP updated. PHASE-16.5.md -> `.claude/old/`.
- CLAUDE.md "Building lifecycle" дополнен ссылкой на generator templates как примеры layout.

После - Phase 17 (Visual fidelity 1: unit/prop models, textures, lighting, road/river splines).

---

## Заметки на полях

- **Generator vs procedural fallback.** Текущий `generateBuildingLayout` после Phase 16 остаётся как минимальный backwards-compat хак. Generator из 16.5 — отдельная, более богатая система. После 16.5 закрытия — переписать `generateBuildingLayout` как тонкий wrapper вокруг `gen.House(seed=hash(plan), defaultParams)` или удалить (если все callers переехали на manifest + generator).

- **Sandbox как regression harness.** В CI/CD (если когда-нибудь будет) sandbox можно запустить с `-headless -test-template=Compound -seed=42 -validate-only` flag — генерирует plan, валидирует, exit 0 / 1. Без рендера. Полезный smoke check для PR'ов.

- **Generator complexity creep.** Соблазн добавить 10 templates (cathedral, mall, bunker complex, train station, ...). Phase 16.5 keeps three. Каждый последующий template = опциональный extender pack, не блокирует фазу.

- **Артистам не подавляет.** Generator output - test content, не replacement art. Когда artist делает кастомное здание (.glb), оно вытесняет generator-вариант через manifest. Generator стоит как «дешёвая прототипная содержимое + validation test», не «вместо artist'а».

- **Sandbox annotation gizmos переиспользуются.** L/W/S/A/C/D/F/V hotkeys полезны и в полной игре для debug. Phase 21 (Visual fidelity 2?) может перенести их как production-quality debug overlay через `app.UI.DebugLayer`. Sandbox — first iteration ground.

- **Furniture data source.** Templates пишут furniture с kind строками (`sandbags`, `desk`, `crate`). `PropTypeRegistry` Phase 16 расширяется этими kinds; если registry не имеет kind — generator emits warning + использует generic `prop_block` placeholder. Generators не должны жёстко зависеть от registry contents; missing-kind fallback bullet-proof.

- **Sandbox + Phase 16 dependency ordering.** Phase 16 closes first (loader / level / annotations machinery). Phase 16.5 starts ON the Phase 16 base — generator emits same shape that loader consumes, sandbox использует те же rendering paths. Параллельная работа невозможна — нужен Phase 16 closure first.

- **Cmd binary name.** `cmd/building_sandbox` matches existing `cmd/particle_sandbox`. Build: `go build -o /tmp/building_sandbox ./cmd/building_sandbox`. Запуск: `/tmp/building_sandbox`.

---

## Открытые вопросы

1. **Generator API style — builder DSL vs declarative struct.** Builder: `gen.NewBuilder().AddLevel(...).AddWall(...).Build()`. Declarative: `gen.BuildingSpec{Levels: []LevelSpec{...}, Walls: []WallSpec{...}}`. Builder ergonomic для intersection algebra (e.g. "wall between these two levels"), declarative simpler для serialization. Phase 16.5 simple: declarative struct primary, builder helpers as thin overlay. Lock at M16.5.A.0.

2. **Sandbox UI framework.** Re-use Phase 13 `drawChip` / `drawCyclicField` from Inspector? Они в `ui` package, sandbox `package main` — import OK. Lock: reuse, не plumb own immediate-mode framework.

3. **Generator parameter ranges.** Office 100 stories? Compound с 50 wings? Phase 16.5 lock: ranges enforced в template (`MaxStories=10, MaxWings=8`). Outside-range -> ValidationIssue.

4. **Determinism для slot positions.** Furniture slots distribute deterministically по seed. Per-furniture sub-seed = hash(seed, kind, instance-index). Лoка at M16.5.A.0.

5. **Sandbox window size persistence.** Sandbox saves/restores window size between launches? Phase 16.5 simple: hardcoded 1280x720 like particle sandbox. Полная игра имеет layout persistence; sandbox не нужно.

6. **.glb file picker реализация.** raylib has `rl.GuiFileDialog` в raygui (not in raylib-go base). Phase 16.5 simple: text input для path. Future полировка с raygui.

7. **Annotation hotkey clashes.** Полная игра использует L (не используется?), W (?), S (movement WASD), A (?). Sandbox - отдельный process, ничего не конфликтует. Lock: own hotkey set, не sync со full-game overlay hotkeys. Phase 21 unification если когда-нибудь.

8. **Validate ошибки в полной игре.** Loader Phase 16 уже валидирует .glb input. Need same validator? Lock: share validator code между sandbox + loader. `systems/building_gen/validate.go` exports `Validate(plan)` consumed both paths.

9. **Generator templates как production content path?** Если artist не делает art assets вовремя, можем ли ship'нуть game с generator-only buildings? Phase 16.5 не отвечает; вопрос для Phase 18+ campaign content. Сейчас generator — dev tool, не shipping content path.

10. **Compound подземный тоннель — entrance ramp или teleport stairs?** Tunnel connects basement levels horizontally. Подходы:
    - **Ramp** (stairs с малым Y-delta + большой XZ-delta) — корректно, но визуально странно (плоская lambdaштrep).
    - **Horizontal corridor** mesh + `stairs_<name>` wp chain на одинаковой Y — geometrically прямой horizontal coridor, semantically passage between two levels.
    Lock: horizontal corridor. Mesh visual не имеет ступеней (просто floor plate), waypoints связывают levels. Generator handles обoих варианта; default — horizontal.
