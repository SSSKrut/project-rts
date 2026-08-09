# Arma Reforger AI — структура и заметки для адаптации

Источник: декомпил `base_game_assets/data{,007}/`.
- BT‑файлы: `data/AI/BehaviorTrees/{Chimera/{Soldier,Group}, Waypoints, SmartActions, Animals}`
- Скрипты: `data007/scripts/Game/AI/{Behavior, Movement, Group, Components, Reaction, Covers, Events, Configs, ScriptedNodes, ...}`
- Префабы агентов: `data/AI/AIAgents/*.et` (`ChimeraAIAgentFull`, `ChimeraAIAgentVehicle`, `SCR_PlayerAIAgent`).

Цель: понять схему «как Reforger принимает решения», чтобы переиспользовать паттерны в нашем `project-rts` (Ark ECS + Go).

---

## 1. Двухуровневая модель: Group ↔ Soldier

Reforger всегда держит ДВЕ параллельные иерархии BT, синхронизированные через mailbox‑сообщения:

### 1.1 Group BT — `Chimera/Group/Group.bt`
```
Root
 └ Sequence
    ├ SCR_AIDecoIsGroupInitializing   // ждать пока все юниты заспавнились
    └ Parallel
       ├ Sequence
       │  ├ SCR_AIDecideActivity   →  out: ActivityTree path, UpdateActivity flag
       │  └ AITaskIdle 0.3 s       // re‑evaluate каждые 300 мс
       ├ Selector
       │  ├ DecoTestVariable(updateActivity == true) → SCR_AIUpdateExecutedAction → SetVar(updateActivity=false)
       │  └ SCR_AIIsValidAction → RunBT(activityTree)   // выполняем выбранную Activity
       └ RunBT("Group/HandleWP.bt")                     // обработка waypoint‑очереди
```
**Переменные группы:** `updateActivity` (bool), `activityTree` (string), `leader` (IEntity).

### 1.2 Soldier BT — `Chimera/Soldier/Soldier.bt`
```
Root → Parallel:
 1) RunOnce(Seq "Default"): SetStance STAND + SetWeaponRaised 0 + SetMovementSpeed WALK
 2) Sequence
     ├ Sequence "Detection":
     │   └ Decorator → SCR_AIFindTargetToLookAt(timeSinceSeenMax=1s, timeSinceDetectedMax=0.4s)
     ├ SCR_AIDecideBehavior  → out: BehaviorTree, UpdateBehavior, UseCombatMove, UpdateInterval
     └ AITaskIdle (Period = UpdateInterval)
 3) Selector → DecoTestVariable(updateBehavior) → SCR_AIUpdateExecutedAction
              ↳ SCR_AIIsValidAction → RunBT(behaviorTree)
 4) Selector → DecoTestVariable(useCombatMove) → RunBT("Soldier/HandleCombatMove.bt")
              ↳ SCR_AIResetCombatMoveState
 5) RunBT "Soldier/LookAction.bt"
 6) RunBT "Soldier/HandleOrders.bt"
 7) RunBT "Soldier/HandleAmmo.bt"
```

**Чтение в один проход (per‑tick одного солдата):**
1. найти что/кого посмотреть → 2. выбрать поведение и UpdateInterval → 3. выполнить behavior‑BT →
4. обработать боевое перемещение → 5. целиться/жесты → 6. вычитать почту приказов → 7. проверить боезапас.

Шаг (2) — то самое utility‑решение: какой BT‑файл крутить из шага (3).

---

## 2. Utility AI поверх BT

### 2.1 `SCR_AIUtilityComponent.EvaluateBehavior(unknownTarget)`
Вызывается из движкового evaluator‑а перед сменой behavior. Делает:

1. `m_ThreatSystem.Update(dt)` — пересчёт угрозы.
2. `m_SectorThreatFilter.Update(dt)` — какие сектора вокруг агента опасны.
3. `m_CombatComponent.UpdatePerceptionFactor(...)` — модификация зрения от уровня угрозы.
4. Чтение почты: `SCR_AIMessageGoal` / `SCR_AIMessageInfo` / `SCR_AIMessage_Cancel` → запуск нужной `SCR_AIReaction`, которая добавит/удалит Behavior из утилити‑списка.
5. Реакции: `m_Reaction_UnknownTarget`, `m_Reaction_RetreatFromTarget`, `m_Reaction_SelectedTargetChanged` (плагины из конфига).
6. `m_CombatComponent.EvaluateWeaponAndTarget()` — выбор цели и оружия.
7. `RemoveObsoleteActions()` + `EvaluateActions()` — выбор Behavior с максимальным `EvaluatePriorityLevel()`.
8. Если новый Behavior выиграл и текущий `IsActionInterruptable()` → `SetCurrentAction(...)`.

### 2.2 Приоритеты Behavior — `SCR_AIActionBase`
Жёстко зашитые float‑константы в трёх диапазонах:

| Уровень | База | Назначение |
|---|---|---|
| NORMAL    | 0    | штатные приоритеты |
| PLAYER    | +1000 | игрок дал приказ — перебивает фон |
| GAMEMASTER| +2000 | миссионный скриптинг — поверх игрока |

Внутри NORMAL (ранжировано высокое→низкое):

```
GET_IN_VEHICLE 130    GET_OUT 125    PICKUP_ITEMS 118    STATIC_ARTILLERY 114
FIRE_ILLUM_FLARE 113  THROW_GRENADE 112    MEDIC_HEAL 111   RETREAT_FROM_TARGET 110
PROVIDE_AMMO 100      ATTACK_SELECTED 90  HEAL_WAIT 83    MOVE_FROM_VEH_HORN 72
ATTACK_NOT_SELECTED 70   OBSERVE_THREATS_HI 69    MOVE_FROM_UNKNOWN_FIRE 68
OBSERVE_UNKNOWN_FIRE 66    HEAL 65    MOVE_AND_INVESTIGATE 64    SUPPRESS 63
DEFEND 61    FIND_FIRE_POSITION 60    OBSERVE_LOW 59    MOVE_INDIVIDUALLY 58
OPEN_NAVLINK_DOOR 54   PERFORM_ACTION/MOVE/MOVE_IN_FORMATION 30
ATTACK_DISREGARD_THREATS 20    WAIT 10    OBSERVE_LOW 4    MOVE_IN_FORM_LOW 3
IDLE_DRIVER 2    ANIMATE 1.5    IDLE 1
```

В PLAYER: `RETREAT_MELEE 1190 > AVOID_CHARACTER 1180 > GET_OUT_HI 1162 > MOVE_FROM_DANGER 1160 > ATTACK_HI 1120 > HEAL_HI 1115 > OBSERVE_UNKNOWN_FIRE_HI 1113`.

В Group (Activity) приоритеты гораздо «плотнее»:
```
GET_IN 130   FOLLOW 130   RESUPPLY 100   COMBAT_WITH_VEHICLES 85
HEAL 80   ARTILLERY 75   ATTACK_CLUSTER 70   SEEK_DESTROY 60
INVESTIGATE_CLUSTER 55   DEFEND_FROM_CLUSTER 55
MOVE 50   PERFORM_ACTION 50   DEFEND 50   GET_OUT 50   SUPPRESS 40   ANIMATE 10
```

### 2.3 Behavior — `SCR_AIBehaviorBase`
```c++
class SCR_AIBehaviorBase : SCR_AIActionBase {
    SCR_AIUtilityComponent m_Utility;
    float m_fThreat = 0.0;       // прибавляется в общий уровень угрозы пока активен
    bool  m_bAllowLook = true;   // разрешать ли LookAction.bt
    bool  m_bResetLook = false;
    bool  m_bUseCombatMove = false;  // включить sub‑BT боевого перемещения
    ResourceName m_sBehaviorTree;    // ← путь к BT‑файлу, который запустит RunBT
    int   GetCause();                // SAFE/SELF_AID/GROUP_GOAL/DANGER_LOW/COMBAT/DANGER_MED/DANGER_HI/ALWAYS
}
```
Класс наследуется → подкласс задаёт `m_sBehaviorTree` + `SetPriority(...)`. Конкретные подклассы:
`SCR_AIIdleBehavior`, `SCR_AIWaitBehavior`, `SCR_AIAttackBehavior`, `SCR_AIDefendBehavior`,
`SCR_AIFindFirePositionBehavior`, `SCR_AIMoveBehavior`, `SCR_AIRetreatBehavior`,
`SCR_AIMoveFromDangerBehavior` (→ `SCR_AIMoveFromGrenadeBehavior` и др.),
`SCR_AIObserveThreatSystemBehavior`, `SCR_AISuppressBehavior`, `SCR_AIHealingBehavior`,
`SCR_AIProvideAmmoBehavior`, `SCR_AIPickupInventoryItemsBehavior`, `SCR_AIPerformActionBehavior`,
`SCR_AIAvoidCharacterBehavior`, `SCR_AIFireIllumFlareBehavior`, …

### 2.4 Activity (групповой аналог Behavior) — `SCR_AIActivityBase`
То же, но владелец — группа (`SCR_AIGroupUtilityComponent`), у каждой Activity опционально привязан `AIWaypoint`. Виды: `SCR_AIIdleActivity`, `SCR_AIMoveActivity`, `SCR_AIDefendActivity`, `SCR_AISearchAndDestroyActivity`, `SCR_AIAttackClusterActivity`, `SCR_AIInvestigateClusterActivity`, `SCR_AIDefendFromClusterActivity`, `SCR_AIFireteamsClusterActivity`, `SCR_AIFollowActivity`, `SCR_AIHealActivity`, `SCR_AIResupplyActivity`, `SCR_AIPerformActionActivity`, `SCR_AIGetInActivity`/`SCR_AIGetOutActivity`, `SCR_AISuppressActivity`.

Activities тоже хранят `m_sBehaviorTree` (путь к Group‑BT файлу в `Group/Activity*.bt`).

---

## 3. Threat system — единственное число для всей реактивности

`SCR_AIThreatSystem` (на агенте) держит четыре независимых вклада, аггрегирует в один скаляр:

```
m_fThreatTotal = clamp(
        m_CurrentBehavior.m_fThreat   // вклад от текущего поведения
      + m_fThreatSuppression           // подавление: пули рядом + взрывы
      + m_fThreatInjury                // кровопотеря (BLEEDING_FIXED_INCREMENT = 0.3)
      + m_fThreatShotsFired            // далёкие выстрелы (decay 11%/s)
      + m_fThreatIsEndangered,         // на меня целятся (ENDANGERED_INCREMENT = 0.2)
      0, 1)
```

Стейт‑машина:
```
SAFE       (< 0.05)
VIGILANT   (0.05 .. 0.33)
ALERTED    (0.33 .. 0.66)
THREATENED (> 0.66)
```
Decay’и:
- Suppression: 10 %/s
- Shots fired: 11 %/s
- Endangered: 20 %/s (но пока цель в фокусе — фиксируется на 0.2 без декея)

Triggers (вход):
- `ThreatBulletImpact(count)` → +0.10 за пулю в `m_fThreatSuppression`.
- `ThreatExplosion(dist)` → линейная интерполяция от EXPLOSION_MAX_INCREMENT=0.2 при dist<12м до 0 при 100м.
- `ThreatShotFired(dist, count)` → ~`(0.002 + 0.008/(d+1))·count`, потолок 0.66.
- BLEEDING DamageEffect → `m_fThreatInjury = 0.3` (фикс), сбрасывается когда лечение убрало эффект.
- Текущая цель в `m_CombatComponent.GetCurrentTarget()` → `m_fThreatIsEndangered = 0.2`.

Threat **читают**: BT‑декораторы (для условных переходов), реакции на опасность (масштаб задержки реакции), сектор‑фильтр угроз, `CombatComponent.UpdatePerceptionFactor` (под угрозой видим хуже: 0.4 vs 1.0).

---

## 4. Danger events — событийная шина реакций

`EAIDangerEventType` (расширяется через `modded enum`) перечисляет типы инцидентов:
`DamageTaken, DoorMovement, Explosion, GrenadeLanding, MeleeDamageTaken, PhysicsContact, ProjectileHit, StartedBleeding, UnsafeArea, Vehicle, VehicleHorn, WeaponFired`.

Файлы: `Reaction/Danger/SCR_AIDangerReaction_*.c`. Каждая реакция — `[BaseContainerProps]` объект, который кладётся в `SCR_AIConfigComponent.m_aDangerReactions[]` (массив редактируется в конфиге фракции/архетипа). На входе:

```c++
override bool PerformReaction(
    SCR_AIUtilityComponent utility,
    SCR_AIThreatSystem threatSystem,
    AIDangerEvent dangerEvent,
    int dangerEventCount)
```

Возвращает `true`, если событие «использовано». Реакция обычно:
1. Считает задержку реакции из (threat state × distance × visibility) — см. `SCR_AIDangerReaction_GrenadeLanding` (`90 ms`..`2 s+1.1 s+0.9 s`).
2. Создаёт `Behavior` (например `SCR_AIMoveFromGrenadeBehavior` с reactionDelay) и кладёт его в utility через `utility.AddAction(behavior)`.
3. Поднимает counter в Threat system (BulletImpact / Explosion / ShotFired).
4. Опционально докидывает информацию в групповую перцепцию (`groupPerception.AddOrUpdateGunshot`).

Тик утилити: `m_Agent.GetDangerEventsCount()` → цикл `GetDangerEvent(i, count)` → `m_Config.PerformDangerReaction(...)` → `ClearDangerEvents()`.

---

## 5. Перцепция

### 5.1 Per‑agent
Движковый `PerceptionComponent` хранит `BaseTarget[]` с `LastSeenPosition`, `TraceFraction`, `GetTimeSinceSeen`. `SCR_AICombatComponent.EvaluateWeaponAndTarget` фильтрует:
- `TARGET_MAX_LAST_SEEN_VISIBLE = 0.5 s` — «вижу прямо сейчас»
- `TARGET_MAX_LAST_SEEN_DIRECT_ATTACK = 1.6 s` / `_CLOSE = 4.5 s`
- `TARGET_MIN/MAX_LAST_SEEN_INDIRECT_ATTACK = 2/7 s` (для МГ: до 14 с)
- `TARGET_MAX_LAST_SEEN = 11.0 s` — после этого цель «потеряна», поведение завершится.
- `TARGET_MAX_DISTANCE_VEHICLE = 700 m`
- `TARGET_SCORE_RETREAT = 75`, `TARGET_SCORE_HIGH_PRIORITY_ATTACK = 98.5`

Perception factor (множитель эффективной дальности зрения):
- SAFE / EQUIPMENT_NONE: 1.0
- VIGILANT/ALERTED: 2.5 (под адреналином видим лучше)
- THREATENED: 0.4 (под подавлением — хуже)
- BINOCULARS: 3.0

### 5.2 Per‑group — `SCR_AIGroupPerception` + кластеры
Группа агрегирует все индивидуальные `BaseTarget` своих членов в `m_aTargetClusters[]` через `SCR_AIGroupTargetClusterProcessor`. Кластеризация — **полярная**: для каждой цели сохраняется (angle, distance) от центра группы; смежные сектора объединяются.

```
TARGET_LOST_THRESHOLD_S    = 10.0  // если никто не видел 10с — потеряна
TARGET_FORGET_THRESHOLD_S  = 150.0 // через 150с — стирается из памяти
```

События кластеров: `OnEnemyDetectedFiltered` (раз за апдейт), `OnEnemyDetected` (per target), `OnNoEnemy`, `OnTargetClusterStateDeleted`. На них группа решает, какую Activity активировать (Attack/Defend/Investigate).

`m_MostDangerousCluster` — указатель на самый опасный, используется в Activity‑BT и для генерации Suppress‑приказов.

---

## 6. Combat Move — независимый под‑BT для боевого перемещения

`Soldier/HandleCombatMove.bt` крутится **параллельно** behavior‑BT, читает состояние из `SCR_AICombatMoveState`. Behavior лишь выставляет `m_bUseCombatMove=true` и желаемый "режим", а реальное «найти укрытие, дойти, выглянуть, отступить» делается сабтрии.

Запросы (`SCR_AICombatMoveRequest`):
- `Type`: MOVE, STOP, CHANGE_STANCE_IN_COVER, CHANGE_STANCE
- `Reason`: STANDARD, FF_AVOIDANCE, MOVE_FROM_TARGET, MOVE_FROM_DANGER, SUPPRESSED_IN_COVER, CHARACTER_AVOIDANCE
- `Direction`: FORWARD/BACKWARD/LEFT/RIGHT/ANYWHERE/CUSTOM_POS (относительно цели)
- `UnitType`: CHARACTER / GROUND_VEHICLE
- флаги `m_bAimAtTarget`, `m_bAimAtTargetEnd`
- callbacks `OnCompleted` / `OnFailed`

Логика разруливания: `SCR_AICombatMoveLogic_*`:
- `Attack` — поиск огневой позиции относительно врага
- `HideFromThreatSystem` — выбрать укрытие из сектора с высокой угрозой
- `HideFromUnknownFire` — реакция «откуда‑то стреляют»
- `MoveFromGrenade` — отход от точки гранаты
- `MoveFromIncomingVehicle` — увернуться от машины
- `MovingCommander` — держать строй при движении лидера

Покрытия: `Covers/SCR_AIFindCover.c` запрашивает `ChimeraCoverManagerComponent` (движковый менеджер cover‑слотов), фильтрует по `CoverQueryProperties` (направление, дальность, тип угрозы, navmesh‑агент). Параметры: `MAX_COVERS_HIGH_PRIORITY=25`, `MAX_COVERS_LOW_PRIORITY=15`, угол сектора 0.3π.

---

## 7. Communication — почта и приказы

`SCR_MailboxComponent` + `AICommunicationComponent` (движок).

Сообщения:
- `SCR_AIMessageGoal` — «цель/задача». Группа → юнит. Прилетают в утилити, `m_ConfigComponent.PerformGoalReaction(...)` создаёт соответствующий Behavior. Пример: WP_Defend.bt → `SCR_AISendGoalMessage_Defend` → у агента появляется `SCR_AIDefendBehavior`.
- `SCR_AIMessageInfo` — «информация» (увидел врага, услышал выстрел). Сначала пытаемся передать существующему Behavior (`CallActionsOnMessage`); если он не «съел» — `PerformInfoReaction`.
- `SCR_AIMessage_Cancel` — отменить Activity/Behavior.

Голосовые радио‑реплики: BT‑нода `SCR_AITalk` с `m_messageType ∈ {REPORT_DEFEND, REPORT_ENEMY, REPORT_HEAL, …}`, `m_ePreset ∈ {IMMEDIATE, NORMAL, DELAYED, …}`, `m_bSynchronous`.

---

## 8. Order graph — игрок и кампания

`AITaskCurrentOrder` достаёт «текущий приказ», `SCR_AIProcessOrder` парсит в (`OrderType`, `IsScriptedOrder`, `ScriptedOrderValue`). Затем огромный `Switch on EOrderType_Character` в `Soldier/HandleOrders.bt`: для каждого типа — последовательность нод (`SCR_AISetStance`, `SCR_AISetWeaponRaised`, `SCR_AICharacterSetMovementSpeed`, `SCR_AISetAIState`, `SCR_AIChangeUnitState`), потом `AITaskFinishOrder`.

Приказы — это «statefull инструкции», которые меняют **статичные параметры** агента (стойка, поднятое оружие, режим скорости, AIState). Реактивную часть всё равно делает utility.

---

## 9. Waypoints как очередь команд

`SCR_AIWaypoint` наследует движковый `AIWaypoint`. Содержит:
- `m_fPriorityLevel` (читается в Activity, передаётся в behaviors → влияет на `EvaluatePriorityLevel()`).
- `m_aSettings : array<ref SCR_AISettingBase>` — оверлей на конфиг пока активен ваяпоинт.
- `m_OnWaypointPropertiesChanged` — invoker для UI/AI.
- `CreateWaypointState(...)` — фабрика рантайм‑состояния, наследуется в подтипах.

Виды (`data/AI/BehaviorTrees/Waypoints/WP_*.bt`):
```
WP_Move        WP_Patrol         WP_Scout         WP_SearchAndDestroy
WP_Defend      WP_Attack         WP_Suppress      WP_Follow
WP_GetIn       WP_GetInNearest   WP_GetOut
WP_Heal        WP_EnactSmartAction   WP_Animate
WP_Blank       WP_Wait
```

Структура одного `WP_Defend.bt`:
```
Root → Sequence
 ├ SCR_AIGetDefendWaypointParameters(WaypointIn) → out: Radius, Origin, PriorityLevel,
 │     UseTurrets, SearchTags, FastInit, HoldingTime
 ├ SetVar(<flag>=true)
 ├ SCR_AISendGoalMessage_Defend(Receiver, PriorityLevel, IsWaypointRelated, ...)
 └ (висим в Running пока цель не завершит/cancel)
```
То есть waypoint — это **функция, которая разворачивается в goal‑messages по всем юнитам группы**, плюс держит счётчик завершения.

Подклассы: `SCR_DefendWaypoint`, `SCR_SearchAndDestroyWaypoint`, `SCR_SuppressWaypoint`, `SCR_TransportWaypoint`, `SCR_BoardingWaypoint`, `SCR_TimedWaypoint`, `SCR_DeploySmokeCoverWaypoint`, `SCR_LoadSuppliesWaypoint`, `SCR_UnloadSuppliesWaypoint`, `SCR_SuppliesTransferWaypoint`, `SCR_AIAnimationWaypoint`.

---

## 10. Smart Actions — переиспользуемые мини‑миссии

`data/AI/BehaviorTrees/SmartActions/SA_*.bt`:
```
SA_CaptureHQ      SA_CaptureRelay    SA_CoverPost   SA_GatePost
SA_LoiterPost     SA_ObservationPost SA_OpenGate    SA_Fireplace
SA_GiveFuelNozzle
```
Каждый SmartAction — отдельный BT, прикрепляемый к объекту мира (через `SCR_AISmartActionComponent`). Юнит «использует» smart‑action как точку с готовым сценарием поведения. WP_EnactSmartAction делегирует туда.

---

## 11. Settings overlay — модификаторы конфига по контексту

`SCR_AISettingBase` (массив прикреплён к waypoint, к Behavior через cause, к группе) умеет временно перекрывать значения `SCR_AIConfigComponent`. Скоупы определяются через `SCR_EAISettingFlags` и `cause` поведения (`SAFE/SELF_AID/GROUP_GOAL/DANGER_LOW/COMBAT/DANGER_MED/DANGER_HI/ALWAYS`).

Пример: stealth‑waypoint может выставить `m_EnableAttack=false`, и пока действует — солдаты пройдут мимо врагов не открывая огонь.

`SCR_AIConfigComponent` — буквально набор чекбоксов: `m_EnableMovement`, `m_EnableDangerEvents`, `m_EnablePerception`, `m_EnableAttack`, `m_EnableTakeCover`, `m_EnableLooking`, `m_EnableCommunication`, `m_EnableLeaderStop`, `m_EnableAimingError`, плюс реакции (`m_aDangerReactions`, `m_aGoalReactions`, `m_aInfoReactions`, `m_Reaction_*`), плюс конфиг оружия (`m_aWeaponTypeSelectionConfig`, `m_aWeaponTypeHandlingConfig`).

---

## 12. Fireteams и подформации

`SCR_AIGroupFireteamManager` (на группе) делит агентов на fireteam‑ы фиксированной формы; есть `SCR_AIGroupFireteam` (огневая группа), `SCR_AIGroupFireteamLock` (lock на пересборку), `SCR_AIGroupVehicle` (когда сидим в машине, формация подменяется). Лидер subformation определяется через `AIGroupMovementComponent.IsFormationDisplaced(handlerId)` — если строй разорван (например, игрок отстал), сабформация переходит «в автономный режим».

---

## 13. Архетипы и параметры скилла

`SCR_AICombatComponent.EAISkill = NONE/NOOB(10)/ROOKIE(20)/REGULAR(50)/VETERAN(70)/EXPERT(80)/CYLON(100)`. `SCR_AIConfigComponent.m_Skill ∈ [0,1]` — общий скилл. Воздействуют на ошибки прицеливания, время реакции, дальность обнаружения.

Роли (`EUnitRole` битовая маска): `RIFLEMAN, MEDIC, MACHINEGUNNER, AT_SPECIALIST, GRENADIER, SNIPER, HAS_SMOKE_GRENADE, HAS_FRAG_GRENADE`. Состояния (`EUnitState`): `WOUNDED, IN_TURRET, IN_VEHICLE, PILOT, UNCONSCIOUS`. AI‑состояния (`EUnitAIState`): `AVAILABLE / BUSY / UNRESPONSIVE`.

---

## 14. Узлы BT — конвенция именования

- **AITask\*** — engine‑native ноды (C++): `AITaskIdle`, `AITaskSetVariable`, `AITaskReturnState`, `AITaskCurrentOrder`, `AITaskFinishOrder`, `AITaskGetGroupChildren`, `AITaskSetPathfindingFilters`, `AITaskResetPathfindingFilters`.
- **SCR_AI\*** — scripted ноды (Enforce Script): `SCR_AIDecideActivity`, `SCR_AIDecideBehavior`, `SCR_AIProcessOrder`, `SCR_AIIsValidAction`, `SCR_AIUpdateExecutedAction`, `SCR_AISetStance`, `SCR_AISendGoalMessage_*`, `SCR_AIFindCover`, `SCR_AIFindTargetToLookAt`, `SCR_AITalk`, `SCR_AIPrintDebug`, …
- Composite: `Sequence`, `Selector`, `Parallel`, `RunOnce`, `Switch`, `RunBT` (sub‑tree call).
- Decorator: `DecoTestVariable`, `DecoTestOrder`, `DecoratorEntity`, `SCR_AIDecoIsGroupInitializing`, `SCR_AIDecoHasWeaponOfType`, `SCR_AIDecoCombatMove_*`.

`RunBT` — фактический полиморфизм: сам Behavior/Activity отдаёт строку с путём BT, родительский BT её крутит.

---

# Адаптация для project‑rts

Наш стек (`Ark ECS + Go`, `core/`, `components/`, `systems/`) уже имеет половину этих понятий — но они разрозненны. Reforger даёт цельный шаблон.

### 15.1 Какие зачатки уже есть

| Reforger | project‑rts |
|---|---|
| `SCR_AIThreatSystem` (4 вклада → 1 скаляр) | `components.Suppression{Level, ThreatDir}` — есть только подавление. Нет агрегата. |
| `EAIDangerEventType` + Reactions | `components.ThreatSource` (Phase 14) — есть «откуда стреляли», нет типизации события. |
| `BaseTarget` + `LastSeenPosition` | `components.Awareness.LastSeen [8]AwarenessEntry` — есть, ring buffer. |
| `SCR_AIGroupPerception` + clusters | **нет** — отсутствует уровень эскадрона для перцепции. |
| `AIWaypoint` + очередь | `components.ActionQueue [4]Action` (MoveTo/Stop/Stance) — минимальная очередь приказов. |
| Behavior‑дерево с приоритетом | **нет** утилити‑слоя; есть только direct ActionQueue. |
| Activity (group level) | **нет**. |
| `SCR_AICombatMoveState` + sub‑BT | `TacticalOverride` маркер уже есть → можно подвесить combat‑move sub‑logic. |
| `SCR_AIConfigComponent` (плагины реакций) | **нет**, но `PropTypeRegistry`/`OrderKind` spec‑table — близкая идея. |
| `EAIThreatState` SAFE/VIGILANT/ALERTED/THREATENED | **нет**, можно вывести из Suppression. |
| FireteamManager | `CommandRoster, FormationData` (Squad как невидимая сущность — есть, fireteam‑деления нет). |

### 15.2 Что брать в первую очередь (low risk, high value)

1. **Threat scalar [0..1] + 4 состояния** на каждом Unit‑е. Один float, 4 вклада с decay‑rate‑ами. Используется как gate в `WeaponSystem.shouldFire`, в `VisionSystem` (перцепция×factor), и как условие для перехода на cover‑logic. Минимальные изменения: расширить `Suppression` или ввести `Threat`.

2. **Типизированные `DangerEvent`** и реестр реакций. Сейчас Phase 14 спавнит `ThreatSource` без типа — добавить enum (`Gunshot/Explosion/GrenadeLanding/UnknownFire/MeleeHit/DamageTaken/Bleeding/VehicleHorn/UnsafeArea`) и pluggable handler‑ы. Уложить рядом со spec‑таблицами (`feedback_spec_table_pattern.md`).

3. **Utility‑слой над ActionQueue**. Сейчас ActionQueue — императивный. Над ним нужен `BehaviorPicker`:
   - Список Behavior‑кандидатов с приоритетом и условием активации.
   - Тот, кто прошёл условия и имеет максимальный priority — становится `CurrentBehavior` юнита, его «программа» заполняет ActionQueue.
   - Перевычисление по таймеру (как `UpdateInterval` из `SCR_AIDecideBehavior`), но не каждый тик.
   - Реализация на Go: либо набор Behavior‑структур с интерфейсом `Score(unit)→float, Build(unit)→[]Action`, либо stateful BT (если хотим визуальный редактор позже).
   Этого хватает, чтобы получить «реактивного бойца» без полноценных BT.

4. **Squad‑level Activity (групповой утилити‑слой)** — отдельный пиклер на CommandRoster. Читает кластер целей (то, что сейчас Awareness/Vision видит у членов) и выбирает: `Move`, `Defend`, `Attack`, `Suppress`, `Investigate`, `Heal`. Activity рассылает по членам goal‑message (= модифицирует их `LocalBlackboard` или прямо ставит `CurrentBehavior` override на нужное).

5. **Group target clusters** — простая полярная кластеризация Awareness‑записей всех членов в `SquadPerception`‑ресурс. Без неё Activity Defend/Attack не имеет «куда защищать/кого атаковать».

### 15.3 Что можно отложить

- **Cover sub‑BT.** В нашей архитектуре `CoverSlot`, `ShootingArc` уже эмиттятся зданиями (Phase 5), но реальный «отойти‑в‑укрытие‑если‑threat‑high» можно делать одним Behavior без вложенного BT — Reforger‑style sub‑tree даёт независимость, но это второй проход.
- **Smart Actions.** Полезно для зданий (`SA_ObservationPost` ≈ pillbox, `SA_GatePost` ≈ КПП), но это уже после того, как Defend/Move/Attack заработают.
- **Fireteam splitting.** У нас уже SquadId — добавить SubsquadId позже, по мере необходимости (если игроку понадобятся 2/3 крыла внутри одного отделения).
- **AI Skill / роли.** Можно сразу класть placeholder поле `Skill float32` в Unit, но не подключать к расчётам до Phase 19+.

### 15.4 Рекомендованный порядок (черновик фаз)

- **Phase A** (фундамент): `Threat` component с агрегатом 4 вкладов + state enum. `DangerEvent` enum + bus (заменить нынешний нетипизированный `ThreatSource`). Один централизованный `ReactionRegistry` (map[DangerType]ReactionFn).
- **Phase B**: `BehaviorPicker` система — выбирает `CurrentBehavior` юнита из набора Behavior‑структур. Минимум 5 behavior‑ов: Idle, MoveTo, Defend, AttackTarget, MoveFromDanger. Перевычисление через интервал, как в Reforger.
- **Phase C**: `SquadActivity` — групповой пиклер. `SquadPerception` ресурс с полярными кластерами целей. Activity‑ы посылают goal‑события юнитам, которые становятся приоритетным Behavior‑ом юнита (или модифицируют priority).
- **Phase D**: combat‑move как отдельная подсистема под `TacticalOverride` — выбор укрытий, suppressive movement, leaning. Здесь подключаются наши `CoverSlot`/`ShootingArc` smart‑object‑данные.
- **Phase E**: smart actions, fireteams, skill, ai settings overlay.

### 15.5 Принципы, которые стоит унаследовать

- **Одна сущность — одно текущее поведение.** Не пытаться смешивать несколько одновременно; параллельность только через ортогональные подсистемы (combat‑move, look, mailbox).
- **Параллельные «всегда‑на» подсистемы.** В Reforger Soldier.bt параллельно крутит: «найти на что посмотреть», «выбрать поведение», «обработать боевое перемещение», «обработать приказ», «проверить ammo». Это даёт декомпозицию без BT‑адобства.
- **Threat — единый общий вход.** Все реакции (задержка реакции на гранату, perception factor, выбор cover‑logic) масштабируются от одного скаляра. Соблазн делать 10 отдельных порогов — Reforger показывает, что один скаляр + 4 thresholds достаточно.
- **Activity diktiert Behavior, но не наоборот.** Юнит выбирает свой Behavior сам, исходя из своей утилитки; группа лишь подкидывает goal‑message с приоритетом, который входит в его утилитку. Никаких прямых «set behavior to X» через границу.
- **Реакции — данные, не код.** `m_aDangerReactions[]` в конфиге фракции — это `array<ref SCR_AIDangerReaction>`. Можно подменить весь набор для другой стороны (например, советские AI бегут от гранаты по‑другому). У нас аналог — spec‑таблицы по `OrderKind`, можно расширить на `DangerKind` / `BehaviorKind`.
- **BT‑ноды держат строки путей, а не классы.** `Behavior.m_sBehaviorTree = "Path/To.bt"`. Полиморфизм через данные. Если/когда у нас появится BT — наследовать тот же подход (BT‑файл как ассет).

### 15.6 Что НЕ повторять

- **Жёсткие float‑константы приоритетов** разбросаны по всему `SCR_AIAction.c` — Reforger‑ово прошлое, плохо масштабируется. У нас лучше: spec‑таблица `BehaviorSpec[Kind]` с `BasePriority` + модификаторы.
- **Глобальный `PerformGoalReaction` switch** по типу сообщения — портяночный код, к которому подкидывают `case` каждый раз. Лучше map[GoalKind]Reaction.
- **Магические `Update interval` в каждой ноде BT.** Reforger в каждом `AITaskIdle Period 0.3` ставит свой интервал; у нас уже есть `LODPolicy.RelevantEvery/DormantEvery` — пользоваться централизованным механизмом, не дублировать таймеры.
- **Engine `PerceptionComponent`** — у нас уже своя `VisionSystem`. Не тащить «черный ящик».

---

## Полезные точки входа для последующего чтения

- `data007/scripts/Game/AI/Behavior/SCR_AIAction.c` — список всех приоритетов одним файлом.
- `data007/scripts/Game/AI/Components/SCR_AIUtilityComponent.c::EvaluateBehavior` — главный цикл утилити.
- `data007/scripts/Game/AI/Components/SCR_AIThreatSystem.c` — реализация threat scalar.
- `data007/scripts/Game/AI/Components/SCR_AIConfigComponent.c` — что вообще можно настроить per‑archetype.
- `data007/scripts/Game/AI/Reaction/Danger/*.c` — образцы реакций на каждый тип события.
- `data007/scripts/Game/AI/Movement/SCR_AICombatMoveLogic_*.c` — образцы combat‑move logic.
- `data/AI/BehaviorTrees/Chimera/Soldier/Soldier.bt` и `Group/Group.bt` — корневые BT, точки входа.
- `data/AI/BehaviorTrees/Waypoints/WP_Defend.bt` — образец «waypoint раскладывается в goal‑messages».
