# Phase 5 — Buildings + Trenches (archived summary)

Closed. Living implementation: `components/aabb.go`, `components/building.go`, `components/trench.go`, `systems/building.go`, `systems/building_layout.go`, `systems/building_index.go`, `systems/trench.go`, `systems/stamp.go` (`RectCut`, `Trench`, `cutAlongPolyline`). Building rendering helpers in `main.go` (`drawBuildingFloor` / `Wall` / `Stairs`). This is the architectural-decisions card; the original milestone plan is in version history.

## Locked decisions

1. **`Building` is a root entity, not a resource.** Carries `Kind / Stories / Yaw / Footprint / Seed` and `WorldPos` (centre) and `AlwaysActive`. Created at startup from `BuildingPlanList`; survives chunk eviction so cross-chunk queries (clearance, AI targeting) keep seeing it.
2. **Children = ordinary entities under `BuildingMember{Building ecs.Entity}`.** Walls / floors / stairs / doors / windows. All carry `WorldPos + LODRelevant` so chunk eviction tears them down via `BuildingChildIndex` (root-keyed).
3. **`BuildingChildIndex` is keyed by building root, not chunk.** Each child belongs to exactly one root, and one root's children all live in one chunk (constraint #5). Easier addressing for Phase 11 destruction than a chunk-keyed map.
4. **Wall = one segment with at most one opening.** Multi-window walls are split into multiple `WallSegment` entities. `WorldPos` is the segment's "from" endpoint; geometry parametrised by `Length / Yaw / Height / Thickness` plus opening fields.
5. **Building footprint must lie inside one chunk.** Per-building constraint that lets all children share a single `Pos.Chunk`. Multi-chunk buildings deferred to Phase 15.
6. **Smart-Object data is written, not read.** `CoverDirection` on every wall, `ShootingArc` on every window, `Occupancy` on doors and windows. Phase 6 (CoverMap) and Phase 10 (TacticalAI) consume.
7. **Bunker = `BuildingBunker` + `Stamper.RectCut`.** Sunken footprint, floor at `surfaceY - bunkerDepth` (3 m), cosine-falloff skirt 4 m wide. Walls extend from the sunken floor up to surface; an extra Stairs entity provides the entrance.
8. **`Stamper.Trench` shares a private helper with `RiverCut`.** Both are cosine-falloff polyline cuts; the shared `cutAlongPolyline` is internal, the public methods stay distinct so call sites read the intent.
9. **Two markers, same idea as RoadSystem:** `BuildingTerrainProcessed` (bunker `RectCut` applied; gated by `Without[Modified]`) + `BuildingsProcessed` (children spawned; NOT gated by `Modified` so child entities respawn even when the heightmap is frozen).
10. **`BuildingPlan` resource.** Hand-authored startup data (3 plans currently). After startup nothing reads it — the `Building` component carries everything systems need.
11. **`PropSpawnSystem` clearance from buildings + trenches.** `tooCloseToBuilding` rejects candidates within 1.5 m of any footprint. `tooCloseToTrench` mirrors river/road logic with a 1.0 m margin.
12. **`LODSystem` excludes building children.** They're pinned to `LODRelevant` by BuildingSystem; chunk owns the lifecycle so generic distance-LOD is moot.
13. **No collisions in Phase 5.** Anchor walks through walls / closed doors / windows. Phase 6 NavGrid + Phase 7 unit controller will respect them; the debug anchor stays a free-flier.
14. **No persistence of buildings.** Deterministic from `BuildingPlan` + `Seed`. `Modified` per-building only when Phase 11 lands destructive damage.

## Deliberately deferred

- Wall / door / window / floor collision → Phase 6 (NavGrid) + Phase 7 (unit controller).
- Cover-slot generation around walls + at corners → Phase 6.
- AI consumers of Smart Objects → Phase 6+.
- Building destruction (`Modified`-on-building, breach entities, breach-as-graph-edge) → Phase 11.
- Multi-chunk buildings → Phase 15.
- Auto-generation from `BiomeDensity` / OSM → Phase 15.
- Floor-slice rendering for tall buildings under the camera → Phase 14 (shader trick).
- Door-open animations + proximity triggers → Phase 16 / Phase 7.
- Loaded building meshes with vertex-color tagging or raycast scanner → Phase 15 (DESIGN.md "Building auto-tagging").
- Internal-room layout (corridors, partitions) — placeholder is one open hall per storey.

## Notes / caveats

- Children store WorldPos in absolute chunk-local coords (not relative to the building root). Direct O(1) render with no parent-chain transforms; the cost is having to re-derive every child if the building ever moves (it doesn't, until destruction).
- Layout generator (`generateBuildingLayout`) is a pure function from `Building` (and host-chunk base coords + surface Y). Identical seed ⇒ identical children, every chunk respawn.
- Render uses the building entity's own helpers (`drawBuildingWall` etc.), NOT `PropTypeRegistry` — buildings are non-uniform and benefit from segment-aware drawing (split + lintel + sill + panel).
- `BuildingChildIndex` evict path iterates buildings filtered to those rooted in the evicting chunk. With the current few-buildings test scene this is trivially cheap; a chunk→buildings reverse index can be added in Phase 15 if scaling demands it.
