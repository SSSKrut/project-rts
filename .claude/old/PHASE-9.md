# Phase 9 — рабочий план

Squad как невидимая ECS-сущность поверх юнитов из Phase 7. Макро-маршрут отряда (центр), формации (offset'ы бойцов), cohesion («эластичный поводок»). Поверх Phase 7 `UnitMovementSystem` — он не меняется, FormationSystem пишет в `ActionQueue` каждого члена ростера. Базис для Phase 10 (тактический ИИ читает Squad), Phase 11 (focus fire, suppression-propagation между членами), Phase 12 (Order-сущности + RadioNetwork action masking).

**Чего в этой фазе сознательно НЕТ.** Доктрины поведения (Patrol / Assault / Stealth / Defense) — Phase 10. Tactical AI / fight-or-flight / cover-evaluation — Phase 10. Persistent `Order`-сущности с условиями и сроками жизни — Phase 12. RadioNetwork как gate ввода игрока (action masking при потере радиста) — Phase 12; в Phase 9 заводим только структурный задел. Распределение бойцов по `Window`-cover-slot'ам через Smart Objects — Phase 10. Векторные приказы (зажим ПКМ + drag для ширины фронта) и UI отрядов с карточками бойцов — Phase 14. Number-key bind'ы (Ctrl+1 / 1) — отдельная подзадача в этой же фазе (M9.5).

ROADMAP — высокоуровневый трекер. Этот файл — рабочий, обновляется по ходу выполнения.

---

## Решения, которые лочим до начала кода

**P1. Squad — отдельная ECS-сущность без `WorldPos`, фиксированный ростер `[8]ecs.Entity`.**

```go
// Marker (filter-target)
type Squad struct{}

// CommandRoster — fixed-size unit list. 0 = empty slot. Slot index доpубль в
// формации (см. P3): 0 = командир по центру, 1..7 — позиции по типу формации.
const SquadRosterSize = 8

type CommandRoster struct {
    Members [SquadRosterSize]ecs.Entity
    Count   uint8           // фактическое число живых членов; ≤ SquadRosterSize
}

// FormationData — текущая формация и пространственные параметры.
type FormationData struct {
    Type    FormationKind
    Forward rl.Vector3      // unit XZ-вектор «куда смотрит отряд»; пишется
                            // SquadMacroPathSystem'ом (= последний шаг центра)
    Spacing float32         // метров между соседями
}

type FormationKind uint8
const (
    FormationLine    FormationKind = iota   // в шеренгу перпендикулярно Forward
    FormationColumn                         // в колонну вдоль Forward
    FormationWedge                          // клин (V): командир впереди
    FormationLoose                          // рассредоточено (Boids-like jitter,
                                            //   spacing = радиус облака)
)

// MacroPath — текущий маршрут центра отряда. Fixed [8] — макро-путь редко
// длиннее 6-7 точек на чанк (NavService возвращает waypoints с шагом ≈1 м,
// мы децимируем — см. P5).
const SquadMacroPathSize = 8

type MacroPath struct {
    Waypoints   [SquadMacroPathSize]WorldPos
    Head        uint8         // индекс текущей цели; Count == 0 ⇒ путь пуст
    Count       uint8
    Goal        WorldPos      // финальная цель (для ре-плана)
    HasGoal     bool          // true ⇒ есть активный приказ
    ReplanAt    float32       // session-time, после которого можно ре-планировать
                              // (default-throttle 1-3 сек, см. P5)
    LastPlanned float32
}

// RadioNetwork — структурный задел Phase 12. В Phase 9 пишется один раз при
// формировании Squad'а (HasRadioman = true если у одного из членов Equipment.
// Secondary указывает на Radio-сущность; Phase 7 такие сущности ещё не
// спавнит, так что флаг де-факто всегда false). Никто из Phase 9 систем не
// читает RadioNetwork — это форма, не поведение.
type RadioNetwork struct {
    Frequency   uint8         // 0 = «не настроена»; реальные частоты — Phase 12
    HasRadioman bool          // false ⇒ Phase 12 будет блокировать ввод
    HQReachable bool          // false ⇒ нет связи со штабом; LW в Phase 12
}
```

Squad-сущность = `Squad + CommandRoster + FormationData + MacroPath + RadioNetwork`. Пять компонентов, нет `WorldPos` — отряд это абстракция, центр вычисляется на лету (см. P4). `AlwaysActive` маркер — Squad не должен попадать под LOD-eviction даже если все его члены ушли в dormant (отряд может стоять глубоко в тылу с приказом «оборонять» и активироваться при контакте).

**P2. `SquadMember{Squad ecs.Entity}` на каждом бойце в ростере — обратный указатель.**

Зеркало `BuildingMember` из Phase 5. Без него каждая FormationSystem итерация требовала бы перебора всех Squad'ов для поиска squad'а юнита (O(юниты × отряды)). С `SquadMember` — O(юниты).

```go
type SquadMember struct {
    Squad     ecs.Entity      // 0 = юнит вне отряда (одиночка)
    SlotIndex uint8           // индекс в CommandRoster.Members; 0 = командир
}
```

Инвариант: для каждого `SquadMember{Squad: s, SlotIndex: i}` верно `world.Get[CommandRoster](s).Members[i] == thisEntity`. Поддерживается атомарно через `SquadService` (P10), нельзя править руками.

`SquadMember` — добавляемый компонент (архетип меняется при join/leave). Это требует обычной для проекта дисциплины: мутации архетипа собираются в локальный slice внутри системы и применяются после закрытия запроса.

**P3. FormationSystem пишет `ActionQueue` бойцов; UnitMovementSystem не меняется.**

Phase 7 P-заметка прямо предсказала эту форму: «UnitMovementSystem не меняется — он по-прежнему просто следует Target'у». Это работает так:

1. `SquadMacroPathSystem` (P4) считает `MacroPath` для центра отряда раз в 1-3 сек, обновляет `MacroPath.Waypoints`.
2. `FormationSystem` каждый тик (Active 100 ms / Relevant 500 ms / Dormant disabled — P7):
   - Берёт текущий waypoint центра (`MacroPath.Waypoints[Head]`) либо `MacroPath.Goal` если путь пуст.
   - Пересчитывает текущий **центр отряда** = среднее `WorldPos` по живым членам ростера (вес 1 / Count).
   - Для каждого слота `i` вычисляет `offset[i]` от формации (deterministic from `FormationKind, SlotIndex, Spacing, Forward`).
   - Целевая точка члена = `centerWaypoint + offset[i]`.
   - Чистит `ActionQueue` бойца (`ClearActions`), пушит один `MoveTo(centerWaypoint + offset[i])`.

UnitMovementSystem продолжает работать без изменений — он видит свежий `MoveTo` в очереди и идёт к нему с separation steering.

**Производительность.** 12 юнитов × 60 fps = 720 ClearActions+PushAction в секунду — это микросекунды. Для Phase 11 (200 юнитов) станет ~12k ops/sec, тоже фон. Если когда-то станет видно — дешёвая оптимизация: писать `ActionQueue` только при изменении offset'а > ε (юнит уже идёт куда нужно).

**Альтернатива (отвергнута).** Завести отдельный компонент `FormationOffset{Target WorldPos}` и в UnitMovementSystem читать Target оттуда (если есть). Плюс — нет лишних writes в ActionQueue. Минус — два пути в одном системе, ветвление, потеря инварианта «всё движение через ActionQueue» из Phase 7. Проще держать ActionQueue единственным каналом.

**P4. Центр отряда вычисляется на лету, не хранится.**

Squad не имеет `WorldPos`. Центр = среднее по `WorldPos` живых членов ростера. Считается:
- В `FormationSystem` каждый тик (когда нужен offset).
- В `SquadMacroPathSystem` при переоценке (раз в 1-3 сек) — для запроса `NavService.FindPath(center, goal)`.

Это ~8 чтений `posMap.Get` на отряд за тик — копейки. Альтернатива «кэшировать центр в Squad'е» добавляет инвариант (когда обновлять?), не выигрывая в производительности на текущем масштабе.

**P5. SquadMacroPathSystem — A* для центра, throttle 1-3 секунды + триггер ре-плана.**

```go
const (
    SquadReplanInterval     float32 = 1.0    // обычная переоценка
    SquadReplanGoalChangeEpsilon float32 = 0.5  // м; приказ обновился ≤ ε ⇒ не пере-планируем
    SquadReplanCenterDriftEpsilon float32 = 5.0  // м; центр уехал > ε ⇒ принудительный ре-план
)
```

Per-Squad алгоритм (раз в `SquadReplanInterval`):
1. Если `MacroPath.HasGoal == false` — пропустить.
2. Считаем центр (P4). Если центр в `arrivalRadius` (см. ниже) от `Goal` — сбросить `HasGoal`, отряд idle.
3. Иначе вызываем `NavService.FindPath(center, Goal, NavOpts{Locomotion: LocomotionFoot})`.
4. **Децимируем waypoint'ы**: NavService возвращает шаг 1 м, нам это много. Берём каждый N-й (N зависит от Spacing формации, типично 4-8 м между макро-точками). Сохраняем `Goal` как обязательный последний waypoint.
5. Пишем в `MacroPath.Waypoints`, обновляем `LastPlanned`.

**Триггеры ре-плана вне throttle:**
- Игрок отдал новый приказ (Goal изменился > `SquadReplanGoalChangeEpsilon`) — немедленный ре-план.
- Центр отряда отдрейфовал от ожидаемой траектории > `SquadReplanCenterDriftEpsilon` (например, бойцы массово ушли в обход препятствия) — ре-план на следующем тике.

`SquadArrivalRadius` = `1.5 * Spacing`. Когда центр в этом радиусе от `Goal`, `HasGoal` сбрасывается.

**Память.** `MacroPath.Waypoints [8]WorldPos` — 8 × ~32 байт = 256 байт на отряд. На 50 отрядов (Phase 11 батальная сцена) = 12 KB — фон.

**P6. Cohesion интегрирован в FormationSystem, не отдельная система.**

Каждый тик FormationSystem проходит ростер, и для каждого бойца:
1. Считает дистанцию `WorldPos` бойца до центра отряда.
2. Если `dist > CohesionLeash` (default = `4 * Spacing`, типично 16-32 м):
   - Удалить юнита из `CommandRoster.Members[slotIndex]`, уменьшить `Count`.
   - Удалить компонент `SquadMember` с юнита (через mutation buffer, см. P2).
   - Юнит остаётся жить как одиночка — его `ActionQueue` сохраняется, последний пушинутый `MoveTo` остаётся в Head.

Юнит-одиночка обратно сам в отряд не возвращается — нужен явный `SquadService.Join` (P10) либо merge через горячую клавишу (M9.5). Это та самая «эластичный поводок → выпал → одиночка» механика из DESIGN.md.

**Почему не отдельная система.** FormationSystem уже считает дистанцию каждого бойца до центра (для cohesion-проверки). Добавлять отдельный CohesionSystem с дублирующим pos-lookup'ом — лишний проход. Меньше кода = меньше точек регрессии.

**Cohesion в Phase 7 терминах.** Это первая реальная мутация архетипа на основании пространственного состояния. Pattern: собираем `[]ecs.Entity` для leave в локальный slice внутри FormationSystem, после закрытия Filter Query применяем `world.RemoveComponent[SquadMember]` + правим CommandRoster. Это уже отработано в LODSystem / StreamingSystem.

**P7. LOD-политики Squad-систем — Active по таймеру, Relevant реже, Dormant disabled.**

| Система | Active | Relevant | Dormant |
|---|---|---|---|
| `SquadMacroPathSystem` | 1000 ms | 3000 ms | disabled |
| `FormationSystem` | 100 ms | 500 ms | disabled |

Squad относится к Active/Relevant если **большинство** членов в Active/Relevant. Для Phase 9 берём упрощение: Squad наследует тиер своего командира (boец на slot index 0). Командир в Active → Squad в Active. Это устойчиво пока CommandRoster.Members[0] — реально командир (см. P10 — при выходе командира кто-то занимает его слот).

Реализация: Squad-сущность не имеет `LODActive`/`LODRelevant` маркеров; SquadMacroPathSystem и FormationSystem сами выбирают тиер исполнения, читая LOD-маркеры командира через `SquadMember[CommandRoster.Members[0]]`.

**P8. Selection в Phase 9 — Squad-aware, без ломки Phase 7 семантики.**

`selected []ecs.Entity` остаётся в `main.go` как в Phase 7. Что меняется в обработке ПКМ:

1. **Все selected принадлежат одному и тому же Squad'у** (через `SquadMember.Squad`) → приказ отдаётся **Squad'у** через `SquadService.OrderMoveTo(squad, goal)`. Это ставит `MacroPath.Goal`, `HasGoal=true`, триггерит немедленный ре-план.

2. **Selected разнородный** (миксы Squad'ов / одиночек, или просто один одиночка) → приказ отдаётся каждому **индивидуально**, как Phase 7 (`ClearActions` + `PushAction(MoveTo)` на каждый юнит). Юниты, которые в Squad'е, **выпадают из ростера** (их `MacroPath`-следование заменено персональным приказом — они идут как одиночки).

   Эта механика — реализация фразы из DESIGN.md «отстал от центра > X — выпал из ростера»: персональный приказ это пограничный случай «явно ушёл от центра по приказу игрока».

3. **Shift+ПКМ** для appending — работает только для одиночек (Phase 7 поведение). Для Squad — Shift+ПКМ append'ит waypoint в `MacroPath.Goal`-цепочку (но пока в Phase 9 МакроПути одно-целевые: append отдаёт второй приказ только когда первый выполнен; реальная multi-goal очередь приказов — Phase 12 Order-сущности).

**`H` (Stop)** для Squad — `SquadService.Stop(squad)` чистит `MacroPath.HasGoal`, FormationSystem перестаёт писать ActionQueue, бойцы доходят до текущего offset'а и останавливаются. Для одиночек — Phase 7 поведение.

**P9. Squad-формирование — горячая клавиша `T` (Team).**

UI Phase 9 минимальный (полный UI отрядов — Phase 14):
- **`T` (Team)** при `len(selected) >= 2`: создать новый Squad из selected. Если кто-то из selected уже был в Squad'е — он покидает старый (Cohesion-style leave), вступает в новый. Старый Squad остаётся жить с уменьшенным Count; если Count падает до 0 — Squad despawn'ится.
- **`U` (Ungroup)** при `len(selected) >= 1`: каждый юнит из selected покидает свой Squad (если был); сами Squad'ы остаются.
- **`Ctrl+1` ... `Ctrl+5`**: bind selected (если они в одном Squad'е — bind'ится Squad; иначе — bind'ится `[]ecs.Entity` selected как ad-hoc список).
- **`1` ... `5`**: recall — selected = bind. Если bind — Squad, рамка вокруг центра + кружочки вокруг каждого члена; если bind — `[]ecs.Entity`, обычное selection.

Селектор формаций — пока `F1`/`F2`/`F3`/`F4` (Line/Column/Wedge/Loose). Применяется к Squad'у, в котором лежит selected. Spacing — фиксированный per-FormationKind (Line: 2 м, Column: 2 м, Wedge: 3 м, Loose: 4 м).

**P10. SquadService — service object (не System), как `Stamper` / `NavService`.**

Все мутации Squad'ов / ростеров проходят через `SquadService` чтобы инвариант P2 (CommandRoster ↔ SquadMember) не разъезжался.

```go
type SquadService struct { /* ecs.Map handles */ }

func NewSquadService(w *ecs.World) *SquadService

// CreateFromUnits спавнит Squad-сущность, прописывает CommandRoster, навешивает
// SquadMember на каждый из units. Если кто-то из units уже в Squad'е — сначала
// Leave из старого. Возвращает новый Squad.
func (s *SquadService) CreateFromUnits(units []ecs.Entity, formation FormationKind) ecs.Entity

// Join добавляет unit в существующий Squad. Если Roster полный — возвращает
// false (не делает ничего). Если unit уже в этом же Squad'е — no-op.
func (s *SquadService) Join(squad, unit ecs.Entity) bool

// Leave удаляет unit из его Squad'а (если есть). Сжимает CommandRoster.Members
// (сдвигает хвост, чтобы Members[i] для i < Count не было нулей; SlotIndex
// остальных бойцов пере-проставляется).
func (s *SquadService) Leave(unit ecs.Entity)

// Despawn разваливает Squad: каждый член получает Leave, Squad-сущность
// удаляется.
func (s *SquadService) Despawn(squad ecs.Entity)

// OrderMoveTo ставит цель Squad'у. Триггерит немедленный ре-план в
// SquadMacroPathSystem (через сброс ReplanAt).
func (s *SquadService) OrderMoveTo(squad ecs.Entity, goal WorldPos)

// Stop сбрасывает HasGoal; формация дойдёт до текущих offset'ов и останется.
func (s *SquadService) Stop(squad ecs.Entity)
```

Сжимать ростер при Leave (а не оставлять дыры) — потому что offset'ы формации завязаны на SlotIndex, и дыра = «пустое место в шеренге». Сжимаем → формация остаётся плотной. Цена — Count-1 swap'ов SquadMember.SlotIndex при удалении из середины. На 8-местном ростере — фон.

Командир (slot 0) — особый: при Leave командира роль переходит к slot 1 (новый slot 0). Если ростер пустой после Leave — Despawn Squad'а.

**P11. Render Squad'а — линии от центра к каждому члену + индикатор формации.**

Когда любой член Squad'а в `selected`, рисуем:
- `rl.DrawSphere` или `DrawCircle3D` радиусом 0.5 м в **центре отряда** (P4 расчёт), цвет — squad-color (детерминированно от squad ent ID hash).
- `rl.DrawLine3D` от центра к каждому живому члену тонкой squad-color линией.
- Поверх — стандартная Phase 7 cyan-подсветка selected'ов.

Это даёт визуальный ответ «кто в отряде» без UI-карточек (Phase 14). Также: hold-`F` (или новая клавиша — `K`?) рисует все Squad'ы на сцене в squad-color, не только selected.

**P12. Test scene — 12 юнитов из Phase 7 разбиваются на 3 Squad'а по 4 при старте.**

`main.go` уже спавнит 12 юнитов в трёх кластерах (cluster 1/2/3 по 4). Phase 9 после спавна юнитов вызывает `squadService.CreateFromUnits(cluster1, FormationLine)` × 3. Это даёт три готовых Squad'а на старте сцены.

Каждый кластер получает разную формацию для визуальной проверки:
- Cluster 1 (вокруг дома 1) — `FormationLine`, командир с восточного края.
- Cluster 2 (между домом 2 и дорогой) — `FormationWedge`, командир южнее.
- Cluster 3 (у бункера) — `FormationColumn`, командир северный.

Anchor остаётся вне Squad'ов, как в Phase 7 — он камера/marker, не боец.

**P13. Что осознанно НЕ делаем в Phase 9.**

Каждый пункт сопровождается «куда переехало».

- **Доктрины поведения отряда** (Patrol / Assault / Stealth / Defense, реакция на контакт, рассредоточение под огнём) → **Phase 10**. В Phase 9 формация — статичная геометрия offset'ов, никаких переключений по контексту.
- **Tactical AI / fight-or-flight / cover-evaluation / SurvivalInstinctSystem / DynamicCoverSystem (Cover Shadows)** → **Phase 10**. Phase 9 не читает `Suppression` / `Awareness` / `CoverMap` — только пишет позиции.
- **Persistent `Order`-сущности** с условиями, сроками жизни, приоритетами, on-complete-actions, периодическими (patrol-loop), attack-move → **Phase 12**. В Phase 9 «приказ» = `MacroPath.Goal + HasGoal flag` внутри Squad-компонента, не отдельная сущность.
- **RadioNetwork как gate ввода** (action masking при потере радиста) → **Phase 12**. В Phase 9 только структурное место (`HasRadioman bool`).
- **KW vs LW рации**, ретранслятор через технику → **Phase 12**.
- **Распределение бойцов по `Window`-cover-slot'ам** при приказе «оборонять здание», «занять окна» → **Phase 10**.
- **Squad-doctrines panel в UI** → **Phase 14**.
- **Векторные приказы** (зажатый ПКМ + drag для ширины фронта или направления атаки) → **Phase 14**.
- **Карточки бойцов в нижней панели** при выделении Squad'а → **Phase 14**.
- **Меню построения формаций** (визуальная палитра Line / Column / Wedge / Loose / Echelon / Diamond / File + spacing-слайдер + превью) → **Phase 14**. Phase 9 даёт только хоткеи `F1`-`F4` для четырёх базовых формаций; полный UX-слой описан в `DESIGN.md` секция «Меню построения формаций».
- **Полный UX команд** (ПКМ-tap vs hold context-menu, double/triple-click sprint/attack-move, drag-and-drop бойцов между Squad'ами в карточках, auto-formation suggestions для multi-squad) → **Phase 14**. Принцип «один клик = одно намерение» зафиксирован в `DESIGN.md` секция «UX команд».
- **Режимы пехоты+техника** (`EmbarkedIn` для Mounted/Riding, `EscortPair` для Following/Escorting, AI-вето на mount-into-burning, Cover Shadow от движущейся техники) → **Phase 8** (структурно: `Vehicle.SeatCount`) + **Phase 10** (поведение Following/Escorting + AI-вето) + **Phase 11** (модель урона на десанте). Подробности — `DESIGN.md` секция «Пехота и техника». В Phase 9 формация — всегда «свободная», без привязки к технике.
- **Тактическая карта со штабными иконками и индикаторами связи** → **Phase 14**.
- **Multi-Squad concentrationmechanics** (выделил 2+ Squad'а, кликнул цель → распределить полукругом охвата) → **Phase 14** (нужен UI для multi-squad selection-as-formation).
- **Squad split на огневые группы** автоматически (3-4 человека: командир + пулемётчик / медик / радист) — **Phase 10/14**. Phase 9 даёт только manual split через `T` / `U`.
- **Сводная группа из бойцов разных Squad'ов** (DESIGN.md) — Phase 9 даёт через `T` (создаёт новый Squad из selected, рвёт старые); полноценный «merge» (объединить два Squad'а в один) — `M9.5` опционально.
- **Squad-persistence на диск** → **Phase 16 polish** (вместе со save/load всего мира).
- **Squad-spawn в LOD-Dormant** (отряды глубоко в тылу с приказом, активирующиеся при контакте) → **Phase 13** (стратегический ИИ).

---

## Семь мильстоунов

### M9.1 — Squad core types + SquadService + spawn 3 squad'ов в test scene

**Цель.** На сцене существуют 3 Squad-сущности с заполненным CommandRoster, каждый юнит несёт SquadMember с правильным SlotIndex. SquadService доступен как глобальный service object (как Stamper / NavService).

**Делаем:**
- `components/squad.go`: `Squad`, `CommandRoster`, `FormationData`, `FormationKind`, `MacroPath`, `RadioNetwork`, `SquadMember`. Все константы (`SquadRosterSize`, `SquadMacroPathSize`).
- `systems/squad_service.go`: `SquadService` со всеми методами P10. Внутри держит `*ecs.Map[Squad]`, `*ecs.Map[CommandRoster]`, `*ecs.Map[FormationData]`, `*ecs.Map[MacroPath]`, `*ecs.Map[RadioNetwork]`, `*ecs.Map[SquadMember]`, `*ecs.Map[components.AlwaysActive]`.
- `main.go`:
  - `squadService := systems.NewSquadService(app.World)` рядом с `navService`.
  - После спавна 12 юнитов: `squadService.CreateFromUnits(cluster1, FormationLine)` × 3 (см. P12).

**Проверяем.**
- `Filter[Squad]` возвращает 3 сущности.
- На каждом юните `SquadMember.Squad` валиден; `SquadMember.SlotIndex < 4`.
- `CommandRoster.Members[SlotIndex] == thisUnit` для каждого юнита (P2 инвариант).
- На сцене ничего не движется — SquadMacroPathSystem ещё нет.

### M9.2 — SquadMacroPathSystem (A* центра + throttle + ре-план триггеры)

**Цель.** Когда у Squad'а `HasGoal=true`, его `MacroPath.Waypoints` содержит децимированный путь от центра к цели. Throttle 1 сек, ре-план при изменении `Goal` или дрейфе центра > 5 м.

**Делаем:**
- `systems/squad_macro_path.go`: `SquadMacroPathSystem`. LOD-policy: `ActiveEvery: 1000ms`, `RelevantEvery: 3000ms`, `DormantEvery: LODDisabled` (P7).
  - Filter `Squad + CommandRoster + MacroPath + FormationData`.
  - Per-Squad: вычислить центр (P4), проверить `HasGoal + arrivalRadius` (P5 шаг 2), вызвать `NavService.FindPath(center, Goal, NavOpts{Locomotion: LocomotionFoot})`, децимировать (P5 шаг 4), записать в `MacroPath.Waypoints`.
  - Триггер ре-плана через сброс `ReplanAt = 0` — система видит и пере-планирует на следующем выполнении (а не дополнительный hot-path путь через service).
  - **NB:** SquadMacroPathSystem нужен **ecs.World** (не `posMap`), потому что центр считается через ростер. Lookup'ы посMap.Get на 8 юнитов — копейки.
- `systems/squad_service.go`: `OrderMoveTo` ставит `Goal + HasGoal=true + ReplanAt=0`. `Stop` ставит `HasGoal=false`.
- `main.go`: regger `app.AddSystem(squadMacroPathSys)` между `vision` и `lod` (после движения и vision, перед LOD).

**Проверяем.**
- Через ад-hoc дебаг (например ТЕМПОРАЛЬНО `OrderMoveTo` для squad1 в коде main.go при старте): `MacroPath.Waypoints[0..Count]` покрывает путь от cluster1 до цели.
- При `Goal` далеко от центра — пишутся `Count > 0` waypoint'ы; при достижении (центр в `arrivalRadius`) — `HasGoal` сбрасывается.

### M9.3 — FormationSystem (offsets + ActionQueue write + cohesion)

**Цель.** Бойцы в Squad'е держат формацию (offset'ы от центра по `FormationKind`). При движении центра — бойцы идут параллельно. Если боец отстал > leash — выпадает из ростера.

**Делаем:**
- `systems/formation.go`: `FormationSystem`. LOD-policy: `ActiveEvery: 100ms`, `RelevantEvery: 500ms`, `DormantEvery: LODDisabled`.
  - Filter `Squad + CommandRoster + FormationData + MacroPath`.
  - Per-Squad:
    1. Считаем центр (P4).
    2. Берём `centerWaypoint = MacroPath.Waypoints[Head]` (или `Goal`/center если path пуст).
    3. Считаем `Forward` = `(centerWaypoint - center).Normalize()` (если не движется — оставляем старый Forward).
    4. Per-slot: `offset = formationOffset(Type, SlotIndex, Spacing, Forward)`. Pure function в `formation.go`.
    5. `targetPos = centerWaypoint + offset`.
    6. **Cohesion check:** `dist = bouncerPos.Distance(center)`; если > `4 * Spacing` → push в local `leaveBuffer []ecs.Entity`.
    7. Иначе: `ClearActions(actionQueue)`, `PushAction(MoveTo(targetPos))`.
  - **После закрытия Filter Query**: для каждого `e` в `leaveBuffer` вызвать `squadService.Leave(e)`.
- `systems/formation.go::formationOffset`: pure function, вычисляет offset для каждого FormationKind. Тестово реализуем 4 формации:
  - **Line:** Slot 0 в центре, чётные индексы вправо (`Right = perp(Forward)`), нечётные влево, шаг = Spacing. `[0, +1, -1, +2, -2, +3, -3, +4]`.
  - **Column:** Slot 0 впереди (`-Forward * SlotIndex * Spacing`), идём в линию назад.
  - **Wedge:** Slot 0 впереди (командир), Slot 1-2 за ним под углом 45° назад-вправо/влево по `Spacing`, дальнейшие — расширяющий клин.
  - **Loose:** Slot 0 в центре, остальные в hashed positions внутри радиуса `Spacing * 2` (детерминированно по SlotIndex, чтобы офсет не дрожал).

**Проверяем.**
- Squad с FormationLine идёт по приказу — бойцы держат шеренгу, перпендикулярную направлению движения.
- При смене направления (waypoint поворачивает) — формация пере-ориентируется (Forward обновляется).
- Если боец застрял (дерево, текстура) — через несколько секунд он уезжает > leash → попадает в `leaveBuffer` → Squad'е CommandRoster.Count уменьшается, юнит становится одиночкой со старым ActionQueue (направлен туда, где был его offset).

### M9.4 — Selection-aware приказы (ПКМ Squad-aware, H-Stop)

**Цель.** ПКМ при гомогенном selected (все из одного Squad'а) → приказ Squad'у. ПКМ при гетерогенном — приказ каждому индивидуально (как Phase 7 + leave-from-Squad). H — Stop для Squad'а или каждого одиночки.

**Делаем:**
- `main.go` (RMB-обработчик):
  - Завести helper `groupSelected(selected, squadMemberMap) (commonSquad ecs.Entity, isHomogeneous bool)`.
  - Если `isHomogeneous && commonSquad != 0`: `squadService.OrderMoveTo(commonSquad, target)`.
  - Иначе: для каждого `e` в selected — если был в Squad'е, `squadService.Leave(e)`; затем `ClearActions(aq)` + path + `PushAction(MoveTo)` (Phase 7 логика).
- `main.go` (H-handler):
  - Гомогенный → `squadService.Stop(commonSquad)`.
  - Гетерогенный → Phase 7 логика per-юнит.
- `main.go` (Shift+RMB):
  - Squad: append'ит как одноуровневый второй MoveTo (пока без полноценной MacroPath-цепочки, см. P8).
  - Одиночки: Phase 7 логика.

**Проверяем.**
- Кликнул бойца из cluster 1, ПКМ далеко → Squad1 идёт целиком в формации.
- Marquee всех 12 юнитов → они в трёх разных Squad'ах → ПКМ заставит каждого выпасть из своего Squad'а и идти индивидуально (можно проверить визуально: формация разваливается в облако).
- H на Squad → бойцы доходят до текущих offset'ов и стоят. Ходить пешком обратно (сдвинуть anchor на 5 м WASD'ом, кликнуть нового selected → Squad всё ещё держит позицию, новый selected получает свой приказ).

### M9.5 — `T`/`U`/`F1`-`F4` + Ctrl+number bind / number recall

**Цель.** Игрок управляет Squad'ами горячими клавишами: создавать, разбивать, менять формацию, бындить и вызывать.

**Делаем:**
- `main.go`:
  - `T`: при `len(selected) >= 2` — `squadService.CreateFromUnits(selected, FormationLine)`. Selected обновляется на этот один новый Squad (selected = ros.Members).
  - `U`: per-юнит в selected — `squadService.Leave(e)`.
  - `F1`-`F4`: для гомогенного selected (P8) — изменить `FormationData.Type` соответствующего Squad'а; `Spacing` подставить из таблицы (Line:2, Column:2, Wedge:3, Loose:4).
  - `Ctrl+1` ... `Ctrl+5`: bind = current selected как `[]ecs.Entity` (или ID Squad'а если гомогенно). Хранится в `binds [5][]ecs.Entity` или `binds [5]bindEntry`.
  - `1` ... `5`: recall = selected = bind[i].
- Опциональный M9.5b (если останется время): merge двух Squad'ов в один. UX: ad-hoc selection с двумя Squad'ами + `T` пере-создаёт один Squad (старые despawn'ятся через каскад leave). Это естественно работает поверх `CreateFromUnits` — он сам вытащит юнитов из старых Squad'ов.

**Проверяем.**
- Marquee всех 12 → `T` → один Squad из 8 (т.к. SquadRosterSize=8, лишние 4 не влезают и остаются вне ростера). HUD показывает `Squads: 4` (3 старых уменьшенных + 1 новый из 8 + 4 одиночки... Хм. Или старые с Count=0 уже despawn'ятся? — да, P10 говорит «если ростер пустой, Despawn»). Итого: один новый Squad из 8 + 4 одиночки + 0 старых.
- `U` на гомогенном selected → бойцы вылетают, Squad с Count=0 → Despawn.
- `F1`-`F4` на разном Squad'е — формация мгновенно меняется на следующем тике FormationSystem'а.
- `Ctrl+1` → выделить новых юнитов → `1` → возврат к bind1.

### M9.6 — RadioNetwork scaffold

**Цель.** Структурный задел Phase 12. RadioNetwork-компонент создаётся при `CreateFromUnits`, поля заполняются по placeholder-логике. Никто не читает.

**Делаем:**
- `systems/squad_service.go::CreateFromUnits`: после спавна Squad'а добавить `RadioNetwork{Frequency: 0, HasRadioman: hasRadiomanInRoster(units), HQReachable: false}`.
- Helper `hasRadiomanInRoster(units []ecs.Entity) bool` — пробегает Equipment.Secondary каждого юнита, ищет сущность с `Radio` компонентом. **В Phase 9 такие сущности ещё не спавнятся** — функция просто возвращает false; компонент существует чтобы Phase 12 нашла куда писать.

**Проверяем.**
- `Filter[RadioNetwork]` возвращает количество = Squad'ов.
- Все `HasRadioman=false`, `Frequency=0`, `HQReachable=false`. Это ожидаемо — Phase 12 ещё не наступила.

### M9.7 — Render Squad'а + HUD + финальный test-pass

**Цель.** Игрок визуально понимает структуру отрядов. HUD показывает Squad census. Полный сценарий проходим.

**Делаем:**
- `main.go` (render):
  - Helper `drawSquadConnections(squad, roster, posMap, color)`: центр + линии к членам.
  - При selected с гомогенным Squad'ом — рисуем squad'е connections.
  - Hold `K` (или другая свободная клавиша — посмотреть на N/C/V/F/Y/G — `K` свободна) → рисуем все Squad'ы в их squad-color.
- `hud.go` (расширить expanded HUD): новые строки `Squads: N` + `Squad members: M` + `Soloists: S`.
- Finaltest scenario:
  1. Запуск — 3 Squad'а × 4 человека, видны в connections при hold-K.
  2. Squad1 (hold-K → виден синий клин/линия), кликнуть командира, ПКМ за домом 1 → весь Squad идёт в формации.
  3. Squad2 (Wedge), смена формации `F1` (на Line) — клин разворачивается в шеренгу.
  4. Squad3 (Column) идёт через бункер: ПКМ внутрь бункера → Squad спускается по лестнице (`NavService` через TransitionRegistry — Phase 7 проверено).
  5. Marquee Squad1 + Squad2 (8 человек), `T` → один Squad из 8, старые Squad'ы пропадают (Despawn). HUD: `Squads: 2 (1 new + Squad3)`.
  6. ПКМ далеко на новый Squad-of-8 → 8 человек идут одной толпой в формации Line.
  7. Один из Squad-of-8 застревает (положить дерево на путь через `X`-стэмп? — нет, X — heightmap-стэмп; используем уже существующее дерево). Через 5 сек он отстаёт > leash → выпадает из Squad'а в одиночки. HUD: `Soloists: +1`.
  8. `Ctrl+1` на Squad3 → `Ctrl+2` на новый-Squad-of-8 → отвлечься (WASD по карте) → `1` (selected возвращается к Squad3).
  9. `Ctrl+P` snapshot — проверить, что squad-системы укладываются в micro/sub-msec.
  10. Quit / restart — Squad'ы пере-создаются как при старте (детерминированно по cluster1/2/3); Phase 7 юниты пере-спавнятся, Squad'ы сверху.

**Проверяем.** См. сценарий — без визуальных дефектов / падений / роста памяти. Профайлер: `formation` < 100 µs, `squad_macro_path` < 200 µs (с throttle 1 сек, на каждом тике актуально только 1 из 60 кадров).

---

## Что считаем «закрытием Phase 9»

- `Squad` сущность с фиксированным `[8]ecs.Entity` ростером, обратная ссылка `SquadMember` на каждом бойце, инвариант поддерживается через `SquadService`.
- `FormationSystem` пишет `ActionQueue` бойцов на основе `FormationKind + SlotIndex + Spacing + Forward`. Четыре формации: Line / Column / Wedge / Loose.
- `SquadMacroPathSystem` строит и поддерживает `MacroPath` для центра отряда через `NavService.FindPath`. Throttle 1 сек + триггеры ре-плана.
- Cohesion: боец отстал от центра > `4 * Spacing` → `Leave` из ростера, продолжает жить как одиночка.
- Selection-aware ПКМ: гомогенный selected → приказ Squad'у; гетерогенный — Leave + Phase 7 индивидуальная логика.
- Горячие клавиши: `T` / `U` / `F1`-`F4` / `Ctrl+1`...`Ctrl+5` / `1`...`5`.
- `RadioNetwork` компонент существует с placeholder-данными; никто не читает.
- Render: hold-`K` рисует все Squad'ы как центры + linkage; selected'ный Squad подсвечивается всегда.
- HUD census: `Squads: N`, `Squad members: M`, `Soloists: S`.
- Phase 7 семантика для одиночек сохранена — игрок может игнорировать Squad-механику и продолжать управлять как раньше.
- Pipeline: `... → ground_stick → unit_movement → vision → squad_macro_path → formation → lod → ...` (squad-системы между vision и lod — после движения / sensing, перед LOD/render).

После этого — обновление ROADMAP, Phase 9 → ✅, переход к Phase 10 (Tactical AI). Phase 10 потребляет все деферренные «отдадим в Phase 10» отметки: Suppression-driven retreat, Cover Evaluation через Smart Objects (распределение по окнам), доктрины поведения, Cover Shadows, Scatter Protocol.

---

## Заметки на полях

- **Squad как единица архитектурного сдвига.** Phase 7 Phase-7 предлагал «UnitMovementSystem не меняется», и Phase 9 это держит — **вся формация делается через перепись ActionQueue**, низкоуровневый контроллер не знает о Squad'ах. Это позволит Phase 10 (Tactical AI) писать в ActionQueue приоритетно поверх FormationSystem (например, при Suppression юнит ныряет в укрытие, его ActionQueue перехватывает SurvivalInstinctSystem'ом, FormationSystem'у нечего перетирать пока Suppression держит). Чисто перенести владение очередью между уровнями.

- **`SquadRosterSize = 8`, не 4 и не 12.** Отделения пехоты в Cold War: советское отделение — 6-8, NATO squad — 9-13. Брать 8 — компромисс: помещаются классические pair-fireteam'ы (2×4) и стандартное отделение. Если когда-то понадобится 12 — это не структурная правка (`[12]ecs.Entity` вместо `[8]`, плюс пере-prove memory budget'а). Phase 9 — 8.

- **`SquadMacroPathSize = 8` waypoint'ов.** NavService возвращает path с шагом 1 м; средний путь по чанку (64 м) = ~64 шагов, после децимации шагом 6-8 м ≈ 8-10 точек. Если путь длиннее — ре-план «чанк за чанком»: дойдя до последнего waypoint'а, перезапросить путь от текущей позиции к Goal. На батальной сцене (Phase 11) с дальними приказами это естественно работает; для текущего test-сцены (~50 м) одного запроса хватает.

- **Cohesion leash = `4 * Spacing`.** Жёстко: при Spacing=2 м (Line/Column) leash=8 м; при Spacing=4 м (Loose) leash=16 м. Это работает потому что плотные формации требуют плотной cohesion, а Loose терпит больший разброс. Альтернатива — отдельный per-Squad параметр `MaxLeash`, но он ровно коррелирует с Spacing на текущих 4 формациях. Если Phase 10 заведёт «доктрины» с разными leash'ами на одной формации — превратим в поле.

- **FormationSystem чистит ActionQueue каждый тик — не «ломает» ли это Stop/Stance действия игрока?** Нет: `Stop` для Squad'а — это `HasGoal=false`, FormationSystem не пишет ActionQueue (некуда писать центр-waypoint). `Stance` — приказ ставится через личный ActionQueue юнита (через ad-hoc selection); если юнит в Squad'е и игрок отдал ему `Stance` — он выпадает из Squad'а через Leave (как с MoveTo, P8). Это согласовано: персональный приказ = leave squad.

- **Anchor вне Squad'ов навсегда.** Anchor — camera target / player marker, не боец. WASD двигает якорь как раньше, Squad-механика его не трогает. Если когда-то понадобится «командующий взвод во главе с игроком» (для Direct Control в Phase 7 P-заметке отвергнутого) — anchor можно будет вытворить как fake-юнит. Phase 9 этого не делает.

- **`SquadService.Despawn` может прийти как побочный эффект `Leave` (последний боец вышел).** Это рекурсия на один уровень: Leave → проверить Count, если 0 → Despawn → удалить Squad-сущность. Никакого риска (Squad-сущность сама себя не Leave'ит).

- **Производительность FormationSystem на 50 squad'ов × 8 бойцов = 400 ActionQueue write'ов / 100 ms = 4000 ops/sec.** Это ничто. Если когда-то profiler покажет дрейф — оптимизация «писать ActionQueue только при изменении offset > ε» вернёт коэффициент 5-10× (юнит обычно идёт куда нужно, offset меняется только при manoeuvre).

- **Squad без `WorldPos` — нюанс для рендера и сериализации.** Squad-сущность не появляется в стандартных пространственных запросах (`Filter[WorldPos, Squad]` — пустое). Render центра делается через явное вычисление в render loop'е (P11). Save/Load (Phase 16) — Squad сериализуется как `(roster []entityID, formation, macroPath)` без позиции.

- **Что произойдёт если Squad-сущность попадёт в чанк-eviction случайно?** Не попадёт — у неё нет `WorldPos`, `TerrainStreamingSystem` её не видит. `LODSystem` тоже игнорирует — отсутствие `WorldPos` исключает Squad из LOD-фильтров. Squad живёт пока не Despawn'ится явно.

- **`SquadMember.SlotIndex` — `uint8`, для Phase 9 хватает (max=7).** Если ростер расширится до 16+ — изменить на `uint16`. Текущий `[8]` отчётливо не ставит этот вопрос ребром.

- **Ад-hoc `selected` остаётся в `main.go` как было в Phase 7.** Никакого ECS-компонента `Selected{}` — это тактический shortcut для UI, состояние живёт в main loop'е. Squad является persistent ECS-абстракцией, selected — нет. Это разные слои: Squad живёт в мире, selected живёт в инпуте. Phase 14 (UI) может прокачать selected до tooling-уровня (карточки бойцов, etc.), но базисная семантика «selected = таргет ввода, Squad = персистентная группа» останется.

- **MacroPath децимация — параметр.** Сейчас «каждый N-й waypoint, N=4-8 в зависимости от Spacing». Если визуально макро-путь будет «дрожать» (резкие повороты на децимированных углах) — добавить line-of-sight smoothing (если из waypoint[i] виден waypoint[i+2], выкинуть [i+1]). Для Phase 9 — без smoothing'а, тестим как есть.

- **Что НЕ делать в `formationOffset` — детерминированный hash для `Loose`.** Hash берётся **только** от `SlotIndex`, не от squad-id и не от time. Это даёт «Боец с slot=3 всегда стоит вон там относительно центра» — формация дрожит только когда меняется состав (а тогда оно и должно «реорганизоваться»). Hash от squad-id дал бы разную геометрию у разных Loose-Squad'ов (визуально интересно), но усложнит дебаг.

- **Опциональная микро-фича — командирский маркер.** Slot 0 (командир) рисовать чуть выше или ярче. Для Phase 9 — пропустим, добавим в Phase 14 вместе с UI карточек.

- **Почему `K` для overlay, а не очередная буква вроде `Q` или `Z`.** Буквы N/C/V/F/Y/G заняты. K (Kommand?) свободна, рядом с другими текущими overlay'ями (K-N-V-F-Y) на клавиатуре, легко вспоминается.

- **Пост-сборка фиксы (после первой реализации).** На реальной сцене всплыли три бага и одно ограничение:
  1. **`free(): invalid size` при повторных `T`.** `SquadService.CreateFromUnits` держал `*CommandRoster` через `rosterMap.Get(newSquad)` поверх цикла, в котором `leaveInternal` мог триггерить `world.RemoveEntity` старого squad'а. Ark использует swap-on-remove (`storage.go:267`) — последний squad в той же archetype переносился в освободившийся слот, наш долгоживущий указатель становился staleм. **Фикс**: `CreateFromUnits` разнесена на 4 стадии — sanitise input (dedup + Alive), detach all from old squads, spawn new squad с заранее посчитанным `CommandRoster`-value, attach `SquadMember` на каждый юнит. Ни одной точки между стадиями pointer на Squad-archetype не пересекает архетипное изменение чужой squad-row. Аналогично пере-стейджена `Join`. `leaveInternal`/`Leave`/`Despawn`/`OrderMoveTo`/`Stop` теперь делают `world.Alive` guard на входе и на dead-entity путях — Ark `Map.Get` паникует на dead entity, поэтому подходить туда без проверки нельзя.
  2. **`T` ejected almost everyone сразу при создании squad'а.** Cohesion-чек (`dist > 4*Spacing`) бежал даже когда у squad'а ещё нет приказа — на спред-разбросанные кластеры это срабатывало мгновенно. **Фикс**: cohesion-чек активен только при `MacroPath.HasGoal=true`. Пока отряд idle, члены могут стоять сколь угодно далеко; первый же приказ запускает «эластичный поводок». Это согласуется с PHASE-9 P6 (текст про «отстал > X — выпал» подразумевает движение, не статичный idle).
  3. **Jittery / circular movement в плотных формациях.** Forward-вектор пересчитывался каждый тик из `(target - center) / mag`. Когда центр близко к target, любой микро-сдвиг центра разворачивал unit-вектор на десятки градусов; offset'ы вращались, MoveTo переписывалось каждые 100 мс на новый поворот; UnitMovementSystem не успевал доехать. **Фикс**: (а) Forward обновляется только когда `mag > formationForwardLockDist (5 м)`, иначе остаётся прошлый — на финальном участке формация фиксирует ориентацию. (б) `formationPushTolerance = 0.7 м` — если новый target отличается от последнего queue-MoveTo < 0.7 м, ActionQueue не переписывается, юнит докатывает уже стоящий приказ. Это ровно «писать ActionQueue только при изменении offset > ε» из старой заметки про производительность, только теперь не как оптимизация, а как фикс корректности.
  4. **`SquadCenter` принимал `*ecs.World`.** В Phase 9 юниты не умирают, но Phase 11+ будут. Подстраховались Alive-чеком внутри, чтобы render/AI не упали по dead-member в ростере раньше, чем кто-нибудь напишет «при смерти юнита снимать его из squad'а».

- **Wait-for-stragglers — future Phase 10.** Сейчас отряд движется с одной общей скоростью (UnitMovementSystem уважает `MaxSpeed[Stance]`, формация просто пишет target). В реальном бою выглядит дёрганно: одни ушли вперёд, другие отстали, формация рвётся, потом цельется. Хорошо бы периодически (раз в 1-3 сек) переоценивать кого-нибудь как лидера дистанции и:
  - либо тормозить «передних» (Stance Crouch или замедление за счёт лёгкого MoveTo назад к slot offset'у),
  - либо разрешать «отстающим» бежать быстрее (Stance модификатор, отрицательный stamina для Phase 11),
  - либо явно ставить отряду «привал» (pause MacroPath head advance, дать всем дойти до текущих offset'ов).
  Это пересекается с доктринами (Patrol vs Assault — разный leash к скорости), поэтому правильное место — Phase 10 Tactical AI вместе с SurvivalInstinctSystem.

- **«Скруглить» цель ActionQueue для squad'ов.** Связанная мысль: UnitMovementSystem.arrivalRadius=0.6 м. Когда squad дошёл до Goal и FormationSystem перестал писать (HasGoal=false после arrival), юниты пытаются добраться до своего offset'а из последнего записанного MoveTo. Если они в радиусе 0.6 м — поп, всё ок. Если ровно на границе — могут крутиться вокруг. Phase 10 / 11 может ввести «squad-arrived» статус и явный `ActionStop` всем членам — тогда они стоят неподвижно ровно где докатились, без последнего лёгкого jitter'а.
