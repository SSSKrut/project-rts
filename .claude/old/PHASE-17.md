# Phase 17 - Tactical AI Wave 2 (рабочий план)

После Phase 14 у нас работает combat-core (стрельба, HP, suppression, RoE). После Phase 15 - реактивное движение (SurvivalInstinct, IndividualPosition, событийный лог). После Phase 16 - здания с этажами, дверями, окнами, cutaway-UX. Phase 16.5 даёт generator для тестовых building'ов.

Phase 17 закрывает разрыв между **"юниты движутся"** и **"юниты ведут себя как живые"**. До этой фазы:

- Юнит ходит по slot-offset'у формации. Если slot за стеной - бьётся в стену.
- Под огнём SurvivalInstinct ищет ближайший cover slot без понимания "укрытие должно быть между мной и врагом".
- В малое укрытие набивается 3-4 юнита, потому что Occupancy не enforced.
- Stance меняется только командой игрока. Юнит не ложится сам под пулями.
- Building generator выдаёт прямоугольники в 1-2 этажа без variation.

Все пять болей лечатся в одной фазе, потому что они **обмениваются одним фундаментом**: Threat scalar (агрегат угрозы) + DangerEvent (типизированные события) + per-unit pathing (MicroPath). Без них любая отдельная попытка fix'а ломается о то, что остальные компоненты живут в старой модели.

**Цель.** В конце фазы тест-сцена `cmd/tactical_sandbox`: один Office в 3 этажа с 4 дверьми, 5-юнитный Recon, 3-юнитная враг-команда у одной из дверей. Игрок отдаёт Garrison → видит как Recon заходит через дальнюю дверь (не ту что под огнём), занимает позиции у окон верхних этажей (не у тех что смотрят на врага только спиной), ложится под огнём из MG. Никто не бежит спиной 20 метров, никто не торчит за пустым местом, никто не залезает четвертым в куст.

ROADMAP §17 - порядок фаз и зависимости. CLAUDE.md "Tactical AI" + "Hierarchical AI" - архитектурные принципы. COMMAND-MODEL.md §5 (RoE) + §6 (Per-weapon intent) - readers уже работают, мы добавляем producer'ы. ARMA-REFORGER-AI.md - источник идей для Threat scalar / DangerEvent / Combat-move sub-system; берём паттерны, не реализацию.

---

## Структура фазы

Пять параллельных tracks, один фундамент:

- **17.0** Threat foundation - один скаляр на юнита + типизированные DangerEvent'ы. Снимает блокер для B/C.
- **17.A** MicroPath per-unit - каждый юнит сам строит микро-маршрут через `NavService`. Удаляет funnel-hack из Phase 16.B.1.d.
- **17.B** Cover behavior 2.0 - turn-then-run, cover validation против ThreatDir, capacity gate.
- **17.C** Stance autonomy - юнит сам ложится / встаёт на колено по Threat state.
- **17.D** Building generator Wave 2 - 3+ этажа, multi-wing, multi-entrance, interior subdivisions.
- **17.E** Group target clusters (опционально, можно отложить) - polar clustering Awareness'а членов в squad-level perception.

Порядок: 17.0 строго первый. После него A/B/C/D параллельно. E - финал, если останется время.

Track 17.A удаляет временный funnel-hack из FormationSystem (Phase 16.B.1.d). После M17.A.2 этот hack снимается.

---

## Track 17.0 - Threat foundation

### Решения, которые лочим

**0-P1. Threat - один float [0..1] + state enum.**

Текущий `Suppression{Level, ThreatDir}` (Phase 14) расширяется до:

```go
type Threat struct {
    Total       float32   // [0..1] агрегат
    Suppression float32   // пули рядом, decay 10%/s
    ShotsFired  float32   // далёкие выстрелы, decay 11%/s
    Endangered  float32   // на меня целятся (фикс 0.2 пока цель в фокусе)
    Injury      float32   // bleeding/wounded (фикс 0.3 пока эффект)
    ThreatDir   rl.Vector3  // направление основной угрозы (unit vec, threat→unit)
    State       ThreatState
}

type ThreatState uint8
const (
    ThreatSafe       ThreatState = iota  // Total < 0.05
    ThreatVigilant                       // 0.05..0.33
    ThreatAlerted                        // 0.33..0.66
    ThreatThreatened                     // > 0.66
)
```

Total пересчитывается раз в тик в `ThreatSystem` из четырёх вкладов + clamp [0,1]. Decay rates через `ThreatSpec`-таблицу.

**Почему один скаляр.** Reforger показывает, что 4 thresholds на один float покрывают весь spectrum реакций (perception factor, reaction delay, cover/stance trigger). Альтернатива "отдельная переменная на каждую реакцию" плодит магические числа без улучшения качества.

**0-P2. DangerEvent - типизированная событийная шина.**

`ThreatSource` (Phase 14) - one-shot entity без типа. Заменяем на типизированную модель:

```go
type DangerKind uint8
const (
    DangerGunshot       DangerKind = iota  // звук далёкого выстрела
    DangerBulletImpact                     // пуля прилетела рядом
    DangerExplosion                        // взрыв в радиусе
    DangerGrenadeLanding                   // граната упала и видна
    DangerUnknownFire                      // стреляют, но не знаю откуда
    DangerMeleeHit                         // ножевое
    DangerDamageTaken                      // получил урон
    DangerBleeding                         // активное кровотечение
    DangerVehicleHorn                      // машина гудит рядом
    DangerUnsafeArea                       // зона помечена опасной (artillery)
)

type DangerEvent struct {
    Kind     DangerKind
    Source   ecs.Entity     // кто/что вызвал (для tracker'а / KIA log)
    Pos      WorldPos       // где произошло
    Strength float32        // amplitude (расстояние, урон, hit-count)
    Time     float32        // squadService.Clock at emit
}
```

`DangerEvent` живёт **в per-unit ring buffer** (8 слотов), а не как отдельная entity. `WeaponSystem`/`DamageService` пишут события в targets' buffers - serial post-pass уже есть, в нём же добавляем `pushDangerEvent`. `ThreatSystem` каждый тик читает буфер юнита, увеличивает соответствующий contrib, гасит decay'ями, обновляет state.

**Почему ring buffer на юните, не глобальные entity:**
- Старая модель `ThreatSource` - per-event entity → archetype change → cache miss. Новая - inline массив фиксированного размера.
- Каждое событие читается ровно один раз (ThreatSystem). Глобальная entity была бы нужна если бы несколько consumer'ов хотели читать одно событие.
- ThreatSource entity всё ещё остаётся для **Phase 15 SurvivalInstinct** ("кластер активных угроз"), но это уже derived signal, не источник.

**0-P3. Spec-table для decay + thresholds.**

Все магические числа (decay rates, threshold values, contrib amplitudes) живут в `ThreatSpec` массиве, индексированном по `DangerKind`. Pattern из `feedback_spec_table_pattern.md`. Compile-time exhaustiveness через `SpecForDangerKind` switch.

Threshold'ы (`Safe/Vigilant/Alerted/Threatened`) - тоже spec'ы:

```go
var threatStateThresholds = [...]float32{
    ThreatVigilant:   0.05,
    ThreatAlerted:    0.33,
    ThreatThreatened: 0.66,
}
```

### Милстоны

- **M17.0.1** - `components.Threat` компонент + миграция `Suppression` (rename + расширение полей). Заглушка `ThreatSystem` (только Total = Suppression). Все callsite'ы Phase 14 (`shouldFire`, `propagateSuppression`, `pickTarget`) переписаны на `Threat.Suppression` + `Threat.ThreatDir` без изменения семантики.
- **M17.0.2** - `DangerEvent` ring buffer на юнита. `WeaponSystem` пишет `BulletImpact`/`Gunshot` события. `ThreatSystem` читает буфер, обновляет contrib'ы, гасит decay, пересчитывает State.
- **M17.0.3** - `ThreatSpec`-таблица + state thresholds. Migration `SurvivalInstinct` (Phase 15) на чтение `Threat.State` вместо raw `Suppression.Level`.
- **M17.0.4** - Tests: per-DangerKind contrib accumulation + decay shape. Один `_test.go` файл в `systems/`, без mock'ов мира - чистый pure-data round-trip через `ThreatSystem.tick(events, dt)`.

**Объём.** ~300-400 строк в `components/threat.go` + `systems/threat.go` + ~200 строк миграции callsite'ов. 3-4 дня.

---

## Track 17.A - MicroPath per-unit

### Решения, которые лочим

**A-P1. MicroPath - per-unit waypoint stream + LRU replan.**

```go
type MicroPath struct {
    Waypoints [16]WorldPos
    Head      uint8       // current waypoint index
    Count     uint8       // valid waypoint count
    GoalSnap  WorldPos    // goal'а на момент планирования - для replan-on-shift
    ReplanAt  float32     // earliest re-plan time (throttle)
    Dirty     bool        // queued for replan
}
```

`MicroPathSystem` каждый тик:

1. Для каждого юнита с `MicroPath.Dirty == true` - запрос `NavService.FindPath(unit.pos, goal)`, decimate, заполнить Waypoints, `Dirty = false`.
2. Для каждого юнита **без** `Dirty`, но с `Head < Count`:
   - Если до текущего waypoint < arrivalRadius - `Head++`.
   - Если новый goal (`ActionQueue.Head.Target`) уехал > 2 м от `GoalSnap` - `Dirty = true`.
3. Throttle: max N replan'ов за тик через worker pool budget.

`UnitMovement` берёт текущий waypoint вместо `ActionQueue.Head.Target` как short-term цель. Если `MicroPath.Count == 0` (fresh unit) или `Head >= Count` - fallback на `ActionQueue.Head.Target` (прямая линия, как сейчас).

**Почему 16 waypoints.** Decimated A* path на 60-метровом маршруте даёт ~10-12 waypoints. 16 - запас + headroom для будущих локальных корректировок (cover-via, kneel-then-run).

**A-P2. FormationSystem пишет в `ActionQueue.Head.Target`, MicroPath строит маршрут.**

Сейчас FormationSystem делает `ClearActions + PushAction(MoveTo target)` напрямую. Меняем: FormationSystem пишет в `ActionQueue.Head.Target`, **plus** ставит `MicroPath.Dirty = true` если target изменился. `UnitMovement` читает из MicroPath.

**A-P3. Leader-wake bias.**

Slot members (i > 0) получают цель не как raw `center + offset`, а как:

```go
// leader's path: roster.Members[0].MicroPath.Waypoints[0..Count]
// slot's lateral offset: from FormationOffset(kind, i, spacing, forward)
// effective goal: leader.Waypoint[k] + lateral_offset, where k = min(i, leader.Count-1)
```

Это даёт "следование по следу лидера" - slot 4 не выбирает свой обходной маршрут, он идёт за лидером с боковым offset'ом. Когда коридор узкий (дверь) - lateral_offset проецируется на нулевую перпендикулярность, юниты выстраиваются в column. Когда коридор широкий - lateral_offset восстанавливается.

**Почему важно.** Без bias 5 юнитов выбирают 5 разных маршрутов вокруг здания - один с севера, второй с юга, третий через парк. Выглядит хаотично.

**A-P4. Удаление funnel-hack из Phase 16.B.1.d.**

`FormationSystem.processSquad` сейчас содержит block `case centerInside && !memberInside && i != 0` с `lastOutsideWaypoint`. После M17.A.2 этот path удаляется - MicroPath сам построит маршрут через дверь.

### Милстоны

- **M17.A.1** - `MicroPath` компонент + `MicroPathSystem` skeleton. `UnitMovement` reads MicroPath when Count > 0. Один юнит с hand-set MicroPath в test scene - визуальная проверка.
- **M17.A.2** - FormationSystem интегрирован: пишет `ActionQueue.Head.Target` + ставит `MicroPath.Dirty`. Funnel-hack удалён. Door test scene должен дать PASS 5/5 < 8с (vs 12с с hack'ом).
- **M17.A.3** - Leader-wake bias. Test: 5 юнитов в формации идут через дверь - визуально как column, не как 5 параллельных маршрутов.
- **M17.A.4** - Throttle + budget. `pathBudgetPerTick = NumCPU * 8`. Замеры через Phase 7.5 profiler: `micro_path` система median ms vs unit count.
- **M17.A.5** - Stuck detection. Юнит, который не движется > 1с с непустым MicroPath - `Dirty = true` + увеличить penalty для последнего сегмента (anti-thrashing).

**Объём.** ~600-800 строк в `systems/micro_path.go` + `components/micro_path.go` + 200 строк правок в `unit_movement.go`/`formation.go`. 6-8 дней.

---

## Track 17.B - Cover behavior 2.0

Зависит от 17.0 (читает `Threat.State` + `ThreatDir`). Опционально использует 17.A для движения к cover (через MicroPath, не straight-line).

### Решения, которые лочим

**B-P1. Cover validation: dot(CoverDir, ThreatDir) gate.**

`CoverSlotIndex.QueryNearbyCovers(unit, threatDir, limit)` фильтрует слоты:

```go
// CoverDirection.Dir - outward normal от cover (= в сторону открытой стороны).
// ThreatDir - unit vector threat→unit.
// Cover защищает от угрозы со стороны -CoverDir.
// Если threatDir и coverDir смотрят в одну сторону - cover за спиной у unit'а
// относительно угрозы. Reject.
if Dot(slot.CoverDir, threatDir) > -0.3 {
    continue
}
```

`-0.3` (а не `0`) - небольшой tolerance: cover может быть боком, важно что не за спиной.

**B-P2. Approach-cone scoring.**

Score(slot) = (cover_quality × 100) - (distance × 5) - (angle_penalty × 30) - (capacity_penalty × 50).

- `cover_quality` - из `PropTypeRegistry[propType].CoverEffectiveness` (1.0 для стены, 0.7 для камня, 0.3 для куста).
- `distance` - метры до slot'а.
- `angle_penalty` - угол между направлением "куда стоит бежать" (= `unit.Forward` если не под огнём, или `-threatDir` если бежим от угрозы) и `dir_to_slot`. Slot позади - penalty высокий, юниту пришлось бы бежать спиной.
- `capacity_penalty` - +50 если slot уже occupied (см. B-P3); +1000 если at cap.

**B-P3. Capacity gate.**

`Occupancy.Current` уже в компоненте, но никто не пишет. Добавляем:

- `SurvivalInstinctSystem` при выборе slot'а инкрементит `Occupancy.Current`. Запоминает выбор в `LocalBlackboard.AssignedCoverSlot ecs.Entity`.
- При смене slot / cancel override / unit death - декремент.
- При query: slot at cap (`Current >= Max`) выпадает из top-N (через большой penalty), но не reject полностью - может стать last resort если других нет.

**Threading.** Phase 15 SurvivalInstinct запускается serial (один проход), reserve-and-claim в одном проходе безопасен. Если когда-то параллелим - atomic counter.

**B-P4. CombatMove: turn-then-run.**

Разделение Yaw на `FacingYaw` (куда смотрит) и `VelocityYaw` (куда движется). Поведение по `Threat.State`:

- `Safe/Vigilant` - `FacingYaw == VelocityYaw` (смотрим куда идём, как сейчас).
- `Alerted/Threatened` - `FacingYaw = atan2(-threatDir.X, -threatDir.Z)` (смотрим на угрозу, идём куда нужно).
- При начале движения под `Threatened`: если `|VelocityYaw - FacingYaw| > 90°` и distance > 3 м - сначала поворачиваем тело (`FacingYaw → VelocityYaw` за 0.3 с), потом полная скорость. < 3 м - разрешаем shuffle backward (отступить шагом не разворачиваясь).
- При прибытии в cover - `FacingYaw` восстанавливается на `-threatDir` (лицом к угрозе).

`Motion` компонент расширяется: `Yaw float32` → `FacingYaw, VelocityYaw, TargetFacingYaw float32`. Animation lock interpolator (`Motion.YawLerpRate = 4 rad/s` по умолчанию).

**B-P5. Movement к cover через MicroPath (если 17.A landed).**

SurvivalInstinct ставит slot pos как `ActionQueue.Head.Target` + поднимает `MicroPath.Dirty`. Юнит идёт через A*, не straight-line. Если 17.A ещё не landed - fallback на straight-line.

### Милстоны

- **B.1** Cover validation gate (`dot(CoverDir, threatDir)` reject). Test: одиночный юнит, враг с севера, рядом 2 стены (одна с севера, другая с юга). Юнит должен выбрать южную (между ним и врагом), не северную (за спиной).
- **B.2** Approach-cone scoring. Test: юнит с врагом фронтально, два равноценных cover'а - один впереди, один сзади. Выбирает впереди.
- **B.3** Capacity gate. Test: один маленький куст (Occupancy.Max=1), 5 юнитов под огнём. Один залезает в куст, остальные ищут другие cover'ы. Никаких 5-в-одном.
- **B.4** CombatMove turn-then-run. Test: юнит лицом на север, граната летит с севера, нужно отступить на юг. Юнит сначала поворачивается, потом бежит лицом вперёд - не пятится спиной 5 метров.
- **B.5** Cover-via-MicroPath integration (depends on 17.A). Test: юнит в коридоре с открытой дверью, враг с другой стороны коридора. Юнит идёт к cover через дверь (не сквозь стену).

**Объём.** ~400-500 строк в `systems/cover_select.go` + правки в SurvivalInstinct + правки в Motion/UnitMovement. 5-7 дней.

---

## Track 17.C - Stance autonomy

Зависит от 17.0 (читает `Threat.State`).

### Решения, которые лочим

**C-P1. StanceController как часть SurvivalInstinct или отдельная система?**

Lock: **отдельная система** `StanceControllerSystem`. Reason: SurvivalInstinct уже работает с cover slot lifecycle (reserve/release), смешивать stance logic усложнит. Stance - простой mapping `Threat.State → target stance` с animation lock.

**C-P2. State → Stance mapping.**

```go
switch threat.State {
case ThreatThreatened:
    target = StanceProne
case ThreatAlerted:
    target = StanceCrouch
default: // Safe, Vigilant
    target = StanceStand
}
```

Полное движение → отмена prone (нельзя ползти быстро, lock'ит motion). Если юнит должен двигаться > 3 м/с - принудительно `StanceStand` или `StanceCrouch`, prone снимается.

**C-P3. Player override.**

Player command (Z/X/C, если Phase 21 их добавит) - вешает `StanceOverride{Until float32}` маркер на 10 секунд. Пока маркер активен - StanceController skip. После - снимается, autonomous control возвращается.

**C-P4. Animation lock между переходами.**

`Stance.LockUntil float32` - не разрешать смену чаще раз в 0.5 с. Защита от prone↔stand↔prone каждый тик.

### Милстоны

- **C.1** `StanceControllerSystem` + `StanceOverride` marker. Mapping `Threat.State → Stance`. Animation lock 0.5 с.
- **C.2** Movement gate: prone snap to crouch когда target speed > 1.5 м/с. Test: юнит под огнём ложится; игрок отдаёт MoveTo - юнит встаёт на колено перед началом движения.
- **C.3** Phase 7 `Vision.AngleDot` интеграция: prone узкий cone (cos 30°), crouch средний (cos 60°), stand широкий (cos 90°). Уже частично там; уточнить таблицу.

**Объём.** ~150-200 строк. 2-3 дня.

---

## Track 17.D - Building generator Wave 2

Независимо от других tracks, кроме того что Compound buildings разумно тестировать только с MicroPath (17.A).

### Решения, которые лочим

**D-P1. HouseParams расширяется до 5 этажей + lookup.**

```go
type HouseParams struct {
    Stories  uint8       // 1..5
    SizeX, SizeZ float32
    DoorSides []uint8    // [0..3] sides with doors, default [0]
    Wings    []WingSpec  // empty = simple rectangle
    InteriorWalls bool   // subdivide each Wing into rooms
}

type WingSpec struct {
    OffsetX, OffsetZ float32   // relative to main center
    SizeX, SizeZ     float32
    Stories          uint8     // can differ from main
    ConnectVia       ConnectType  // Passage (no door) | Door | Corridor
}
```

**D-P2. Multi-story stairs - cascade.**

Прямая лестница (`StairsLength=4`) + `BunkerDepth=3` работает для 2 этажей. Для 3+ - cascade: лестница из 2 секций с площадкой посередине, разворот на 180°. Footprint меньше (тот же 4×2 м), высота больше.

Новый template `AddCascadeStair(local, fromLevel, toLevel)` в `building_gen/builder.go`. Внутри cascade - 2 `Stairs` entity со связкой через `LevelTransition.Mid` (промежуточная площадка как half-level).

**D-P3. Interior walls + Room concept.**

Внутри Wing появляются `InteriorWall` сегменты. Деление Level на rooms - **виртуальное**: новый компонент `Room{ParentLevel, AABB, Name}`, не отдельная иерархия. Cover slots / fire arcs читают Room AABB вместо Level AABB для local-context. Phase 17 `Room` минимальный (просто маркировка); полноценная room-aware AI - Phase 21+.

**D-P4. Multi-entrance.**

`DoorSides []uint8` вместо одного `DoorSide`. Builder spawn'ит door на каждой указанной стороне. Spawn-locations distinct, randomized по seed.

Compound template - explicit door на каждом building'е (4 здания × 2 двери = 8 entrances). Garrison resolver уже выбирает first-floor pos здания; Phase 17 Garrison не нужно специально расширять.

**D-P5. New templates: Office, Compound.**

```go
// systems/building_gen/office.go
func GenerateOffice(seed, params, pos, kind) *BuildingPlan {
    // 3 stories, central stair, 2-3 rooms per floor via InteriorWalls,
    // 1 main entrance (south side), windows on all external walls.
}

// systems/building_gen/compound.go
func GenerateCompound(seed, params, pos, kind) *BuildingPlan {
    // 3-5 connected wings via Passage, courtyard in the middle,
    // 1 entrance per wing on exterior.
}
```

Sandbox (`cmd/building_sandbox/`) picker UI добавляет 2 новых template'а к существующему House. Hotkey-overlays Phase 16.5 (L/W/S/A/C/D/F/V/G) работают без изменений.

### Милстоны

- **D.1** `HouseParams.Stories: 1..5` + cascade stairs. Test: House 5 этажей, лестница идёт нормально, NavGrid baked корректно.
- **D.2** `Wings []WingSpec` + Passage connectors. Test: L-shape, U-shape - визуально + walkthrough nav.
- **D.3** Interior walls + Room concept. Test: House с 4 комнатами на этаж - стены видимы, rooms помечены.
- **D.4** Multi-entrance (`DoorSides`). Test: House с 4 дверьми - все entrances работают, Garrison resolver выбирает first-floor pos.
- **D.5** `GenerateOffice` template + sandbox picker.
- **D.6** `GenerateCompound` template + sandbox picker.

**Объём.** ~600-800 строк в `building_gen/` + sandbox UX updates. 4-6 дней.

---

## Track 17.E - Group target clusters (опционально)

Если в практике M17.0/B показывает что `Threat.ThreatDir` шатается при multi-threat - закрываем 17.E. Иначе откладываем в Phase 19.

### Решения, которые лочим

**E-P1. SquadPerception resource.**

Аналог Reforger'овского `SCR_AIGroupPerception`. Per-squad polar clustering of members' Awareness entries.

```go
type TargetCluster struct {
    CenterAngle  float32   // от squad center, [0..2π]
    AngleSpread  float32   // half-angle
    AvgDistance  float32
    Members      [8]ecs.Entity   // contributing enemy targets
    Count        uint8
    LastSeenAt   float32
}

type SquadPerception struct {
    Squad    ecs.Entity
    Clusters [4]TargetCluster
    Count    uint8
}
```

Aggregator система запускается каждые 500 мс (Active tier), walks members' `Awareness.LastSeen` rings, кластеризует по polar coordinates от squad center, заполняет `SquadPerception`.

**E-P2. Reader integration.**

- `SurvivalInstinct` - `ThreatDir` для cover scoring читается из `SquadPerception.MostDangerousCluster` (если есть несколько кластеров, выбираем по weighted score: distance × member_count).
- `AttackTarget` resolver - target.Pos может interpolate'ся в centroid кластера, не attached к одной entity.

### Милстоны

- **E.1** `SquadPerception` resource + aggregator система.
- **E.2** SurvivalInstinct migration на чтение из кластеров.

**Объём.** ~300 строк, 3-4 дня. Если не успеваем - Phase 19.

---

## Зависимости и порядок

```
17.0 ──┬── 17.A ──┬── 17.B
       │         │
       ├── 17.C  │
       │         │
       └── 17.E (опционально, после 17.0)

17.D - параллельно всем, не блокирует ничего
```

Старт: M17.0.1 (Threat rename). После M17.0.3 - можно начинать любой из A/B/C/D параллельно.

Phase 17 closure criteria - в Open Questions ниже.

---

## Что переносится из старого Phase 17

Старый Phase 17 ("Visual fidelity 1") переезжает в **Phase 17.5**. Содержимое не меняется - models, textures, road splines, biome map overlays, map object icons. Зависимость Phase 17.5 → Phase 16 (AssetRegistry) уже выполнена. Зависимость Phase 17.5 → Phase 17 (tactical AI Wave 2) - визуальный pass осмыслен только когда юниты ведут себя сносно.

ROADMAP.md обновляется: `Phase 17` description меняется на Tactical AI Wave 2, новая `Phase 17.5` секция = старый текст.

---

## Открытые вопросы

1. **Threat.ThreatDir aggregation.** Сейчас `Suppression.ThreatDir` пишется последним `BulletImpact`. Под огнём с двух сторон direction шатается. Lock: M17.0.2 пишет ThreatDir как weighted average по recent DangerEvents (weight = `recency × strength`). Если это работает достаточно стабильно - 17.E ждёт Phase 19.

2. **Cover slot reservation vs prediction.** Если юнит A зарезервировал slot, юнит B не видит этот slot. Что если A умирает в пути? B уже выбрал другой slot - не перевыбирает. Acceptable первая итерация. Phase 21 polish - re-evaluate cover_slot каждые 5с.

3. **MicroPath cost на 300+ юнитов.** Phase 25 целит на 300 active units. 0.5 Hz path-refresh × 300 = 150 calls/сек. NavService.FindPath на 60м path ~ 0.5-2 мс. Total 75-300 мс/сек. Это 1-5% CPU. Acceptable, но если потолок surface NavGrid (64×64) поднимется - может стать узким местом. Phase 25 budget review.

4. **CombatMove FacingYaw vs aim direction.** Игрок может хотеть "идти лицом туда, целиться туда". В Phase 17 lock: FacingYaw = aim direction (= `-threatDir` под угрозой, `velocityYaw` иначе). Phase 21+ может ввести `AimYaw` отдельно от FacingYaw (солдат смотрит на улицу, но винтовка направлена в сторону).

5. **Stance vs movement speed gate.** Lock: prone forbids motion > 1.5 м/с. Snap to crouch при попытке двигаться быстрее. Альтернатива - "crawl на 1 м/с" - polish для Phase 21+.

6. **Building Wave 2 + multi-chunk constraint.** Phase 5 lifted multi-chunk constraint в Phase 16.B.1.b. Compound из 5 wings на 30×30 м площадке - может пересекать 2-4 чанка. Test scenario: walk past compound, walk back. Все wings должны re-spawn.

7. **Compound LoD / culling.** Большое здание имеет много children (walls + furniture + markers). LoD на children через `LODRelevant`, но 5-wing compound с 100+ children на каждом wing - 500+ children visible. Phase 17 polish: hierarchical LOD - root building entity carries an "LoD bucket" tag; children within radius render fully, beyond radius - just exterior shell.

8. **17.E aggregation cost.** Polar clustering 8 awareness entries × N members × every 500ms - cheap. Worst case 8 squads × 8 members × 8 entries = 512 ops/сек. Acceptable.

9. **Player override duration for Stance.** Lock 10 с. Альтернатива - до new order. Phase 21+ может сделать настраиваемым.

10. **Leader-wake bias degenerate cases.** Лидер мёртв - бывшие slot'ы становятся самостоятельными. Lock: при leader death FormationSystem promote slot[1] в leader (`roster.Members[0] = old slot 1`), bias восстанавливается.

11. **Bigger buildings + map representation.** Большие compound'ы должны быть видимы на 2D-карте. Phase 17.5 (map object icons) делает это. Без него - в Phase 17 sandbox только.

12. **Funnel-hack removal regression test.** После M17.A.2 удаления funnel-hack - door test scene (5-юнитный Recon в 8×8 house) должен PASS < 8с. Если фейлится - rollback и debug.

---

## Closure criteria (для phase 17 как целого)

Сцена `cmd/tactical_sandbox/main.go` (новый бинарь, аналог `cmd/building_sandbox`):

- 1 Office из GenerateOffice (3 этажа, 2 двери).
- 1 Compound из GenerateCompound (3 wings).
- 1 Recon squad (5 человек, player faction).
- 1 MotorRifle squad (8 человек, enemy faction) с DefendPosition у Office.

Player order: Garrison(Office).

Pass criteria:
- ✅ Все 5 Recon членов внутри Office за < 15 с после order.
- ✅ Ни один не идёт через дверь под прямым огнём MG (выбирают дальнюю / боковую дверь).
- ✅ Под огнём - 3+ переходят в Crouch, 1+ в Prone, остальные Stand за cover.
- ✅ Cover slot pile-up = 0 (никакого "4 на куст").
- ✅ Cover-behind-back = 0 (юнит не торчит за стеной которая между ним и нами).
- ✅ Profiler `micro_path`/`threat`/`cover_select` суммарно < 8 мс median tick на 50 active units.

---

## Полезные ссылки

- `.claude/ARMA-REFORGER-AI.md` - источник идей для Threat / DangerEvent / Combat-move. Берём паттерны (один скаляр, типизированные события, sub-system для combat move), не реализацию (BT, hard-coded float priorities).
- `.claude/COMMAND-MODEL.md` §5 (RoE), §6 (Per-weapon intent) - consumers готовы, мы только улучшаем producer'ы.
- `.claude/GAMEDESIGN.md` §5 (Movement), §7 (RoE) - design constraints.
- `systems/spatial_bake.go` - cover slot generation. Phase 17 не меняет generation, только consumer (cover_select).
- `systems/formation.go` - funnel-hack в M17.A.2 удаляется. Leader-wake bias в M17.A.3 добавляется.
- `systems/survival_instinct.go` (Phase 15) - читатель `Threat.State` после M17.0.3.
