# Phase 15 - рабочий план

Большая фаза, три параллельных track'а: тактический ИИ (15.A), индивидуальное позиционирование юнитов + расслабление формаций (15.B), UI/UX foundation после UI.md (15.C). Tracks независимы по коду, но логически связаны - autonomy / reason feedback из 15.C опираются на reactive behaviour из 15.A; individual positioning из 15.B обнажает reason "юнит вне formation slot" в Inspector.

Цель фазы - после 14.5 (combat core) и 14.6 (hotfixes) сделать так чтобы:
- Юниты под огнём не стояли как мишени (15.A SurvivalInstinct).
- Игрок мог поставить отдельного бойца за дерево без вырывания из squad'а (15.B).
- UI отвечал на "что юнит делает и почему" (15.C reason feedback).

Без этих трёх tactical core не feels right - юниты тупят, formation жёсткая, игрок не понимает AI.

ROADMAP - порядок фаз. COMMAND-MODEL §7 - реактивное поведение / контракт с Phase 14. UI.md - детальный UX spec для 15.C. GAMEDESIGN §4 / §9 - принципы. Этот файл - план фазы.

---

## Track 15.A - Tactical AI

### Решения, которые лочим

**A-P1. Reactive behaviour - TacticalOverride marker pattern.**

`SurvivalInstinctSystem` находит unit'а с `Suppression > threshold`, выбирает cover slot перекрывающий ThreatDir, ставит на unit'а маркер `TacticalOverride{Reason, Until}`. UnitMovementSystem видит маркер -> читает `ActionQueue` с приоритетом override action (move to cover). FormationSystem filter'ит `Without[TacticalOverride]` - unit временно вне формационного учёта.

Маркер снимается:
- ThreatSource decay'нул + Suppression упал ниже clear-threshold N сек подряд.
- Player issue'д new order (explicit override clears tactical override).
- Unit в covered slot (achieved goal).

`Until` - safety timeout (например 30 сек). Если за это время не cleared, force-clear для предотвращения залипания.

**A-P2. Cover evaluation - utility score.**

Для каждого cover slot вокруг unit'а (читаем из CoverMap + cover_slots, Phase 6):

```
score = dot(slot.CoverDir, -threatDir) * distanceFalloff * (1 - occupancyPenalty)
```

- `dot(CoverDir, -ThreatDir)` - cover faces away from threat (1.0 - идеальная защита, 0 - sideways, -1 - exposed).
- `distanceFalloff = 1 / (1 + dist / 10m)` - близкие slots приоритетнее.
- `occupancyPenalty` - 0 если slot свободен, 0.5 если занят союзником, 1 если врагом.

Best score wins. Записывается в `LocalBlackboard.AssignedCover` для consistency между тиками (не мечется между slots при шуме в score'е).

**A-P3. ScatterProtocol для squad scramble.**

Когда `DeltaSuppression > threshold` в squad center за окно (~3 сек) - squad переходит в `SquadState.Scrambling`. Все members индивидуально ищут cover, formation slots игнорируются. Те кто не нашёл cover за 5 сек - prone в место. После 15 сек без подавления - squad re-aggregates back to formation.

Phase 15 simple: scramble через `TacticalOverride` маркер на всех members одновременно. Phase 15 не делает full state machine для squad'а - just bulk-apply override.

**A-P4. Doctrines (Patrol / Assault / Stealth / Defense) - macro presets.**

Не отдельный enum - набор default'ов для `MovementProfile + EngagementRules + BehaviorRules`. Apply doctrine = bulk-set fields. UI chip "Doctrine: Stealth" в Inspector меняет несколько fields одним кликом.

Список:
- **Patrol** - Walk + Stand + Standard + RoadPrefer + FreeFire + AutoCover.
- **Assault** - Run + Stand + Standard + Direct + FreeFire + AggressiveReturn.
- **Stealth** - Walk + Crouch + Quiet + CoverSeek + HoldFire + NoReposition.
- **Defense** - Walk + Crouch + Standard + Direct + ReturnFire + HoldPosition.

Storage - `DoctrineSpec` table (Spec pattern из 14.5). Component `ActiveDoctrine{Code}` на squad'е - визуально show какая активна.

**A-P5. Posture audible detection.**

`MovementProfile.Posture = Quiet` уже scaffolded. Phase 15 даёт real effect:
- Quiet posture reduces unit's noise emission radius.
- Vision system extension: hostile units detect noisy targets через increased awareness range (audio cone, не visual cone).
- Practical effect: Stealth doctrine units crouching past enemy patrol meet less detection чем standing units.

Implementation: `AudioEmissionRadius` - field в `Vision` или отдельный component. Vision query расширен на audio detection (вторичный sensing channel).

**A-P6. Standoff / Sector в EngagementRules - живые.**

Phase 13 scaffolded but unused. Phase 15 wires:
- `Standoff` - prefer to fight at >= N meters. Squad с этим rule auto-repositions if enemy closer than Standoff threshold (reverse-move at Walk pace).
- `Sector` - half-cone yaw + half-angle. Squad's WeaponSystem filter'ит targets вне sector. Defends only the sector направление. UI ghost arc shows sector при DefendPosition.

**A-P7. Wait-for-stragglers - macro path advance gate.**

Carry-over from Phase 9 feedback. SquadMacroPathSystem advance freeze если roster max-distance-to-center > 2*FormationSpacing. Resume когда rejoin. Это closes "командир улетел вперёд, остальные не догнали".

Implementation: per-tick compute roster spread, gate macroPath.advanceWaypoint() на spread < threshold.

---

### Милстоуны 15.A

**M15.A.0 - SurvivalInstinct skeleton.**

`SurvivalInstinctSystem` (new). InitUI handles, Filter for `Unit + Suppression + Awareness`. Per-tick (time-sliced 1-bucket per second through ID hash):
- For each unit with Suppression.Level > threshold:
  - If no TacticalOverride - acquire AssignedCover via utility score.
  - Set TacticalOverride{Reason: TacticalOverrideUnderFire, Until: now+30s}.
  - Push ActionMoveTo to cover slot pos.
- For each unit with TacticalOverride:
  - If Suppression.Level < clear_threshold for 3s continuous - clear override.
  - If Until expired - clear (safety).

`CoverEvaluation` helper (utility function). Reads CoverMap + cover slots.

**M15.A.1 - ScatterProtocol.**

Squad-level state machine добавить `SquadState{Code}`: Idle / Engaged / Scrambling.
DeltaSuppression tracking per-squad через rolling-window resource.
Trigger scramble: deltaSupp > threshold in 3s window.
Scramble action: all live members get TacticalOverride for cover.
Recovery: deltaSupp < low for 15s -> squad back to Engaged.

**M15.A.2 - Doctrines spec table.**

`DoctrineSpec` per Spec pattern. 4 doctrines. UI chip in Inspector squad view: 4 chips, click applies doctrine to squad. Active doctrine highlighted. Doctrine writes through MovementProfile / EngagementRules / BehaviorRules (so existing readers immediately see changes).

**M15.A.3 - Audio detection extension.**

`Audio` field on Unit (or component). Vision system pass extended with audio detection cone (typically 360 degrees but range-modulated). Quiet posture reduces audio emission. Test scene: enemy patrol detects loud (Run + Stand) squad earlier than quiet (Walk + Crouch).

**M15.A.4 - Standoff / Sector wired.**

WeaponSystem.shouldFire respects Sector. SquadMacroPath / FormationSystem may insert reposition if standoff violated. Inspector EngagementRules section expanded with Standoff slider + Sector yaw indicator.

**M15.A.5 - Wait-for-stragglers.**

SquadMacroPathSystem freeze advance when spread > 2*spacing. UI indicator in Inspector: "Waiting for stragglers" reason.

---

## Track 15.B - Formation slack + individual positioning

### Решения

**B-P1. IndividualPosition component.**

```go
type IndividualPosition struct {
    Mode IndividualPositionMode  // Absolute / Relative
    AbsolutePos    WorldPos
    RelativeOffset rl.Vector2  // XZ from squad center
    AcquiredAt     float32     // session time
}
```

Optional. Present when player explicitly placed unit. Absent -> normal formation slot.

**B-P2. FormationSystem filter behaviour.**

```
Filter[Unit, SquadMember, Without[IndividualPosition]] -> slot driven
Filter[Unit, SquadMember, IndividualPosition] -> override driven
```

Two parallel sub-passes. Override sub-pass reads IndividualPosition.Mode:
- Absolute -> goal = AbsolutePos verbatim
- Relative -> goal = squadCenter + RelativeOffset

**B-P3. UX entry points.**

- Single-unit selection (LMB on unit, not marquee) - Inspector switches to single-unit view (already exists in Phase 7/12).
- Shift+RMB on 3D-point (while single unit of squad selected) - spawn IndividualPosition{Absolute, target}.
- Multi-unit selection (subset of squad in marquee) - Shift+RMB на разные точки -> per-unit Absolute overrides.
- "Return to formation" Inspector chip - remove IndividualPosition.

**B-P4. Override lifecycle.**

- Absolute -> persists until removed.
- Relative -> persists, recomputes goal each tick relative to current squad center.
- Issue new Order to squad:
  - Default: keep individual overrides (squad moves, units keep relative offset to center if Relative, или fly to absolute if Absolute - в этом случае squad waits for them at goal).
  - Alt+RMB Issue: clear all individual overrides first.
- Unit death/leave squad - component removed automatically (entity destroyed).

**B-P5. Wall-aware separation steering.**

Phase 14.6 ships basic reflection. Phase 15.B extends:
- Sliding (parallel-glide along wall) когда movement direction near-parallel to wall.
- Adjacent unit slot swapping - если два юнита идут в overlapping slot'ы, более-far swap'ает slot с closer для no-stop progress.
- Tunnel handling - в узком проходе formation collapses на single-file column auto.

**B-P6. Slot clamping vs InsideBuilding (carry from 14.6).**

Phase 14.6 introduced slot clamping. Phase 15.B полирует:
- Slot inside building footprint -> spiral search.
- Slot in unreachable region (no path from squad center) -> drop slot, unit holds with squad center.
- Slot underwater (river prop) -> drop / search.
- Squad approaching building entrance - формация collapses to file column auto.

---

### Милстоуны 15.B

**M15.B.0 - IndividualPosition component + FormationSystem split.**

Component added. FormationSystem refactored to dual-filter pass. Smoke test: spawn unit with IndividualPosition{Absolute, (10, 10)} - unit goes there, ignores formation slot.

**M15.B.1 - UX wiring.**

Single-unit selection path. Shift+RMB handler. Inspector "Return to formation" button. Multi-unit subset selection -> per-unit individual placement.

**M15.B.2 - Override lifecycle.**

Default keep-overrides on new Order. Alt+RMB clear-overrides modifier. Test: stealth squad with 4 units placed individually behind cover. New MoveTo - default carries overrides relative; Alt+MoveTo resets to formation slot.

**M15.B.3 - Wall avoidance polish.**

Sliding steering vs walls. Tunnel single-file. Slot swap adjacency. Playtest: squad через узкий коридор без застревания.

**M15.B.4 - Slot clamping polish.**

Search radius extended. Building approach -> auto-file. Unreachable slot fallback.

**M15.B.5 - Living movement polish.**

Per-unit motion feel without animation (animation = Phase 17/25). Все changes
поверх UnitMovementSystem.step + Motion component, без архитектурных дельт.

- **Acceleration / braking.** Motion расширяется полем `Accel float32` (или
  отдельный Inertia component). step() заменяет `mot.Speed = clamp(desired, max)`
  на ramp `speed += clamp(desired - speed, -accel*dt, +accel*dt)`. Per-stance
  accel: Stand 4 m/s^2, Crouch 2, Prone 1. ~15 lines.
- **Smooth turning.** Cap Yaw delta per tick: `maxYawRate = 3 rad/s` (~170 deg/s,
  realistic для пехоты). Pre-движение turn: |desired - cur| > 60deg -> speed
  reduced (нельзя бежать вбок).  ~10 lines.
- **Per-unit personality (slack).** Hash entity.ID -> deterministic per-unit
  offsets: SlotJitter (+/- 0.3m random slot offset), ReactionDelay (0-200ms
  before responding to slot change), SpeedMul (0.95-1.05). Squad не идёт
  идеально ровно, члены чуть разной скорости. Hash pattern уже есть в
  FormationLoose slotHash32. ~20 lines.
- **Path look-ahead.** Cut corners: aim at Waypoints[Head+1] когда
  |pos - Waypoints[Head]| < 2m. ~15 lines.
- **Anticipatory deceleration.** Перед поворотом или waypoint близко - снижение
  скорости. Cost: braking distance = speed^2 / (2*accel), если distance to next
  turn point < braking -> tone speed down. ~25 lines.
- **Garrison rally / column collapse from Phase 14.6 followup.** FormationSystem
  detects centerTarget on NavInBuilding cell (или приближение к узкому
  проходу) -> auto-switch на FormationColumn behind commander. Все members в
  file через дверь. Без A* per-member (см. Phase 14.6 design discussion -
  squad-as-unit principle). Объединяется с B-P5 tunnel handling +
  B-P6 building-entrance file. ~50 lines.

Combined: ~120 lines. Один пас по UnitMovement + FormationSystem +
extension Motion struct. Playtest goal: squad движется по полю / через
здание / в узком коридоре - кажется "живой", не machine-like.

**Deferred к Phase 17/25.** Real walk/run animation cycle, idle pose,
visible stance transitions, footstep audio, body-tilt при повороте -
требуют моделей и vertex anim.

---

## Track 15.C - UI/UX foundation

### Решения

**C-P1. Autonomy Level - Spec table + persist component.**

`AutonomySpec` table (4 entries: Strict / Cautious / Adaptive / Survival). Each spec carries default BehaviorRules + MovementProfile + EngagementRules suggestions.

`ActiveAutonomy{Code}` component on Squad. Set via Inspector chip. Side-effects:
- Bulk-applies spec fields to squad's BehaviorRules (overwrites unless individual fields user-edited - track per-field "dirty" flag, see C-P2).

Spec details (initial):
- **Strict** - HoldUntilOrdered=true, all auto-flags false, SuppressionThreshold=1.0 (never react).
- **Cautious** - HoldUntilOrdered=false, AutoStance=true, AutoReposition=false, ReturnFire=true, SupprThr=0.5.
- **Adaptive** - all auto-flags true, SuppressionThreshold=0.3.
- **Survival** - all auto + scatter / retreat permission, SuppressionThreshold=0.15.

**C-P2. Field-level "edited by user" tracking.**

Без этого Autonomy chip overwrite'нет hand-tuned settings each click. Solution: track per-field "dirty" flag in a parallel `BehaviorRulesEdit{DirtyMask}` component.
- Player toggles individual flag - dirty bit set.
- Autonomy chip clicked - apply spec fields, skip dirty ones.
- Reset chip in Inspector clears all dirty bits and reapplies pure spec.

Phase 15.C simple: dirty mask uint8 (one bit per BehaviorRules field). Detailed UX-flow обсуждается later.

**C-P3. Reason feedback line.**

Inspector single-squad / single-unit view добавляет block:

```
State:    Moving to Forest Edge
Override: Taking cover under fire
Reason:   Suppression 0.72 > threshold 0.45
Resume:   No visible enemy 10s AND suppression < 0.20
```

Source data:
- State - active Order kind + target label.
- Override - TacticalOverride.Reason if present.
- Reason - human-readable mapping from override reason. Reasons enum:
  - `UnderFire`
  - `LostLOS`
  - `NoPath`
  - `Reloading`
  - `OutOfAmmo`
  - `WaitingForTransport`
  - `RadioLost`
  - `BlockedByContact`
  - `HoldFirePreventsAttack`
  - etc. (see UI.md §7 full list).
- Resume - per-reason rule (template per reason kind).

Phase 15.C simple: covers ~6 most common reasons. Full UI.md list - Phase 21.

**C-P4. Event log panel.**

New panel. Initially occupies one of L1 slots via Tab swap (Field-preset has Inspector tab, Command-preset adds Event Log tab on right column). Phase 18 (L4) moves to dedicated dockable panel.

Event types (initial):
- `EnemyContact` - first sighting of enemy by squad.
- `KIA` - unit died.
- `SuppressionStart` - squad enters suppressed state.
- `OrderCompleted` - successful completion.
- `OrderFailed` - failed completion.
- `BuildingCleared` - garrison success (Phase 16).
- `LowAmmo` - any squad member < 25% ammo.

`Event` ECS entity (or resource ring buffer). Components: `EventKind`, `EventAt` (WorldPos + Time), `EventSquad` (optional), `EventText` (cached display string).

`EventLogPanel.render` - vertical list, newest top, auto-scroll. Click event -> map camera flyTo position.

**C-P5. Map pings.**

`MapPing{Pos, Color, SpawnAt, TTL, Kind}` entity. Visual: pulsing circle on map. Auto-spawned on key events (EventLog driven), or via explicit player API (Phase 21 ping hotkey).

Phase 15.C: events auto-ping (EnemyContact, KIA, BuildingCleared). Player ping API - Phase 18.

**C-P6. AttackMove+HoldFire warning chip.**

Carry-over from project memory TODO. Inspector when squad has:
- EngagementRules.Mode = HoldFire
- AND active Order has OrderParamAttackMove

Shows strike-through "AttackMove" chip with tooltip:
"AttackMove ignored. Squad's RoE is HoldFire. Switch to ReturnFire or FreeFire to enable opportunistic engagement."

One-liner fix, big UX win.

**C-P7. Order timeline draft (Inspector inline, not full panel).**

Phase 15.C ships **inline timeline** в Inspector single-squad view - just list of queued orders с progress bars. Не full git-tree timeline panel (то - Phase 18).

```
Orders:
[==>     ] Move to Forest          (active, 35%)
[        ] Defend Position         (queued)
[        ] Patrol Loop             (queued, loop)
```

Phase 18 takes this further: dedicated panel с visual timeline, animated, gaps, branches.

---

### Милстоуны 15.C

**M15.C.0 - Autonomy Level spec + chip.**

AutonomySpec table. Component ActiveAutonomy. Inspector single-squad chip row (4 chips). Click applies. Dirty mask scaffold (no UI yet).

**M15.C.1 - Reason feedback line.**

ReasonCode enum. Inspector block. Initial 6 reasons wired. TacticalOverride.Reason translation. Resume condition template per reason.

**M15.C.2 - Event log panel.**

Event entity + EventLog resource ring buffer. EventLogPanel ui slot. Auto-scroll, click-to-fly. Wired to 5 initial event kinds (EnemyContact, KIA, SuppressionStart, OrderCompleted, OrderFailed).

**M15.C.3 - Map pings.**

MapPing entity + spawn on key events. MapRenderSystem draws pulsing circles. TTL-driven despawn.

**M15.C.4 - AttackMove+HoldFire warning.**

Inspector chip strike-through + tooltip. One-line fix to existing chip render.

**M15.C.5 - Inline order timeline.**

Inspector single-squad orders section reformatted as timeline-like list. Progress bars from OrderProgress. Active highlight.

---

## Что считаем "закрытием Phase 15"

- **15.A** - юниты под огнём ищут cover, squad scramble'ит при шквале, doctrines presets работают, audio detection wired, standoff/sector живые, wait-for-stragglers активен.
- **15.B** - IndividualPosition component работает, Shift+RMB ставит юнит за дерево, wall-aware steering без застреваний.
- **15.C** - Autonomy chips работают, Reason feedback в Inspector, Event log + map pings live, AttackMove warning, inline timeline.
- ROADMAP updated, PHASE-15.md -> old/.
- Issue list updated. Phase 14.6 issues closed unless carried over.

После - Phase 16 (Buildings 2.0 + Asset pipeline).

---

## Заметки на полях

- **Track independence.** 15.A/B/C можно делать в произвольном порядке. Зависимости:
  - 15.A нужен 14.6 (wall avoidance fix).
  - 15.B нужен 14.6 (slot clamping skeleton).
  - 15.C self-contained mostly.
  - 15.C.4 (AttackMove warning) можно сделать в 14.6 если хочется бесплатный win.

- **TacticalOverride расширяемость.** Маркер несёт `Reason` enum. Phase 17+ может добавить новые reasons (Reloading, MountingVehicle, BuildingTask) - тот же pattern, не требует архитектурных изменений.

- **Cover slot occupancy** - кто кладёт `Occupancy` writes? Phase 6 wrote slots as data, no reader. Phase 15.A reader сам managess occupancy when assigns slot (writes ToReleasedAt when releases). Очень простой "self-managed by SurvivalInstinct" scheme.

- **Doctrine vs Autonomy** - oба УUI-уровневых концепта. Доктрина = "что squad делает" (Patrol/Assault/Stealth/Defense), Autonomy = "сколько свободы AI" (Strict/Cautious/Adaptive/Survival). Они orthogonal: Stealth Doctrine + Strict Autonomy = "выполнять стелс-приказы буквально, не отклоняться". Stealth + Survival = "стелс пока safe, отступать при контакте".

- **Reason feedback overhead** - rendering 5-6 строк текста в Inspector per frame неощутимо. Reason computation тоже cheap - чтение TacticalOverride.Reason + lookup table. Не optimize prematurely.

- **Event log retention** - ring buffer ~200 events. Старые drop'аются. Phase 18 может добавить save-to-file mission log.

---

## Открытые вопросы

1. **Dirty mask UX.** Когда player tweak'ает individual BehaviorRules field, ему нужен visual indicator "this field is now custom, Autonomy chip won't overwrite"? Или silent? Phase 15.C simple: silent + "Reset to Autonomy default" button.

2. **TacticalOverride priority.** Player issue'ит explicit AttackTarget на enemy. SurvivalInstinct want's cover. Который wins? UI.md §3 implies player explicit > AI auto. Implementation: AttackTarget order is "explicit fire intent" -> TacticalOverride cleared / suppressed. Lock decision in M15.A.0.

3. **Squad-level vs per-unit reactive behaviour.** ScatterProtocol scatters all members at once. Should individual auto-cover work per-unit independently (e.g. one unit takes hit, only that unit covers)? Phase 15 simple: per-unit. Scramble = many per-unit triggers fire same tick.

4. **Audio detection mechanics specificity.** Range, modulation by stance/pace/equipment. Phase 15.A simple: cone radius = base * paceMul (Walk 1.0, Run 1.5, Sprint 2.0) * postureMul (Standard 1.0, Quiet 0.4). Numbers placeholder, balance in Phase 25.

5. **Wait-for-stragglers UI hint.** When triggered, Inspector shows "Waiting for X, Y, Z" with names? Or just generic? Phase 15 simple: generic "Waiting for stragglers (3/4 caught up)".

6. **IndividualPosition relative recomputation cadence.** Per-tick? Or only at squad center motion events? Per-tick simpler, per-tick cost минимальный (1 vector subtraction per overridden unit). Lock per-tick.

7. **MapPing pulsing animation.** Smooth sin-driven or step-anim? Phase 15 simple: sin(time * 4) controls outer radius. Phase 18 polish может dial.
