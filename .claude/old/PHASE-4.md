# Phase 4 — Roads (archived summary)

Closed. Living implementation is in `components/road.go`, `systems/road.go`, `systems/road_preprocess.go`, `systems/stamp.go` (`RoadFlatten`), and `systems/prop_spawn.go` (road clearance). This file is the architectural-decisions card kept for reference; the original milestone-by-milestone plan is in version history.

## Locked decisions

1. **`RoadGraph` is a singleton resource, not entities.** Read by RoadSystem, prop clearance, future Phase 6 NavGrid and Phase 8 vehicle pathfinder. Mutated only at startup. Edges store `From / To uint16` indices into `Nodes`, plus `Kind` and `Width`.
2. **`Rivers` (was `RiverNetwork` in plan) is also a resource.** Already migrated in Phase 3; Phase 4 just reads it from systems uniformly.
3. **`RoadKind = Bridge` is a sub-edge tag, not a separate entity / marker.** Auto-assigned at preprocess time; users never author it.
4. **Preprocess: split road edges at river-strip boundaries.** `PreprocessRoadGraph` densely samples each edge, finds the in/out transitions of the polyline strip (`d < polyline.Width/2`), inserts new nodes at those transitions, and tags inside-strip sub-edges as `RoadBridge`. Sample-based detection survives the case where an edge intersects only one polyline-segment point. Road×road junctions are NOT auto-split — user supplies them in the input graph (OSM importer will deliver them properly structured in Phase 15).
5. **`Stamper.RoadFlatten`** — cosine-falloff blend toward a linear `targetY` between segment endpoints. Half-cos weight `w = 0.5*(1 + cos(π * d / (Width/2)))`; at `d = 0` the heightmap is fully replaced; at the strip edge, no change. NOT additive (unlike `RiverCut`). Bridge sub-edges skip flatten — heightmap stays cut by the river underneath.
6. **Two markers, mirroring why Phase 5 needs the same pattern:** `RoadProcessed` (heightmap-flatten applied; gated by `Without[Modified]` so player edits aren't double-applied) + `RoadPropsSpawned` (visuals; NOT gated by `Modified` so road-surface and bridge props survive on player-edited chunks).
7. **Pipeline order:** `... river → road → prop_spawn → terrain_mesh ...`. River first so the bridge can sit on top of an already-cut riverbed; prop_spawn last so trees see road clearance.
8. **Road-surface visual = three prop types** (`PropRoadHighway`, `PropRoadLocal`, `PropRoadDirt`) plus `PropJunction`. Plane primitives, fixed 4 m spawn step. Width comes from per-type meta `Size.X` (placeholder until Phase 15 swaps in real meshes). Bridge reuses Phase 3's `PropBridge`.
9. **`Modified` is NEVER set by RoadSystem.** Procedural; derivable from the graph on respawn — zero disk footprint.
10. **Road / junction props live in `PropChunkIndex`,** so chunk eviction tears them down with the rest of the chunk's props. Same lifecycle as Phase 3 vegetation.
11. **PropSpawnSystem clearance.** `tooCloseToRoad`: distance from candidate to nearest segment < `edge.Width/2 + 1.0 m` ⇒ skip. O(edges × candidates), fine at hand-authored sizes.

## Deliberately deferred

- Vehicle pathfinding along `RoadGraph` → Phase 8 (`RoadFollower`).
- NavGrid road-bias for infantry path-cost → Phase 6.
- Auto-split of road×road intersections → Phase 15 (OSM gives them well-structured).
- Auto-generation of road networks between settlements → Phase 15.
- Differentiated surface shaders → Phase 16 polish.
- Bridge / road damage and destruction → Phase 11.
- LOD for road props (long stretched mesh per-edge instead of plate chain) → Phase 16.
- Highway-following-terrain (Highway as multi-segment path that hugs hills) → Phase 15.

## Notes / caveats

- The "midpoint inside polyline-strip ⇒ Bridge" heuristic is robust because preprocessor splits at strip boundaries before tagging. Earlier "midpoint within `Width/2` of polyline" idea would have failed when only a single intersection exists.
- `Stamper.RoadFlatten` blends toward a straight `targetY` line between endpoints, so a Highway between two points at different heights cuts a slope through whatever hill sits between them. Acceptable for placeholder scenes; OSM importer will deliver short multi-segment paths that hug terrain.
- Road plate offset `+0.05 m` above target Y to avoid z-fighting with the terrain mesh on flatten-blend boundaries.
- Junction nodes spawn their plate in the chunk that owns `node.Pos` (deterministic floor-rounding rule), avoiding duplicate spawns when a node sits on a chunk seam.
