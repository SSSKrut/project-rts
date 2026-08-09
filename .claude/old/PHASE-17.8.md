# Phase 17.8 — рабочий план

**Tactical AI Wave 3.** Серьёзная переработка per-unit AI до состояния «отряд = серьёзная единица, не цирк со застреванием в дверях». Phase 17 (Wave 2) построил фундамент per-unit'а (MicroPath, Threat, StanceController, Cover 2.0, SurvivalInstinct, Autonomy), но в Phase 17.6 testing'е стало видно ключевые пробелы: юниты бьются в стену не обходя, squad упирается в первую секцию 3-секционного здания, formation slots иногда попадают за стены без обхода, multi-section building pathing неполный. Phase 17.8 закрывает все эти провалы плюс добавляет Utility-evaluator слой с hysteresis для «осознанных» переключений mode'ов и reason-feedback в Inspector.

Перед Fog of War (Phase 18+). Без серьёзного AI FoW превратится в frustration — игрок не видит часть карты, доверяет AI её отыграть, а AI глупый. 17.8 — критический путь.

`PHASE-17.md` (Wave 2) — base layer, эта фаза расширяет, не переписывает. `COMMAND-MODEL.md` §7 (реактивное поведение) — формальная схема. `GAMEDESIGN.md` §9 (иерархический AI: Strategic / Operational / Tactical) — Phase 17.8 ровно на нижнем тактическом тире.

Длинная фаза по объёму (10 milestones, ~1500-2000 строк новой логики + рефакторинг), потому что цель амбициозная. Архитектурный лок-ин в P-decisions важнее скорости — фаза задаст шаблон для будущих Strategic / Operational tiers (Phase 22).

---

## Что в Phase 17.8 сознательно НЕТ

- **Strategic AI** (кто куда наступает, какие squad собрать, какие RoE назначить на оперативном уровне) — Phase 22. Это neural-net-eligible tier; 17.8 — чисто классический tactical (BT / Utility / FSM).
- **GOAP (Goal-Oriented Action Planning)**. Решение зафиксировано — Utility + reactive replan покрывают 90% случаев тактической пехоты без planner cost. Если по результатам 17.8 покажется что эмерджентного поведения мало — рассмотрим в Phase 22+ для оперативного тира, не для tactical.
- **Multi-squad coordination** (joint orders, formation crosswalk, fire-and-manoeuvre между отрядами) — Phase 19/21. 17.8 — единичный юнит и его соседи внутри одного squad'а.
- **Vehicle behaviour** (танки, БТР, БМП в Utility evaluator) — Phase 19. В 17.8 vehicle-mode placeholder, без реального reader'а.
- **Medic healing logic.** Treating mode опционален и оставлен как stub spec entry для Phase 21 (Engineering / Medical full).
- **Per-weapon target selection** (weapon-bar override). Это Phase 14 weapon-bar carry-over, отдельный UX track.
- **Animation lerp / smoothing.** 17.8 — поведенческие изменения. Visual smoothing — Phase 17.5 или 25.
- **Neural net анywhere.** Tactical tier остаётся классическим, как зафиксировано в CLAUDE.md / GAMEDESIGN.md.
- **Civilian / Neutral faction поведение.** 17.8 — Player vs Enemy. Civilians (если когда-нибудь появятся) — отдельный pass.

---

## Архитектурная картина

Per-unit AI делится на пять концептуальных слоёв, каждый — независимая ECS-система:

```
┌─────────────────────────────────────────────────────────────┐
│  PERCEPTION    Awareness (Phase 7) — vision LastSeen FIFO   │
│                Threat (Phase 17) — DangerBuffer signal mix  │
│                LocalBlackboard (новое) — per-unit shared    │
├─────────────────────────────────────────────────────────────┤
│  REASONING     UtilityEvaluatorSystem (новое, 0.5s cadence) │
│                Reads: Perception. Writes: CurrentMode +     │
│                Reason. Spec table UtilitySpec — 5-7 modes.  │
├─────────────────────────────────────────────────────────────┤
│  PLANNING      MicroPathSystem (Phase 17, расширяется) —    │
│                slot-aware: pathfinds к individual formation │
│                slot, не к squad center. FormationSystem     │
│                становится только slot-writer.               │
├─────────────────────────────────────────────────────────────┤
│  AVOIDANCE     LocalAvoidanceSystem (новое, per-tick) —     │
│                ORCA half-plane solver. Reads: neighbours    │
│                + walls. Writes: adjusted Motion.Velocity.   │
├─────────────────────────────────────────────────────────────┤
│  EXECUTION     UnitMovementSystem (existing, упрощается) —  │
│                applies velocity. reflectAgainstWalls        │
│                deprecated после ORCA, удаляем.              │
│                StanceController (existing) — Threat→Stance. │
│                WeaponSystem (existing) — reads CurrentMode  │
│                для refined fire decisions.                  │
└─────────────────────────────────────────────────────────────┘
```

Data flow per tick (упрощённо):
1. Perception системы пишут в Awareness/Threat/Blackboard.
2. Раз в 0.5s UtilityEvaluator переоценивает CurrentMode.
3. MicroPathSystem строит/обновляет путь к slot, dependent от Mode (Following → squad slot; Engaging → engage position; TakingCover → cover slot).
4. LocalAvoidance корректирует velocity от соседей и стен.
5. UnitMovementSystem применяет velocity.
6. StanceController + WeaponSystem действуют на основе Threat + Mode.

---

## Решения, которые лочим до начала кода

**P1. Action modes — финальный список 6 штук.**

```
Following      — default; идёт к formation slot / squad waypoint.
Engaging       — есть target в LOS + range + RoE allows; ведёт огонь, ограниченно двигается.
TakingCover    — под огнём / threat > threshold; ищет и удерживает cover.
Repositioning  — после выстрела / при потере cover; быстрый relocate к новой cover.
Reloading      — out of ammo + no immediate threat; stand or stay in cover.
Suppressed     — Threat.Total > heavy threshold; ложится, фиксируется. (отдельный от TakingCover — Suppressed = реактивное, TakingCover = планируемое.)
```

Treating (medic), Mounted (vehicle), Building (engineer) — добавлены как spec entries с `Enabled=false` для Phase 19/21 extension.

**P2. Utility scoring — weighted sum signals.**

Per-mode scoring function живёт в `UtilitySpec.Score func(*UtilityContext) float32`. `UtilityContext` бандлит per-tick state: Threat, Awareness, distance to formation slot, cover.Quality nearby, weapon.Ammo / RangeM, allies within radius, etc.

Базовые формулы (тюнятся в M17.8.9):

```
Following.Score      = 0.5 - threat * 0.3 - (1.0 if reload_needed else 0)
Engaging.Score       = (target_in_range ? 0.7 : 0.0) - threat * 0.2 + ammo_factor
TakingCover.Score    = threat * 0.9 + (cover_available ? 0.2 : -0.3)
Repositioning.Score  = (just_fired ? 0.6 : 0.0) * (cover_compromised ? 1.5 : 1.0) - threat * 0.5
Reloading.Score      = (ammo < 0.2 ? 0.8 : 0.0) - threat * 0.6
Suppressed.Score     = threat * 1.2 - 0.3  (наступает только когда threat очень высок)
```

Numbers — стартовые, не финальные. M17.8.9 — tuning под playtest.

**P3. Hysteresis — anti-flap protection.**

Mode-switch правило: `newMode != currentMode` требует одновременно:
- `newMode.Score > currentMode.Score + ModeSwitchDelta` (delta = 0.15 базово).
- `now - LastModeSwitch >= MinModeDuration` (1.0s базово, может per-mode override через spec).

Сохраняем `LocalBlackboard.LastModeSwitch float32 (session-time)`.

**P4. RVO/ORCA local avoidance — full implementation.**

Не forward-ray bandaid (отклонён пользователем). Per-tick per-unit:

1. Neighbour set: spatial hash query с радиусом `ORCANeighbourRadius = 6.0 m`, cap 15 neighbours (sorted by distance).
2. Static obstacles: walls в радиусе `ORCAWallRadius = 4.0 m` через wall segment iteration (per-chunk filter уже есть в WeaponSystem snapshot pattern).
3. Half-plane constraints: для каждого neighbour создаём ORCA half-plane (formula: Van den Berg et al., «Reciprocal n-body Collision Avoidance», 2011). Для каждого wall — half-plane прижимающий к тангенциальной стороне.
4. Linear program 2D: ищем velocity ближайшее к prefVelocity, удовлетворяющее всем half-planes. Если infeasible — выбираем least-conflict (3-D linear program с distance penalty).

Хорошо известная литература, есть reference implementations (RVO2 library). Для нас — Go port, ~400-500 строк математики плюс структура. Заменяет `reflectAgainstWalls` polностью.

**ORCA params (тюнятся):**
- `TimeHorizon = 2.0s` (предсказание collision до 2 сек впереди)
- `TimeHorizonObst = 1.5s` (короче для статических — стены ближе)
- `Radius = unit.Collider.Radius` (existing, 0.4-0.5m обычно)
- `MaxNeighbours = 15`

**P5. `LocalBlackboard` rework.**

Был пустой struct (Phase 7 stub). Теперь:

```go
type LocalBlackboard struct {
    CurrentMode      ActionMode  // P1 list
    LastModeSwitch   float32     // session-time
    Reason           [32]byte    // fixed-size string for Inspector (избегаем allocации)
    ReasonLen        uint8
    AssignedCover    ecs.Entity  // Phase 17 existing usage — оставляем
    AssignedTarget   ecs.Entity  // Engaging mode target (out of Awareness)
    GoalSlot         WorldPos    // target slot computed by FormationSystem
    GoalLevel        ecs.Entity  // если goal внутри здания — Level entity
    PrefVelocity     [2]float32  // pre-ORCA velocity (x,z); ORCA читает + writes back
}
```

Reason — fixed-size [32]byte для избежания allocации каждый tick. Helper `SetReason(blackboard, "Taking cover N-E")` copies в fixed buffer.

**P6. Slot-aware MicroPath.**

Сейчас MicroPath строится к `squad.MacroPath.Waypoint[k]` или к squad center. После рефакторинга: цель = `LocalBlackboard.GoalSlot`. FormationSystem на каждом тике пересчитывает slot per unit (existing formula) и пишет в blackboard, **но больше не clamp'ит** unit position.

UnitMovementSystem reads goal from MicroPath (через ORCA-adjusted velocity), не из formation slot напрямую. Path-completion ⇒ unit at slot. Если slot meпересчитан (squad moved) — MicroPath.GoalSnap check видит drift > 1m → MicroPath.Dirty → replan.

**P7. Multi-section building TransitionEdge bake.**

Текущий `spatial_bake_transitions.go` создаёт edges Surface↔Level через doors и Level↔Level через stairs. **Pass 4 расширяется**: для каждой пары Level entities на ОДНОЙ storey (та же DisplayOrder / Stories index), если их AABB пересекаются по XZ ИЛИ если Level Y-difference < 0.5m AND distance между AABB centres < union-width — создаётся NodeLevel ↔ NodeLevel TransitionEdge через ground (Y-step minimal).

Параметры:
- `LevelJunctionMaxXZGap = 1.5m` — Level'ы соединяются если их центры не дальше этой суммы (size_a + size_b) * 0.5 + gap.
- `LevelJunctionMaxYDiff = 0.5m` — Y разница маленькая (одна storey).

Тест-case: 3-section Office. Levels[0..2] для ground storey, после bake — TransitionEdge 0↔1 и 1↔2 (или 0↔1↔2 transitive).

**P8. Replan triggers — расширенный список.**

`MicroPath.Dirty = true` ставится при:
1. **Goal drift** > 1.5m (existing — squad center moved).
2. **Path blocked**: после prev waypoint reached, next waypoint не достигается > 0.8s.
3. **Wall collision repeated**: ORCA reports `OvercrowdedSolution` (ситуация когда нет feasible velocity) 3 раза за 1 sec.
4. **Mode change**: UtilityEvaluator switched CurrentMode → goal изменился (новая cover, новое engage position).
5. **Floor transition exit**: только что прошли через TransitionEdge → replan на новой Level.

Bookkeeping: `LocalBlackboard.OvercrowdedSince float32` (track 1) + `LocalBlackboard.StuckSince float32` (track 2).

**P9. Per-tick performance budget.**

50 units (typical scene) target: AI cost ≤ 3 ms median tick.

- UtilityEvaluator: 0.5s cadence, ~50 evals * ~5 μs = 0.25 ms (amortized).
- LocalAvoidance (ORCA): per-tick, ~50 * ~40 μs = 2 ms (dominant cost).
- MicroPath: 16-replan-per-tick budget (existing), ~0.5 ms.
- Total: ~2.75 ms на 50 units. На 100 units — ~5.5 ms, ещё в пределах 16ms frame budget на 60fps.

Time-slicing: UtilityEvaluator через `entity.ID % 30 == tick % 30` bucket distribution, чтобы не все units переоценивают одновременно (cache hot).

**P10. Spec table pattern для UtilitySpec.**

Соответствует zafiksiрованной memory `feedback_spec_table_pattern`:

```go
type ActionMode uint8

const (
    ModeFollowing ActionMode = iota
    ModeEngaging
    ModeTakingCover
    ModeRepositioning
    ModeReloading
    ModeSuppressed
    ModeCount
)

type UtilityContext struct {
    // bundled per-tick state
    Threat        float32
    Awareness     *Awareness
    DistToSlot    float32
    CoverQuality  float32  // -1 if no cover nearby
    AmmoFraction  float32
    JustFired     bool     // last shot within 1s
    // ... etc
}

type UtilitySpec struct {
    Mode    ActionMode
    Name    string
    Score   func(*UtilityContext) float32
    Enabled bool
}

var UtilitySpecs = [ModeCount]UtilitySpec{ /* one row per mode */ }
```

Compile-time exhaustiveness через `[ModeCount]UtilitySpec` (как `OrderKindSpecs`).

**P11. CurrentMode → executor wiring (явная таблица).**

| Mode | MicroPath goal | StanceController | WeaponSystem |
|---|---|---|---|
| Following | formation slot | Mode default (Stand/Crouch by Profile) | shouldFire ok if RoE+Awareness |
| Engaging | engage position (existing logic) | Crouch | shouldFire enabled |
| TakingCover | cover slot (existing pickCover) | Crouch/Prone by Threat.State | shouldFire if has visible threat |
| Repositioning | new cover slot picked | Crouch (transit) | shouldFire suppressed during transit |
| Reloading | hold current or cover slot | Crouch | shouldFire suppressed (animation) |
| Suppressed | hold current pos | Prone (forced) | shouldFire suppressed (suppression) |

WeaponSystem.shouldFire получает дополнительный gate: `CurrentMode != Reloading && CurrentMode != Suppressed` (read из LocalBlackboard).

**P12. `reflectAgainstWalls` deprecated, удаляется после ORCA готов.**

В M17.8.5 (ORCA готов) `unit_movement_walls.go` удаляется (~150 строк). Это explicit deletion — без ORCA bandaid становится bug, с ORCA — overhead.

**P13. Inspector reason row — minimum viable display.**

Inspector override-section показывает (если CurrentMode != Following):
```
Mode:   TakingCover
Reason: Suppression 0.72 N-E
Since:  3.2s
```

Single-squad / single-unit view — текст из `LocalBlackboard.Reason`. Multi-select — aggregate "3 units in cover, 2 reloading".

**P14. Test scenarios как первый-class артефакты.**

Не просто closure criteria — конкретные scene-файлы. Что нам нужно:

1. **`scene_corridor.go`** — 30 units squad через узкий corridor (3m wide). ORCA должен развести в column-like flow.
2. **`scene_office_3section.go`** — 3-section Office, squad из 6 units. ПКМ-tap на каждую секцию доводит squad до целевой.
3. **`scene_under_fire.go`** — 8 player units + 4 enemy в напротив. Cover available. Все player units должны переходить в TakingCover, ≥ 2 в Engaging если cover insufficient.
4. **`scene_multi_squad.go`** — 3 player squads через одну road. ORCA разводит без stuck'ов.

Эти scenes — в `cmd/ai_sandbox/` (новый sandbox), не в main.go testbed. Sandbox используется для regression testing после каждого M-milestone.

---

## M-milestones (sequence важен)

**M17.8.0 — Architecture lock-in + sandbox.**

- Все P-decisions финализированы.
- `cmd/ai_sandbox/` создан с минимальной scene-loading инфраструктурой (CLI flag `-scene=name`).
- Test scenes как Go-функции `BuildSceneCorridor(world)` / `BuildSceneOffice3Section(world)` / etc.
- Document data flow в комментарии package-level.

**M17.8.1 — LocalBlackboard rework + ActionMode enum.**

- Расширить components/blackboard.go (если не существует — создать; P5 layout).
- Helper `SetReason(b, text string)` — fixed-buffer copy без allocations.
- `ActionMode` enum + первоначальные `UtilitySpec` rows (только Name field — Score функции в M17.8.2).
- Spec table compile-time guard.

**M17.8.2 — UtilityEvaluatorSystem skeleton + scoring.**

- Новый file `systems/utility_evaluator.go` + sibling `utility_evaluator_scoring.go` для per-mode functions.
- Periodic update (0.5s cadence), bucket distribution.
- `UtilityContext` bundling per-tick state.
- Score functions per P2 (стартовые формулы).
- Hysteresis logic per P3.
- Writes `LocalBlackboard.CurrentMode` + `Reason` + `LastModeSwitch`.

Verify: in sandbox `scene_under_fire`, при контакте все player units переходят в TakingCover (или Engaging если cover absent). Inspector показывает Reason.

**M17.8.3 — Mode → executor wiring (P11 table).**

- WeaponSystem.shouldFire reads CurrentMode (через LocalBlackboard handle), gates fire для Reloading/Suppressed.
- StanceController reads CurrentMode для override (Suppressed → forced Prone).
- MicroPath goal selection — switch по CurrentMode (M17.8.4 ниже расширит).

Verify: Reloading mode → юнит не стреляет даже если threat есть. Suppressed → forced Prone.

**M17.8.4 — Slot-aware MicroPath (FormationSystem refactor).**

- FormationSystem перестаёт clamp юниты к slot. Только writes `LocalBlackboard.GoalSlot`.
- MicroPathSystem reads GoalSlot вместо squad waypoint.
- UnitMovementSystem reads MicroPath waypoints напрямую, не через formation clamp.
- Backwards-compat: если LocalBlackboard.GoalSlot == zero (legacy), fallback на squad waypoint behaviour.

Verify: в sandbox `scene_corridor`, squad из 8 units проходит через 3m corridor без свалки — каждый юнит идёт к своему slot, формация чуть растягивается но не ломается.

**M17.8.5 — LocalAvoidanceSystem (ORCA core).**

- Новый file `systems/local_avoidance.go` + sibling `local_avoidance_orca.go` для math.
- ORCA half-plane builder (per neighbour, per wall).
- Linear program 2D solver (с 3D fallback для infeasible).
- Spatial hash neighbour query reuse (Phase 14.5).
- Wall segment iteration per chunk.
- Updates Motion velocity post-ORCA.
- **Delete `unit_movement_walls.go`** (reflectAgainstWalls obsolete).

Verify: `scene_corridor` flow smooth, no stuck'ов. `scene_multi_squad` 3 squads через one road — разводит. Performance: ORCA < 2.5ms median на 50 units.

**M17.8.6 — Replan triggers (P8 expanded list).**

- ORCA reports `OvercrowdedSolution` flag → LocalBlackboard.OvercrowdedSince accumulates.
- StuckSince track (low velocity AND non-zero pref velocity).
- MicroPath.Dirty triggers based on overcrowded/stuck/mode-change/floor-transition-exit.
- Failed state ("no path") — Order → Failed if MicroPath cannot find any route for 5s.

Verify: blocked goals (e.g. player tells squad to MoveTo location behind impassable wall) → Inspector shows "Blocked: no path" within 5s.

**M17.8.7 — Multi-section building TransitionEdge bake (P7).**

- spatial_bake_transitions.go Pass 4 extension.
- Same-storey Level pair detection (AABB junction).
- TransitionEdge NodeLevel↔NodeLevel emitted with small Y-step / Length param.
- Update PHASE-17.6.md outstanding bug section — fixed here.

Verify: `scene_office_3section` ПКМ-tap на каждую секцию — squad доходит до целевой через door нужной секции. J overlay показывает yellow edges между Levels.

**M17.8.8 — Inspector reason row (P13).**

- Inspector single-unit view + single-squad view добавляют Mode + Reason + Since лines.
- Multi-select aggregation: "3 in cover, 2 reloading" pattern.
- Visual style consistent с existing override row (Phase 15 M15.A.3 work).

Verify: каждый из 4 scenes показывает корректные reasons в Inspector.

**M17.8.9 — Tuning + playtest pass.**

- Запуск всех 4 scenes несколько раз, weights tuning.
- ORCA params (TimeHorizon, NeighbourRadius) подгонка.
- Hysteresis delta / MinModeDuration tweaks.
- Performance verify на 100 units (≤ 5ms target).
- Profiler snapshots (Ctrl+P) для regression baseline.

**M17.8.10 — Docs sync + close.**

- COMMAND-MODEL.md §7 (реактивное поведение) обновляется с финальной картой mode'ов и Reasons.
- CLAUDE.md секция Architecture — добавить новые systems (UtilityEvaluatorSystem, LocalAvoidanceSystem) в perиод pipeline list.
- ROADMAP.md — Phase 17.8 → ✅.
- PHASE-17.8.md → `.claude/old/PHASE-17.8.md`.
- Memory note `project_phase_17_8_scope.md`.

---

## Closure criteria (полные)

Все 4 sandbox scenes должны проходить, плюс performance budget.

1. **scene_corridor (50 units squad, 3m corridor).** Squad проходит без collision pile-up. Никто не стоит на месте > 2s. ORCA load median < 2.5ms.

2. **scene_office_3section (6 units squad, 3-section Office).** ПКМ-tap на каждую секцию — squad доходит до целевой через door нужной секции (not first section). Multi-level TransitionEdges visible в J overlay.

3. **scene_under_fire (8 player + 4 enemy).** При контакте:
   - 80%+ player units в TakingCover (если cover availability >= 0.5 per unit).
   - 20% в Engaging если cover insufficient.
   - Inspector показывает Reason per unit ("Suppressed", "Taking cover N-E", "Repositioning").
   - При threat clearing (enemy dead / no awareness 10s) — units возвращаются в Following.

4. **scene_multi_squad (3 player squads * 6 units, one road через choke point).** ORCA разводит. Никто не стоит > 2s. Все squads достигают goals.

**Regression-free:** все sceneы из Phase 14/17 (combat / cover) проходят без degradation. WeaponSystem поведение не сломано. SquadService API не сломан.

**Performance budget:**
- 50 units: AI total ≤ 3ms median (UtilityEvaluator + ORCA + MicroPath + StanceController).
- 100 units: AI total ≤ 6ms median.

**No bandaids removed silently:** `reflectAgainstWalls` явно удалён в M17.8.5 (после ORCA готов). Build не должен fall обратно на reflection.

---

## Зависимости

- **Phase 17 (Wave 2)** — обязательно. MicroPath, Threat, StanceController, Cover 2.0 — расширяются, не строятся с нуля.
- **Phase 14.5** — spatial hash используется в ORCA neighbour query (`SpatialHash.QueryRadius`).
- **Phase 7** — TransitionRegistry, NavService — расширяются P7 (multi-section).
- **Phase 14.6** — NavInBuilding bit + Garrison completion — остаётся.
- **Phase 15** — Autonomy levels, Doctrines — interaction точка: Autonomy.Aggressive boost'ит Engaging weight; Autonomy.Survival boost'ит TakingCover weight. Реализуется как multiplier на base scores в UtilitySpec.Score.

Не блокирует:
- Phase 17.7 (Behavior Panel split) — UI рефактор, независимый. Делать после 17.8 чтобы Inspector reason row уже был на месте.
- Phase 17.5 (Visual fidelity) — модели/текстуры, независимо от поведения.

---

## Открытые вопросы (закрываются в M17.8.0 при architecture lock-in)

1. **Treating mode** — оставляем placeholder spec entry с `Enabled=false` или вообще не включаем? Решение: placeholder (M17.8.1), Phase 21 заполнит.
2. **Vehicle / Mounted mode** — placeholder spec entry? Решение: placeholder (P1 already), Phase 19 заполнит.
3. **Autonomy.Strict** — должен ли запрещать Repositioning (auto-relocate)? Реально это и делает существующий BehaviorRules.AllowAutoReposition gate. Resolution в M17.8.2: Autonomy.Strict + BehaviorRules.AllowAutoReposition=false → Repositioning.Score *= 0 (мы blocking, как сейчас).
4. **Engaging mode и target selection** — кто выбирает Engaged target? Утилити сам или WeaponSystem? Решение: WeaponSystem picks target (existing logic), UtilityEvaluator только решает «есть ли смысл переходить в Engaging» на основе target_available signal. AssignedTarget field в blackboard — debugging only.
5. **CurrentMode сохранение в OrderQueue?** Если squad cancel'ит order — mode reset to Following? Решение: SquadService.CancelAllOrders сбрасывает CurrentMode → Following для каждого member через clearTacticalOverrides-like helper.
6. **ORCA с soloists (unit без squad).** Поведение должно работать. Solist'ы используют MicroPath без formation slot — goal = OrderTarget.Pos напрямую. Decision: M17.8.4 keeps soloist path unchanged, ORCA работает универсально (squad / soloist одинаково).
7. **Inspector reason aggregation** для multi-select. Сейчас Phase 17.6 Inspector multi-select показывает count summary. Reason aggregation — "X in Y mode" pattern, computed in DrawInspector. Решение в M17.8.8.

---

## Технические notes

**ORCA reference impl.** Van den Berg et al., "Reciprocal n-body Collision Avoidance" (2011). RVO2 library (C++) — известный open-source impl, лицензия Apache-style. Мы делаем Go port (не embed C++) ~400-500 строк математики. Структура: `ORCASolver{neighbours, walls}`, `Solve() Vec2` returns adjusted velocity.

**Time-slicing для UtilityEvaluator.** `entity.ID() % bucketCount == tick.frame % bucketCount` — distributing eval load across N=30 frames (≈ 0.5s at 60fps). Если frame rate variable — switch на elapsed-time bucket. Either works.

**Worker pool (Phase 11.6).** UtilityEvaluator и LocalAvoidance — кандидаты на parallelization. Snapshot/parallel/post-pass pattern: snapshot per-unit context serial → ParallelForIndexed для score computation / ORCA solve → serial post-pass для archetype mutations (Mode write).

**Race-detector.** Phase 14.5 pattern: после каждого M-milestone с параллельным кодом — `go test -race ./...` + `go run -race . -workers=4` for 30s in-game.

**Memory.** `LocalBlackboard.Reason [32]byte` фиксированный — не использует heap, не allocates каждый tick. Same pattern как `Awareness.LastSeen` (Phase 7).
