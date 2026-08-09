# Phase 7 — рабочий план

Базовые юниты. Пехотинец как набор мелких компонентов (Spatial / Senso-Cognitive / Execution / Equipment), оружие как отдельные сущности с `OwnedBy`. Поверх Phase 6 NavGrid'а строится unit-движение со steering'ом. Multi-floor навигация (per-floor NavGrid + transitions через двери / лестницы) — приходит сюда из Phase 6. Селекция юнитов мышью + приказы через ПКМ — отдаём приоритет групповым приказам, потому что это базис Phase 9 (Squads + formations).

**Чего в этой фазе сознательно НЕТ.** Combat / стрельба / урон — Phase 11. Persistent Order-сущности с RadioNetwork-зависимым masking'ом — Phase 12. Полные Boids (alignment / cohesion) — Phase 9. Direct-control «взять бойца за плечо» — отказ пользователя, не реализуем; всё через приказы. Tactical AI (выбор укрытий, fight-or-flight) — Phase 10. Squad как отдельная сущность с CommandRoster / FormationData / RadioNetwork — Phase 9.

ROADMAP — высокоуровневый трекер. Этот файл — рабочий, обновляется по ходу выполнения.

---

## Решения, которые лочим до начала кода

**P1. Unit — композиция мелких компонентов + маркер.**

Никакого монолитного `Unit`-компонента. Стандартный набор сущности-пехотинца:

```go
// Marker (filter-target)
type Unit struct{}

// Spatial — где, как стоит, как движется
type Stance     struct{ Code StanceCode }      // см. P5
type Motion     struct{ Yaw, Speed float32 }   // facing + текущая скорость
type Collider   struct{ Radius float32 }       // XZ; высота берётся из Stance-таблицы

// Senso-Cognitive — что видит, насколько подавлен, что знает
type Vision     struct{ RangeM, AngleDot float32 }
type Suppression struct{ Level float32; ThreatDir rl.Vector3 }
type Awareness  struct{
    LastSeen [8]AwarenessEntry   // fixed-size, не slice (DOD)
}
type AwarenessEntry struct{
    Target ecs.Entity
    Pos    components.WorldPos
    Time   float32   // session time of last sighting; 0 = пусто
}

// Execution — что юнит сейчас делает / чем занят
type ActionQueue struct{ Actions [4]Action; Head, Tail uint8 }   // ring buffer
type Action      struct{ Kind ActionKind; Target rl.Vector3 }
type LocalBlackboard struct{ /* пусто в Phase 7; scratch для Phase 10 */ }

// Equipment — что носит. ID-ссылки на сущности оружия, никаких массивов в компоненте.
type Equipment struct{
    Primary, Secondary, Active ecs.Entity   // 0 = пусто
}
```

Юнит-сущность = `WorldPos + Unit + Stance + Motion + Collider + Vision + Suppression + Awareness + ActionQueue + LocalBlackboard + Equipment + LODRelevant`. Двенадцать компонентов на сущности — это нормально для Ark, архетип фиксирован.

Альтернатива «один компонент с вложенной структурой» отвергнута явным правилом из CLAUDE.md (no monolithic Unit). Раздельные компоненты дают: разные системы читают разные подмножества (Vision система НЕ грузит Equipment в кэш), разные частоты обновления (Stance меняется редко, Motion — каждый тик), и архетип-фильтрация через `Without` для исключений (например, `Without[Suppression]` для cover-spawnpoint dummy-юнитов в будущих фазах).

**P2. Multi-floor — per-Floor NavGrid'ы (Q1 ответ).**

Surface NavGrid из Phase 6 живёт как раньше — на сущности чанка. Поверх него:

```go
const MaxFloorSide = 32  // 32×32 клетки = 32×32 м footprint, перекрывает все placeholder-здания

type FloorNavGrid struct {
    SizeX, SizeZ uint8                                 // фактический размер в клетках
    Origin       rl.Vector3                            // chunk-local координата клетки (0,0)
    Cells        [MaxFloorSide * MaxFloorSide]NavCell  // полный фикс-массив, 1 KB
}
```

Привязывается компонентом к `Floor`-сущности (Phase 5), бэйкается в `SpatialBakeSystem` (расширяем существующий из Phase 6) одновременно с растеризацией стен этого этажа.

Bake-логика для FloorNavGrid: footprint этажа (`Floor.SizeX × SizeZ`) делится на 1×1 м клетки. Стены этого этажа (через `BuildingChildIndex` → выбираем walls с `WorldPos.Y` соответствующим уровню этажа) растеризуются как `Cost = 0`. Открытые двери — проёмы. Окна — `Cost = 0` (даже на первом этаже окно непроходимо). Закрытые двери — `Cost = 0`. Slope не считается (этаж — плоскость).

**Почему вариант Б, а не общий 3D-grid'ы на чанке.** Если юниту вообще не надо заходить в дом, мы не платим bake'ом / памятью / A*-обходом за «уровни здания». Floor footprint типично 8-12 м — 64-144 клетки, на порядок меньше полного chunk-slice'а. И многоэтажное здание с пустым полем над ним не накапливает мёртвую массу. Ценой более сложного A* (P3) и того, что в будущем (Phase 8/9/10) могут понадобиться разные стратегии переходов — это записано в Заметки на полях.

**P3. Multi-graph A* через `TransitionRegistry` resource.**

Нав-узел теперь не просто `(cc, i, j)` — он tagged:

```go
type NavNodeKind uint8

const (
    NodeSurface NavNodeKind = iota   // surface NavGrid на чанке
    NodeFloor                        // FloorNavGrid на Floor-сущности
)

type NavNode struct {
    Kind  NavNodeKind
    Chunk components.ChunkCoord  // используется только для NodeSurface
    Floor ecs.Entity             // используется только для NodeFloor
    I, J  int16                  // локальный индекс клетки
}
```

`TransitionRegistry` — singleton-ресурс, заполняется бэйкером:

```go
type TransitionEdge struct {
    From, To NavNode
    Cost     uint8         // переход стоит N (типично 2-4); закрытая дверь = 0 (не проходим)
    Owner    ecs.Entity    // Door / Stairs entity, для invalidation в Phase 11
}

type TransitionRegistry struct {
    Out map[NavNode][]TransitionEdge   // для каждого узла — исходящие переходы
}
```

Источники edge'ов:
- **Door (open)**: 1 edge между surface-узлом снаружи здания (или floor-узлом верхнего этажа, если дверь на 2-й этаж к лестнице) и floor-узлом внутри. Bidirectional — пишем оба.
- **Door (closed)**: edge с `Cost = 0` (≡ непроходимо). Перерасчёт при смене `Door.State` — Phase 12 (когда юниты будут уметь открывать двери); в Phase 7 двери всегда «как заспаунились».
- **Stairs**: 1 edge между floor-узлом этажа `FromFloor` и floor-узлом этажа `ToFloor`. Bunker-вход — Stairs соединяющий surface-узел снаружи и floor-узел сунутого этажа.

A*-расширение узла даёт 8 локальных соседей (внутри своего grid'а) + `TransitionRegistry.Out[node]` (внешние переходы). Heuristic — Chebyshev в **world space** (через `WorldPos` обоих узлов), не в индексной системе — потому что Floor и Surface — разные системы координат.

Phase 6 API контракт сохраняется: `NavService.FindPath(from, to WorldPos)`. Резолвер «какой узел соответствует данному WorldPos»:
1. Если WorldPos попадает в footprint какого-то Floor (сущности `Floor + WorldPos`), и `WorldPos.Y` близок к уровню этажа — `NodeFloor`.
2. Иначе — `NodeSurface` через `(chunk, i, j)` от WorldPos.

**P4. Steering — только separation force (Q2 ответ).**

`UnitMovementSystem` каждый тик для каждого юнита с непустым `ActionQueue` и текущим `MoveTo`-action'ом:
1. Из `ActionQueue.Head` достаём текущий `MoveTo.Target`.
2. Computes `desired = (Target - Pos).Normalize() * MaxSpeed[Stance]`.
3. **Separation**: для всех юнитов в радиусе 1.5 м — push-вектор `(myPos - theirPos) / dist²`, суммируется.
4. `velocity = desired + separationWeight * separation`.
5. Y компонента — нулевая; `GroundStickSystem` (P11) кладёт Y по поверхности.
6. Twist Yaw в `velocity.Atan2`.

Никакого alignment / cohesion. Группы юнитов идущих в одну точку — рассыпаются в облако вокруг цели, не строят клин. **Phase 9 (Squad + Formations) добавит формацию: SquadMacroPath даст центр отряда, FormationSystem распишет offset'ы каждому бойцу, Boids alignment склеит траектории.** В Phase 7 это отсутствует осознанно — мы делаем общий вид движения, форму даст следующая фаза.

`MaxSpeed[Stance]` — таблица:
```
Standing:  5.0 m/s
Crouching: 3.0 m/s
Prone:     1.5 m/s
```

**P5. `StanceCode` — типизированный uint8, не строгий enum (Q4 уточнение).**

```go
type StanceCode uint8

const (
    StanceStand StanceCode = iota
    StanceCrouch
    StanceProne
    // запас на будущее: cover-mounted, vehicle-mounted, drag-injured, sprint, etc.
)
```

Сейчас три значения. **Если/когда в Phase 10 появятся модификаторы** (`InCover`, `Suppressed`, `AimingDownSights`), переезжаем на структуру:
```go
type Stance struct {
    Posture   StanceCode    // что было раньше
    Modifiers StanceModifiers   // битовая маска: cover, suppressed, aiming, ...
}
```

Это **переименование поля** в существующей сущности юнита: код, читающий `unit.Stance.Code`, превращается в `unit.Stance.Posture`. Никто из Phase 7 систем не пишет в Modifiers, так что миграция тривиальна. Сейчас закладываем простой тип, не предугадываем форму.

**P6. Weapon — отдельная сущность с `OwnedBy`.**

```go
type OwnedBy struct{ Owner ecs.Entity }

type WeaponKind uint16

const (
    WeaponAK47 WeaponKind = iota
    // etc.; в Phase 7 один тип — placeholder для P15 test scene
)

type Weapon struct {
    Kind     WeaponKind
    Ammo     uint16
    RangeM   float32
    RoF      float32   // shots per second
    Damage   uint16
}
```

Сущность оружия: `Weapon + OwnedBy{owner=unit} + WorldPos` (WorldPos = текущая позиция владельца + смещение, обновляется системой `EquipmentSyncSystem` каждый тик; Phase 7 — простое копирование, Phase 11 добавит правильное «оружие в руках под Stance»).

В юните `Equipment.Primary = weaponEntityID`. Никакого массива внутри `Equipment`. Когда нужно «выпустить пулю» (Phase 11), систему стрельбы использует `Equipment.Active` чтобы найти оружие.

**Стрелять оружие в Phase 7 не умеет.** WeaponSystem / ballistic / damage / suppression-on-hit — Phase 11. Здесь только структуры данных.

**P7. ActionQueue — fixed `[4]Action`, immediate override на ПКМ (Q3 ответ).**

```go
type ActionKind uint8

const (
    ActionNone ActionKind = iota
    ActionMoveTo
    ActionStop
    ActionStance   // изменить позу
)

type Action struct {
    Kind   ActionKind
    Target rl.Vector3        // для MoveTo — точка; для Stance — Y=StanceCode
}

type ActionQueue struct {
    Actions [4]Action
    Head    uint8   // индекс текущего действия
    Tail    uint8   // куда писать следующее
}
```

Ring buffer, max 4 слота. Юнит исполняет `Actions[Head]`; по завершении — `Head = (Head+1) % 4`. Если `Head == Tail` — очередь пустая, юнит idle.

ПКМ-приказы в Phase 7:
- **ПКМ на точке** → для каждого выделенного юнита: queue clear, `Push(MoveTo(point))`. Immediate override — текущий приказ отменяется.
- **Shift+ПКМ на точке** → `Push(MoveTo(point))` без clear'а — append, юнит дойдёт до текущей цели и продолжит к новой (waypoint chaining).
- **`S` клавиша** → `Push(Stop)` immediate — юнит останавливается на месте.

**Phase 12 (Orders + Comms) переоформит ActionQueue.** Persistent `Order`-сущности с типом / целью / условиями станут источником action'ов; ActionQueue превратится в low-level decomposition (Order = «прорваться к окну с северной стороны» декомпозируется в `MoveTo(window)` + `Stance(Crouch)` + `Order` остаётся жить параллельно). RadioNetwork.Status == OFF будет блокировать запись новых Order'ов (action masking). Phase 7 не пытается предугадать форму — `[4]Action` это temporary shape, перепишется без правок Phase 7 потребителей.

**Условные / on-complete actions, периодические, attack-move** — Phase 12.

**P8. Selection — состояние в `main.go`, без UI-обвязки.**

В Phase 7 нет глобальной UI-системы, селекция — простой `[]ecs.Entity` в `main.go`:

```go
selected []ecs.Entity   // живёт в main loop scope
```

UX (компромисс — пользователь сказал «как удобнее»):
- **ЛКМ-клик** на юните (raycast от мыши через камеру в плоскость через юнит) → `selected = [unit]`.
- **ЛКМ-клик** на пустом месте → `selected = []`.
- **ЛКМ + drag** (marquee box) → `selected = [все юниты в screen-AABB]`.
- **Shift+ЛКМ-клик** на юните → toggle entity в `selected` (добавить если нет, убрать если есть).
- **Shift+ЛКМ + drag** → объединение текущего `selected` с marquee-box-юнитами.
- **ПКМ** → если `len(selected) > 0`, отдать MoveTo всем (см. P7). Если `selected` пуст и есть якорь — fallback на anchor-режим Phase 6.

**Это «группа», не «отряд».** Объединение выделенных юнитов — ad-hoc, исчезает при следующем выделении. **Squad как persistent сущность** с `CommandRoster + FormationData + RadioNetwork` — Phase 9. **Лёгкое разделение группы на отдельных юнитов для распределения по окнам** (упомянуто пользователем) — это Phase 9-10: Phase 9 даст squad-split механику + smart-object-aware распределение по cover-slot'ам Phase 6 (запрос «дай N окон в этом здании, ранжируй по dot(CoverDirection, threatDir)», раздать каждому юниту индивидуальную цель). В Phase 7 такого нет — есть только сырой ПКМ-MoveTo.

Визуальная подсветка выделения: вокруг каждого `selected` юнита — `rl.DrawCircle3D` радиусом 1 м на земле + `rl.DrawCubeWires` вокруг юнита-куба циан-цветом.

**P9. Vision system — chunk + 1-чанк-радиус (уточнение пользователя).**

`VisionSystem` — отдельная LOD-aware система. Per-tick на Active-юнитах, every 500 мс — итерация:
1. Для каждого Active-юнита: его текущий `cc = WorldPos.Chunk`.
2. Filter всех остальных Unit-сущностей через `WorldPos`, оставить тех, чей `WorldPos.Chunk` ∈ `cc + (-1..1, -1..1)` (3×3 чанка вокруг). **Это ограничивает кандидатов 9 чанками — типично 12-50 юнитов проверяются на одного.**
3. Для каждого кандидата: distance check `< vision.RangeM` (макс — `chunkSize` = 64 м), angle check `dot(facing, dirToTarget) > vision.AngleDot`, raycast occlusion check (CoverMap или прямой raycast против walls в этих 9 чанках).
4. Если видим — push в `Awareness.LastSeen` (FIFO в fixed-size 8).

**Максимальный радиус трейса = 64 м (chunk size).** За эту границу не смотрим в Phase 7 — это и для производительности, и для упрощения raycast'а через walls (загружаем только walls 9 чанков). Дальняя видимость (биноли, снайперы) — Phase 11.

Relevant-юниты (вне Active-радиуса якоря, но в Relevant-зоне) — `VisionSystem` обходит каждые 2 секунды, та же логика. Dormant — пропускает.

**Awareness.LastSeen** — pure data в Phase 7, никто не читает. Phase 10 (TacticalAI) и Phase 11 (Combat targeting) — потребители.

**P10. GroundStickSystem расширяется на все Unit-marked сущности.**

В Phase 6 `GroundStickSystem` клипает только anchor (`WorldPos.Y = GroundHeight + AnchorEyeHeight`). В Phase 7 фильтр расширяется до `Unit + WorldPos`. Anchor тоже остаётся (маркер `LODAnchor`), как был.

Multi-floor edge case: если юнит сейчас на этаже (его `WorldPos` лежит в footprint Floor-сущности и `WorldPos.Y` ближе к уровню этажа, чем к surface), GroundStick **не клипает** — Y сохраняется как есть. Резолвер тот же, что в P3 для NavService. Альтернатива — отдельный `OnFloor{Floor ecs.Entity}` маркер, проставляемый при пересечении Door/Stairs; пока обходимся резолвером по геометрии.

**Cubes из Phase 0/1 убираем.** Те 300 «парящих» mobile-кубов были placeholder'ом до GroundStick'а, теперь они мешают (все они получили бы Unit-маркер? — нет, без маркера, но и невидимые в render'е). Просто не спавним их в `main.go`.

**P11. NavService — добавляется опциональный multi-floor контекст.**

API сохраняется (Phase 6 pre-flight предупредил):
```go
func (s *NavService) FindPath(from, to components.WorldPos, opts NavOpts) []components.WorldPos
```

`NavOpts` расширяется:
```go
type NavOpts struct {
    Locomotion   LocomotionKind   // Phase 6
    PreferRoads  bool             // Phase 6
    AvoidOpenedDoors bool         // Phase 7 — заглушка для будущего stealth (Phase 12+)
}
```

Никакого `FloorContext` — резолвер делает работу автоматически. Phase 6 pre-flight снимается.

**P12. Test-scene — 12 soldiers, не 300 cubes.**

В `main.go`: после спавна зданий — 12 юнит-сущностей с фиксированными WorldPos:
- 4 рядом с домом 1 (-25, -40), для теста обхода стен.
- 4 между домом 2 и дорогой (40, 30), для теста road-bias movement.
- 4 у бункера (-30, 55), для теста sunken-entry (когда юнит идёт по лестнице вниз).

Каждый юнит получает `Equipment.Primary = AK47-entity-spawned-here`. Anchor тоже остаётся, движется как раньше через ПКМ-MoveTo (или мы превращаем anchor в одного из юнитов? — оставим anchor отдельно как «player marker» / camera target; он может быть выделен как обычный юнит, но `selected` никак с ним связан).

**P13. Что осознанно НЕ делаем в Phase 7.**

Каждый пункт сопровождается «куда переехало» — пользователь попросил это явно отмечать.

- **Combat / стрельба / урон / suppression-propagation** → **Phase 11**. WeaponSystem не существует. Юнит с оружием — visual placeholder, оружие не стреляет.
- **Persistent Order entities + RadioNetwork action masking** → **Phase 12**. ActionQueue в Phase 7 — простой ring buffer без условий, сроков жизни и приоритетов.
- **Squad как persistent сущность** (CommandRoster, FormationData, RadioNetwork) → **Phase 9**. «Группа» в Phase 7 = ad-hoc селекция мышью.
- **Полные Boids** (alignment + cohesion) → **Phase 9**. Сейчас только separation, юниты идут «облаком».
- **SquadMacroPathSystem + FormationSystem** (центр отряда, offset'ы) → **Phase 9**.
- **Squad-split UI / распределение по окнам** (упомянуто пользователем) → **Phase 9-10**. Сначала Squad как сущность (P9), потом smart-object-aware распределение через cover-slot'ы Phase 6 (P10).
- **Tactical AI** (fight-or-flight, Cover Evaluation, Suppression-driven retreat, scatter protocol) → **Phase 10**.
- **Doctrines** (Patrol / Assault / Stealth / Defense) → **Phase 10**.
- **Direct control** (взять одного бойца за плечо, WASD + mouse-aim) → **отказ пользователя**, не реализуем.
- **Дверь / лестница как usable smart-object** (юнит подходит → дверь открывается) → **Phase 12** (через Order-pipeline).
- **Раздавить препятствие техникой / десант** → **Phase 8/10**.
- **Cover Shadows от техники** (`DynamicCoverEmitter`) → **Phase 10**.
- **Multi-storey штурм через лестницу с прикрытием** → **Phase 10** (в Phase 7 юнит просто прошёл через Stairs-edge, никакой тактики).
- **Анимации движения / стрельбы / поз** (плавные переходы) → **Phase 16 polish**. Сейчас stance меняет высоту куба без интерполяции.
- **Cohesion (эластичный поводок, отстал → выпал из ростера)** → **Phase 9** (релевант когда есть Squad).
- **Action Masking при потере радиста** → **Phase 12**.
- **NavGrid re-bake при разрушении стены / двери** → **Phase 11** (вместе с `NavDirty`-маркером).
- **Air units / aerial nav** → **Phase 8+**.
- **Number-key selection groups** (Ctrl+1 bind, 1 recall) → **Phase 9** (когда Squad как persistent даст естественный target биндинга).

---

## Восемь мильстоунов

### M7.1 — Unit core types + replace cubes + GroundStick

**Цель.** На сцене стоят 12 кубиков-солдат вместо 300 мобильных. Каждый — полностью укомплектованная Unit-сущность (12 компонентов + оружие как отдельная сущность). Все Unit'ы корректно стоят на земле через расширенный `GroundStickSystem`.

**Делаем:**
- `components/unit.go`: `Unit`, `Stance`, `StanceCode`, `Motion`, `Collider`, `Vision`, `Suppression`, `Awareness`, `AwarenessEntry`, `LocalBlackboard`, `ActionKind`, `Action`, `ActionQueue`, `Equipment`, `OwnedBy`.
- `components/weapon.go`: `Weapon`, `WeaponKind`.
- `systems/ground_stick.go` — расширить filter с `LODAnchor` на `LODAnchor | Unit`.
- `main.go`:
  - Удалить цикл спавна 300 cube'ов.
  - Захардкодить 12 юнит-WorldPos'ов + per-unit спавн weapon-entity + установка `Equipment.Primary`.
  - Render-фильтр для `WorldPos + Unit` (placeholder cube, высота от `MaxSpeed[stance]`-таблицы или прямо от `StanceCode`).

**Проверяем.** `go run` показывает 12 кубиков на земле в нужных местах. Anchor по-прежнему ходит. Filter `Unit` возвращает 12 сущностей. Filter `Weapon + OwnedBy` возвращает 12 оружий, каждое привязано к своему юниту.

### M7.2 — FloorNavGrid bake

**Цель.** На каждой `Floor`-сущности существует свой `FloorNavGrid` после bake'а. По клавише `F` — debug-overlay показывает floor-grid'ы как цветные слои внутри зданий.

**Делаем:**
- `components/nav.go` — `FloorNavGrid`.
- `systems/spatial_bake.go` — расширить: после chunk-NavGrid в child-проходе пройти по `Floor`-сущностям этого чанка, для каждого построить FloorNavGrid (footprint → клетки → walls этого этажа из `BuildingChildIndex` через `WorldPos.Y`-фильтр → `Cost = 0` для затронутых клеток, проёмы открытых дверей оставить, окна = блок).
- `main.go` (render): debug-overlay по `F`.

**Проверяем.**
- В 2-этажном доме видны 2 floor-grid'а (этажи стоят на разных Y).
- Стены в floor-grid'е — чёрные (Cost=0). Открытая дверь — светлый «проём» в стене.
- Floor-grid 1-го этажа покрывает первый этаж, не вылезает за footprint.

### M7.3 — TransitionRegistry + multi-graph A*

**Цель.** `NavService.FindPath` корректно строит путь через дверь / лестницу. Anchor (через ПКМ-MoveTo, как в Phase 6) может быть отправлен в любую точку внутри здания и пройдёт туда.

**Делаем:**
- `components/nav.go` — `NavNode`, `NavNodeKind`, `TransitionEdge`, `TransitionRegistry`.
- `main.go` — `ecs.AddResource(world, &TransitionRegistry{Out: map[...][]TransitionEdge{}})` до InitUI.
- `systems/spatial_bake.go` — после floor-bake, для каждой Door / Stairs сущности в чанке: вычислить два узла-конца, добавить два направленных edge'а в registry. Закрытые двери получают edge с `Cost=0`.
- `systems/nav_service.go` — расширить A*:
  - Резолвер `WorldPos → NavNode` (P3): сначала проверка попадания в footprint Floor-сущностей этого чанка (filter ограниченный bbox чанка ± 1), иначе surface.
  - При expansion узла — 8 локальных соседей + `registry.Out[node]`.
  - Heuristic — Chebyshev в WorldPos-пространстве.
- `main.go` — `TerrainStreamingSystem.evict` дополнить чисткой transitions: при удалении Floor / Door / Stairs (через `BuildingChildIndex`) — удалить все edge'и из `registry.Out`, где `Owner == entity`. Для transitions, чьи концы в evicting-чанке — также удалить.

**Проверяем.**
- ПКМ внутри 2-этажного дома (на 1-м этаже у дальней стены) → якорь идёт к двери, входит, идёт к стене. Путь по overlay'ю — пунктирная линия с переходом через дверь.
- ПКМ на 2-м этаже → якорь идёт к двери, поднимается по лестнице, идёт по 2-му этажу. Видно, как Y якоря меняется на лестнице.
- ПКМ в бункер → якорь спускается по входной лестнице.
- ПКМ за закрытой дверью (если есть способ закрыть — пока нет, все default Open) → пропускаем тест.

### M7.4 — UnitMovementSystem + steering

**Цель.** Юниты двигаются по `ActionQueue` к target'ам. Несколько юнитов идущих в одну точку расходятся (separation force). Ground stick срабатывает на каждом тике.

**Делаем:**
- `systems/unit_movement.go`: `UnitMovementSystem`. LOD-policy: Active каждый тик, Relevant 250 мс, Dormant disabled.
  - Filter `Unit + WorldPos + Motion + ActionQueue + Stance + LODActive` (и параллельный для Relevant).
  - Per-unit: получить `MoveTo.Target` из `Actions[Head]`. Если достигнут (distance < 0.5 м) — `Head++`, выйти. Иначе — desired velocity, separation force от соседей в радиусе 1.5 м (другой filter `Unit + WorldPos + LODActive`, naive O(N²) на ~12 юнитов = 144 пары; spatial bucket — Phase 9 при росте N).
  - Apply: `WorldPos += velocity * dt`, `Motion.Yaw = atan2(velX, velZ)`, `Motion.Speed = |velocity|`.
- `main.go`: при ПКМ — push `MoveTo(point)` в `ActionQueue` каждого выделенного. Если `selected` пусто — fallback на anchor-движение (как Phase 6).

**Проверяем.**
- Выделить 3 юнита, ПКМ → все три идут к точке, расходятся (не сливаются в одну позицию).
- Один юнит, ПКМ → идёт прямо к точке.
- Юнит у стены, ПКМ за стеной → идёт через дверь / вокруг.

### M7.5 — Selection (mouse + marquee + Shift)

**Цель.** Игрок выделяет юниты мышью индивидуально и рамкой. Видна подсветка.

**Делаем:**
- `main.go`:
  - State: `selected []ecs.Entity`, `marqueeStart rl.Vector2`, `marqueeActive bool`.
  - Frame logic:
    - LMB pressed — `marqueeStart = mouse`, `marqueeActive = true`.
    - LMB released — если `|mouse - marqueeStart| < 5 px`: ЛКМ-клик-семантика (raycast → ближайший юнит → toggle/replace per Shift). Иначе — marquee: project каждого Unit на screen, сложить попавших в AABB. Replace selected (или union при Shift).
    - Esc или ЛКМ на пустом месте без drag — clear selected.
  - Render: для каждого selected — `rl.DrawCircle3D` радиусом 1 м на земле, цвет циан + cube-wires вокруг юнита.
  - Marquee active — рисуем 2D-прямоугольник в `BeginDrawing` mode (после `EndMode3D`).

**Проверяем.**
- Кликнул на юнита — он выделен (циан-кружок).
- Кликнул на пустое место — selection пустой.
- Drag рамкой через 4 юнита — все 4 выделены.
- Shift+click на 5-го юнита — добавился к selection.
- Shift+click на уже выделенного — удалился из selection.

### M7.6 — VisionSystem + Awareness

**Цель.** Юниты обновляют `Awareness.LastSeen`. По клавише `Y` (или другой) — debug-overlay показывает «кто кого видит» как тонкие линии между парами.

**Делаем:**
- `systems/vision.go`: `VisionSystem`. LOD: Active 500 мс, Relevant 2 сек, Dormant disabled.
  - Per-unit: `chunk = WorldPos.Chunk`. Filter `Unit + WorldPos + LODActive | LODRelevant` ограниченный bbox в 3×3 чанка вокруг.
  - Per-candidate: distance < `vision.RangeM` (макс 64 м, P9), angle check (`AngleDot`), raycast occlusion через walls в этих 9 чанках (filter `WallSegment + WorldPos` ограниченный тем же bbox). Ray от глаз стрелка на высоте 1.5 м к центру кандидата.
  - При успехе — push в `awareness.LastSeen` (FIFO, drop oldest при заполнении).
- `main.go` debug-overlay по `Y`: для каждой пары (a, b) где `b ∈ a.Awareness.LastSeen` — `rl.DrawLine3D` тонкий зелёный.

**Проверяем.**
- Юниты в прямой видимости друг друга — связаны зелёными линиями.
- Юниты разделённые стеной — не связаны.
- Юниты за пределами 64 м — не связаны.
- Юниты в dormant chunk'е — не обновляются (last-seen «застывает»).

### M7.7 — ActionQueue + орcrders + Stop

**Цель.** Очередь приказов работает. Shift+ПКМ — chaining; `S` — Stop.

**Делаем:**
- `main.go`:
  - На ПКМ без shift — для каждого selected: clear `ActionQueue` (Head=Tail=0), push `MoveTo(point)`.
  - На Shift+ПКМ — для каждого selected: push без clear'а (если место есть; иначе drop tail).
  - На клавишу `S` — для каждого selected: clear, push `Stop`.
- `systems/unit_movement.go`: handle `Action.Kind == Stop` → не движется, `Speed = 0`, через 0.1 сек — pop action.

**Проверяем.**
- ПКМ-ПКМ-ПКМ быстро в разные точки — юнит идёт в последнюю.
- Shift+ПКМ-Shift+ПКМ-Shift+ПКМ — юнит обходит все три точки по порядку.
- `S` — юнит останавливается на месте.

### M7.8 — Финальный test-pass

**Цель.** Полный сценарий проходим. Pipeline / persistence / clearance / multi-floor — без регрессий.

**Делаем:**
- HUD расширить: `Units: live=N selected=K | Vision pairs: V`.
- Сценарий:
  1. Запуск — 12 юнитов на местах.
  2. Marquee 4 юнита около дома 1, ПКМ за домом → все 4 идут вокруг, расходясь, без overlap'а.
  3. Click+Shift на трёх разных юнитах в разных группах, ПКМ внутрь 2-этажного дома → все три заходят, проходят на 1-й этаж.
  4. Один юнит + Shift+ПКМ цепочкой waypoints → проходит четыре точки.
  5. Vision check (Y): два юнита по разные стороны стены — нет линии. Сдвинуть одного на дверной проём — линия появляется.
  6. Стэмп кратера X на пути юнита — юнит уходит в дыру (NavGrid stale, P13 Phase 6 pre-flight). Принимаем.
  7. Quit / restart — юниты респавнятся в исходных позициях (детерминировано — они захардкожены), их Awareness начинает «с нуля» (потому что в Phase 7 не персистится).
  8. Большой обход (~60 чанков) — `Awareness.LastSeen` юнитов в dormant'е сохраняется (компонент не удаляется), память не растёт.

**Проверяем.** См. сценарий — без визуальных дефектов / падений / растущей памяти.

---

## Что считаем «закрытием Phase 7»

- 12 placeholder-юнитов на сцене с полным набором компонентов (Spatial / Senso-Cognitive / Execution / Equipment).
- Каждый имеет Weapon-сущность через `OwnedBy`. Стрелять не умеют — это Phase 11.
- Multi-floor навигация работает: ПКМ внутрь здания → юнит заходит через дверь, поднимается по лестнице, спускается в бункер.
- `NavService.FindPath(from, to, opts)` — расширенный API, automatic floor-resolver, без явного `FloorContext`.
- `UnitMovementSystem` ведёт юнитов по `ActionQueue` с separation-steering. Никакой alignment/cohesion / формаций — это Phase 9.
- `VisionSystem` обновляет `Awareness.LastSeen` для каждого юнита, лимит 64 м radius, 9 чанков candidate-set.
- Selection (LMB single + marquee + Shift) и приказы (ПКМ MoveTo, Shift+ПКМ append, S Stop) работают для группы выделенных юнитов.
- `GroundStickSystem` расширен на все Unit-сущности.
- Pipeline: `streaming → load → gen → river → road → building → trench → prop_spawn → spatial_bake → terrain_mesh → ground_stick → unit_movement → vision → ...`.
- Cubes из Phase 0/1 удалены.

После этого — обновление ROADMAP, Phase 7 → ✅, переход к Phase 8 (vehicles + RoadFollower) либо к Phase 9 (Squads + Formations) — приоритет решим к моменту перехода. Phase 9 даст естественный test-bench для всех accumulated «отдадим в Phase 9» отметок (формации, cohesion, persistent Squad-сущность, smart-object distribution).

---

## Заметки на полях

- **Multi-floor transitions — задел на разные стратегии переходов.** В Phase 7 один тип edge'ов (`Cost = 2` через любой Door/Stairs). Phase 8 (vehicles): техника физически не лезет в дом — соответствующие edge'и должны игнорироваться при `NavOpts.Locomotion = LocomotionWheeled/Tracked` (фильтр на типе транспорта vs тип edge'а). Phase 10 (Tactical AI): «штурм через окно» — это новый transition type (`OpeningKind = Window` от Phase 5 даёт основу — Window-сущности уже не блокируют LOS, но блокируют движение в Phase 7; в Phase 10 для штурмующего юнита под определённой доктриной Window становится проходимым, дороже чем Door, и со специальной анимацией). Расширение через новый `TransitionEdgeKind` enum, без правки структуры `TransitionEdge`.

- **Phase 9 как «правильная форма» движения групп.** `UnitMovementSystem` сейчас независимо рулит каждым юнитом. Phase 9 добавит promotion: если юнит в Squad'е, его `Motion.Target` пишется не его собственным A*-результатом, а `FormationSystem`'ом из Squad-position'а. UnitMovementSystem не меняется — он по-прежнему просто следует Target'у. Это значит: Phase 7 движение — это **базис, который Phase 9 не переписывает, а оборачивает сверху**.

- **`StanceCode` vs `Stance struct{Posture, Modifiers}`.** Когда придёт Phase 10 и реально появятся modifier'ы, миграция: rename field, add bitfield, написать Phase 10 code, который пишет в Modifiers. Phase 7 code, читающий `unit.Stance.Code`, заменяется на `unit.Stance.Posture` через одно `gopls rename` — никакой архитектурной правки. Поэтому держим простую форму сейчас, не предугадываем.

- **ActionQueue ring buffer vs slice.** Fixed `[4]Action` соблюдает правило DOD (без allocation per-action). Если 4 окажется мало — Phase 12 при переоформлении в Order-сущности всё равно будет перерабатывать форму, и тогда заодно поменяем размер. Не fight'имся в Phase 7.

- **Vision raycast против walls — performance.** На 12 юнитов × 12 candidate'ов = 144 пары × 8 walls в bbox 9 чанков = ~1100 raycast'ов каждые 500 мс. Это ~2200 raycast/сек. Каждый raycast — segment-vs-segment 2D, ~10 нс. В сумме — 22 микросекунды, незаметно. При росте N (Phase 11 батальные сцены 200+ юнитов) — заведём **per-chunk Vision-graph** (precomputed cellpair occlusion bits) или octree. Phase 7 — naive enough.

- **Selection raycast от мыши.** Raycast-from-screen через `rl.GetMouseRay(mouse, currentCamera)` → пересечение с плоскостью `Y = anchor.Local.Y` (или `Y = 0` глобально). Получается world point; ближайший Unit к нему в радиусе 1 м (placeholder-cube размер) — selected. Точнее можно через Unit's collider-bounding, но лишняя сложность для Phase 7.

- **«Группа» vs «Squad».** `selected` в Phase 7 — список entity-ID, isolation в `main.go`. Никакой ECS-компонент, никакой `Squad`-сущности. Когда Phase 9 заведёт `Squad{CommandRoster}`, Selection превратится в input для squad-management UI («дай этим N юнитам общий приказ» → cмерджить в один Squad если они не в одном; или отдать `Order` существующему Squad'у если они уже в нём). Phase 7 ад-хок-группа никак не пересекается с Squad-сущностью — это разные слои абстракции. Phase 9 должен сохранить семантику Phase 7: ПКМ-приказ ad-hoc-группе работает как раньше, плюс «постоянный Squad» появляется как опция.

- **Direct control — отказ.** Все управление через Selection + ПКМ. Если в будущем понадобится — это лёгкое расширение поверх Phase 7 (Tab toggle + перенаправление WASD на selected unit instead of anchor). Не делаем сейчас, и в pre-flight для будущих фаз тоже не отмечаем — это пользовательский отказ, не deferred work.

- **Anchor в Phase 7.** Остаётся как «player marker» / camera target, не Unit. WASD по-прежнему его двигает. ПКМ при пустом selection делает `anchor.MoveTo(point)`. Когда selection не пустой — ПКМ переключается на selected. Это однозначное правило: **ПКМ всегда отдаёт приказ "тому, кто выделен"; если ничего не выделено — отдаёт anchor'у**.

- **Per-floor NavGrid persistence.** Не персистится (как и Phase 6 surface NavGrid). `FloorNavGrid` — компонент на Floor-сущности, Floor-сущность сама не персистится (вместе с building children), пересоздаётся при возврате чанка из dormant'а через `BuildingSystem` + `SpatialBakeSystem`.
