# Phase 14.6 - рабочий план

Hotfix-фаза после Phase 14.5. Чинит два блокера combat-плейтеста: краш при смерти юнита (Issue #11) и проход юнитов сквозь стены зданий (Issue #7 + расширенный). Плюс докручиваем M14.5.6 (Garrison completion gate), который остался scaffold-only в 14.5. Без этих фиксов ничего из Phase 15 не запустить полноценно.

Фаза узкая и фокусированная. Никаких новых components / systems / архитектурных дельт. Только bug-fixing и docs.

ROADMAP - высокоуровневый трекер. COMMAND-MODEL - спецификация управления. CLAUDE - паттерны после милстоунов. Этот файл - рабочий план фазы.

---

## Решения, которые лочим до начала кода

**P1. Issue #11 (crash) фиксим через "alive-check sweep" pattern, не через "post-death awareness cleanup".**

Two approaches:
- **(A) Defensive readers**: каждый читатель Map.Get на entity делает `world.Alive(ent)` first. Дёшево, локально, не трогает Awareness writer'ов. Минус - паттерн распространяется на много callsite'ов.
- **(B) Cleanup on death**: в `DamageService.ApplyDeath` walk'аем все Awareness FIFO и обнуляем slots с убитым target. Один централизованный fix. Минус - O(N) walk при каждой смерти, дороже.

Phase 14.6 берёт **(A) primary + (B) opportunistic** - alive-check везде где Get of cross-entity reference, plus Awareness sweep как cheap one-pass O(units) после death. (B) дёшево потому что death редка, units мало.

Это закрывает не только конкретный краш, а целый класс: любой будущий reader держащий entity-id-by-value автоматически защищён pattern'ом.

**P2. Issue #7 / walkthrough фиксим тремя слоями.**

Слой 1 - `NavCellInsideBuilding` flag. SpatialBakeSystem помечает cells inside `Building.Footprint` AABB этим flag'ом. `NavService.FindPath` reject'ит paths которые заходят в такие cells без TransitionEdge.

Слой 2 - FormationSystem slot clamping. Когда squad center близко к зданию, formation slots могут попадать внутрь footprint. Pre-write check: если slot cell имеет `NavCellInsideBuilding` или `NavCellBlocked` - spiral search к ближайшему walkable cell.

Слой 3 - UnitMovement basic wall avoidance. Per-tick steering check: если предполагаемое движение пересекает wall в next step, reflect velocity. Это catch'ит юнитов которые уже близко к стене (например через separation push) и держит их снаружи.

Все три нужны вместе. (1) blocks path-find. (2) blocks formation target. (3) catches per-tick edge cases.

**P3. Garrison completion (M14.5.6 wire) - простой "all members on floor" rule.**

`CompletionEveryMemberOnFloor` арм в `evaluateCompletion`:

```
walk roster:
  for each live member:
    if pointInFootprintAABB(member.pos, building) AND
       any FloorNavGrid covers member.pos {
      inside++
    }
return inside == roster.Count
```

Фаза 14.6 ships 100% threshold (все). Phase 15 SurvivalInstinct может ослабить до 50%+ когда squad coherence важнее full occupancy. UI progress fraction `(3/4 inside)` показывается в Inspector queued-orders chip.

NavService через TransitionEdge investigation НЕ скоупится сюда. Если completion ровный fix работает но юниты всё ещё не заходят надёжно - открываем расширенный fix в Phase 15.

**P4. Wall avoidance steering - reflection, не sliding.**

Когда near-future position юнита пересекает wall:
- compute wall normal в XZ
- reflect velocity component along normal (slow against wall, full speed parallel)
- clamp self position out of wall AABB epsilon

Sliding (parallel-glide along wall) - часть Phase 15 wall-aware steering. Phase 14.6 ships только reflection - дёшево, fixes worst-case fall-through.

**P5. Все три фикса в одной фазе - не разбивать.**

Каждый по отдельности - 1-2 дня работы. Вместе - неделя с repro проверкой. Разбивать в подфазы избыточно (нет архитектурных дельт чтобы лочить отдельно). Один атомарный PR / commit batch удобнее для bisect'а в случае регрессии.

**P6. Не открываем Phase 15 work в этой фазе.**

Соблазн "заодно добавлю individual positioning раз уже копаю movement code" - reject. Phase 15.B будет полноценным milestone'ом с собственным P-set'ом. 14.6 - hotfix only, scope discipline.

---

## Милстоуны

### M14.6.0 - Issue #11 crash fix

**Цель.** Краш после убийства юнита перестаёт воспроизводиться. Combat playtest 4-vs-8 идёт 60+ секунд без падений, юниты умирают, despawn'ятся чисто.

**Делаем:**
- `DamageService.ApplyDeath` extended с awareness-sweep step:
  ```
  walk Filter[Awareness]:
    for each LastSeen slot with Target == dyingUnit:
      slot.Time = 0; slot.Target = ecs.Entity{}
  ```
  Cheap (units ~ 30), O(units * AwarenessSlots) = ~240 writes на death.
- `WeaponSystem.pickTarget` - alive-check первой строкой цикла после `e.Target == zero` guard:
  ```
  if !sys.worldRef.Alive(e.Target) { continue }
  ```
- `OrderResolverSystem.advanceOutOfRange` - alive-check `target.Entity` до `posMap.Get`. Если dead - сбрасываем tracker.Elapsed (target gone -> completion = Done в parent caller, мы тут не должны накручивать).
- `OrderResolverSystem.resolveTargetPos` - alive-check до switch. Если target dead, return (parent caller обрабатывает via completion arm).
- `applySplashDamage` callback - уже имеет alive-check, проверить что он первой строкой.
- `propagateSuppression` callback - alive-check уже есть, проверить.
- Sandbox или test scene: setup combat, ждём пока юнит умирает, repro nil-panic. После фикса - clean run.

**Проверяем.** `go run -race .` 60 sec combat без panic'а. Stress: 10 deaths за минуту. Inspector single-unit на убитом entity (selection holds id) - graceful "Unit no longer alive" hint, не краш.

---

### M14.6.1 - Building no-walkthrough fix

**Цель.** Юниты не проходят сквозь стены зданий. Скриншот scenario (squad стоит у стены, юнит R внутри blocked cell) - reproduce и confirm fixed. Path-find через NavService не возвращает paths cutting through building footprint без door.

**Делаем:**
- `components/nav.go` - расширить `NavCell` flags:
  ```go
  type NavCellFlags uint8
  const (
      NavCellPassable      NavCellFlags = 0
      NavCellBlocked       NavCellFlags = 1 << 0
      NavCellInsideBuilding NavCellFlags = 1 << 1 // only via TransitionEdge
  )
  ```
  (Или extend существующий byte field, см. spatial_bake.go).
- `SpatialBakeSystem` Pass N - walk BuildingPlanList, for each building's Footprint AABB mark covered cells с InsideBuilding bit. Не overwrite Blocked bit от walls - они coexist.
- `NavService.FindPath` reject paths с cells matching InsideBuilding flag UNLESS path contains TransitionEdge (Door) whose target floor matches.
- `FormationSystem.slotPlacement` - после compute slot target check: NavGrid cell at slot. If InsideBuilding || Blocked - spiral search в радиусе ~3m к ближайшему walkable cell. Use that as slot target.
- `UnitMovementSystem.step` - wall avoidance:
  ```
  predicted_pos = pos + velocity * dt
  near_walls = SpatialHash(walls).ForEachInRadius(predicted_pos, separationRadius * 2)
  for each wall:
    if segmentIntersectsWall(pos, predicted_pos, wall) {
      normal = wallNormalXZ(wall)
      velocity = reflectXZ(velocity, normal) * 0.5
    }
  ```
  Walls SpatialHash - новый resource, rebuilt при building spawn/despawn (rare). Или alternative - reuse `wallsByChunk` map from WeaponSystem.
- Repro test: setup scene с building near squad spawn. Issue MoveTo через building. Verify path obходит. Issue MoveTo внутрь building без Garrison - path Failed. Issue Garrison - path goes through door.

**Проверяем.** Visual playtest: squad never overlaps building footprint geometrically. Map view (blue cells) coincides with where units actually stand. NavGrid debug overlay (N hotkey) показывает InsideBuilding cells marked distinct color. Repro по user'скому скриншоту - юнит не залезает в стену.

---

### M14.6.2 - Garrison CompletionEveryMemberOnFloor

**Цель.** M14.5.6 wire'ит CompletionEveryMemberOnFloor для Garrison kind. Squad с Garrison order completes только когда все live members внутри footprint AND on Floor cell. Inspector shows "Garrison (X/N)" progress пока incomplete.

**Делаем:**
- `OrderKindSpecs[Garrison].Completion` = `CompletionEveryMemberOnFloor`. ArrivalRadius остаётся как fallback (если нет Floor entity рядом - старая логика).
- `evaluateCompletion` арм `CompletionEveryMemberOnFloor`:
  ```
  bld = buildingMap.Get(target.Entity)
  if bld == nil { return Pending }
  roster = rosterMap.Get(squad)
  inside = 0
  alive = 0
  for member in roster.Members[:Count]:
    if !world.Alive(member) { continue }
    alive++
    pos = posMap.Get(member)
    if pos == nil { continue }
    if pointInFootprintAABB(pos, bld.Footprint) AND anyFloorAt(pos) {
      inside++
    }
  if alive == 0 { return Failed }  // wipeout
  if inside == alive { return Done }
  return Pending
  ```
- Helper `anyFloorAt(pos)` - walk Filter[Floor, WorldPos] в 1-chunk window вокруг pos, check Y proximity (<=1.5m above/below). Cache via building's children index for cheap lookup.
- `Inspector.drawOrderRow` для Garrison kind показывает fraction:
  ```
  Garrison @ Bldg #X42  (3/4 inside)
  ```
  Read inside-count via lightweight resolveCompletion peek (not actual eval, just read tracker if we add one).
  
  Simpler: just compute on demand in render pass (40+ frames/sec * 1 squad * 4 members = 160 checks - negligible).

**Проверяем.** Test scene: select squad, RMB on building. Commander входит через door first, остальные следом. Order InProgress пока хоть один снаружи. Inspector chip "3/4 inside" updates live. Последний заходит - order Completed, squad теперь в Garrison state.

---

## Что считаем "закрытием Phase 14.6"

- Issue #11 closed - 60-second combat test без panic.
- Issue #7 partially closed - building no-walkthrough fixed. Если выявится что NavService через TransitionEdge тоже плохо работает (юниты не входят через дверь надёжно даже когда path correct), это переносится в Phase 15.B note.
- M14.5.6 wired - Garrison completion на (X/N inside) с visual progress.
- ISSUES.md updated - #7 и #11 закрыты, или partial-fix notes если scope расширился.
- PHASE-14.6.md -> `.claude/old/PHASE-14.6.md`.

После - Phase 15 (Tactical AI + Formation slack + UI foundation).

---

## Заметки на полях

- **NavCellInsideBuilding + Door TransitionEdge** - они работают вместе. Door entity создаёт TransitionEdge между outside-surface cell и floor cell внутри. Path-find разрешает crossing если edge доступен (door open). Closed door - block. Window - LOS only, не traversal (Phase 24 may add window breach as separate kind).

- **Wall avoidance reflection vs sliding** - reflection дешевле, fixes fall-through. Sliding (parallel-glide) feels smoother but требует aware-of-wall-direction steering. Phase 15.B unit movement work добавит sliding если playtest показывает что reflection даёт "bouncing" feel.

- **Spiral search для slot clamping** - max radius 3m, step 0.5m, 6 directions. O(36) cells checked worst-case. Cheap. Если не находит walkable - fallback к squad center (unit стоит у командира, не идёт в формацию).

- **Walls SpatialHash** - можно отложить. Phase 14.6 simple: walk wallsByChunk в 1-chunk radius вокруг pos (already have). Если перформанс боль - SpatialHash в Phase 15 формально.

- **Repro fidelity** - test scene должна включать building near squad spawn point. Текущая test scene (3 player squads + 1 enemy) спавнится далеко от building'а - не triggers walkthrough scenario. Добавляем в test scene spawn point ближе к building, или в Phase 14.6 doc'е инструкция "manually move squad to building edge to repro".

---

## Открытые вопросы

1. **Awareness sweep cost при streaming chunks** - если 100+ units load в memory (Phase 16+), awareness sweep при каждой death = O(100 * 8) = 800 writes. Negligible. Но если units >1000 - rethink. Phase 14.6 simple: full sweep.

2. **Slot clamping fallback** - что если spiral search не находит walkable в 3m? Fallback к squad center? Drop slot offset, держать unit at center? Phase 14.6 finalize - подумай в процессе.

3. **Wall avoidance в multi-floor scenario** - unit на 2 этаже подошёл к window edge. Wall (внешняя стена 2-го этажа) has Y range. Reflection должна work только если ray.Y intersects wall.Y range. Phase 14.6 - check Y, no-op если outside range.

4. **NavService alive-check vs FindPath result staleness** - между computeMacroPath и movement, target entity might die. MacroPath uses cached path. После target death, path stays "to where target was". Order completion arm catches it (Done on target death). Не блокер 14.6 но note.
