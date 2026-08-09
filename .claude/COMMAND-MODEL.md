# Command Model

Подробная модель командования: как игрок выражает намерение, как намерение хранится, как оно попадает в исполнение, как UI это поддерживает. Углубление §3 / §4 / §5 / §6 / §8 / §9 GAMEDESIGN.md в виде топик-документа — здесь детали того, что в GAMEDESIGN зафиксировано тезисно.

`GAMEDESIGN.md` — что мы строим и принципы. Этот файл — *как именно* устроено управление. `ROADMAP.md` ссылается отсюда на конкретные фазы. `PHASE-N.md` плана опирается на этот документ как на референс.

Базовые референсы:
- **Sea Power: Naval Combat in the Missile Age** — плановое управление через карту, RoE-матрицы, per-weapon targeting.
- **Combat Mission Cold War** — реактивное поведение пехоты под огнём, TacOps-стиль orders.
- **Total War (новые части)** — ghost-preview placement, facing-drag, formation-aware cursor.
- **Wargame Red Dragon** — vector orders, multi-squad RoE matrix.

---

## §1. Паттерн «plan-then-watch»

Игра строится на презумпции: **большую часть времени игрок паузит и редактирует план, а real-time нужен для наблюдения и быстрых реакций**. Это та же DNA, что в Sea Power и WeGo-режиме Combat Mission. Все UX-решения этого документа исходят из этой презумпции — длинные order chains с условиями и ветвлениями неудобно вводить в реал-тайме, и это нормально. Игрок ставит паузу, планирует, отпускает.

**Уведомления вместо принудительной паузы.** Принудительная auto-pause на каждое значимое событие (контакт, потеря бойца, конец цепочки) ломает поток с другой стороны: игрок теряет контроль над темпом. Вместо этого:

- **Map ping**: при значимом событии на 2D-карте появляется кратковременная пульсация в точке события (контакт, ранение, фиксация цели, конец Order'а). Цвет ping'а кодирует тип события.
- **Event log panel** (Phase 21): автоскроллящаяся лента событий (timestamp + actor + событие). Игрок может проскроллить лог, если что-то пропустил.
- **Speaker icon** (опционально, Phase 25): короткий звуковой сигнал на ping типа.

Auto-pause остаётся как **opt-in настройка** в options: матрица «на каких событиях паузить». Дефолт — ничего не паузит. Игрок включает, например, «pause on first enemy contact» если ему так удобнее. Это примирение «liberty потока» с «нужно реагировать».

**Plan-mode affordances.** В UI явного отдельного «plan mode» режима нет — пауза просто делает игру медленной. Но при `app.TimeScale == 0` UI **может** показывать дополнительные visualization'ы: ghost-preview позиций с большей детализацией, ETA до waypoint'ов вдоль маршрута, длинные textual tooltip'ы, проектируемые линии огня. Это можно постепенно расширять без архитектурных изменений — просто условный branch в render-pass'ах: `if app.Paused { drawDetailedPreview() }`.

**Speed compression** (1×/2×/4×/8×) — это ускорение реал-тайма для длинных переходов. Игрок не паузит, чтобы пропустить 5-минутный марш — он жмёт `+` пока что-то не произошло, потом возвращается на 1×. По сути это «time-skip с возможностью прерывания на ping». Speed compression — не альтернатива паузе, **оба нужны**.

---

## §2. Три слоя управления

Центральная таксономия. Игрок взаимодействует с симуляцией через три **разных по семантике слоя**, каждый со своим UI surface'ом, lifecycle'ом и моделью данных. Их смешение — главный источник UX-путаницы в RTS, и мы их явно разделяем.

### Слой 1: Orders — императивные действия

**Что**: дискретные команды с lifecycle'ом — «иди туда», «сядь в технику», «открой огонь по этой точке», «возведи окоп здесь».

**Где живёт**: отдельные ECS-сущности (`Order` + `OrderKind` + `OrderState` + ...). Очередь приказов — linked list через `OrderChain.Next`, head на entity-владельце через `OrderQueueHead.First`. Архитектура залочена в Phase 11.

**UI surface**: ПКМ (tap / hold / drag) на 3D или карте. Pie menu для выбора kind'а. Order markers визуализируются в обеих view'ах. Inspector показывает текущий + queued orders.

**Lifecycle**: `Issued → InProgress → Completed/Cancelled/Failed`. Pop'ается из очереди по завершении.

### Слой 2: Standing rules — постоянные настройки

**Что**: правила, действующие непрерывно — «можно ли стрелять», «как двигаться», «можно ли менять stance автоматически», «активный радар включён». Это **состояние**, не глагол.

**Где живёт**: компоненты на Squad / Vehicle / Unit (`EngagementRules`, `MovementProfile`, `BehaviorRules`, `VehicleSystems`). Не имеют lifecycle'а — просто читаются другими системами при принятии решений.

**UI surface**: квик-бары / toggle-панели в Inspector'е. Пресеты для типичных комбинаций. Per-role / per-template default'ы.

**Lifecycle**: меняются мгновенно по клику. Применяются ко всему текущему/будущему поведению entity. Не имеют отмены — игрок просто переключает обратно.

### Слой 3: Per-weapon intent — целеуказание оружию

**Что**: «вот этим конкретным оружием — по этой конкретной цели». Sea Power-стиль fire-control. Опциональный override над unit-level Order'ом.

**Где живёт**: опциональный компонент `OrderParamWeaponPref{WeaponEntity}` на Order entity. Если присутствует — WeaponSystem обязан использовать указанное оружие, а не выбирать сам.

**UI surface**: weapon-bar в Inspector'е (горизонтальная панель снизу). Клик на weapon row → cursor становится «aim mode» для этого оружия → следующий клик создаёт Order с weapon-pref. Без weapon-bar клика — стандартный unit-level Order, WeaponSystem сам решает, чем стрелять.

**Когда нужен**: техника с разнотипным вооружением (танк: пушка vs пулемёт vs ZSU vs ПТРК), снайпер, у которого SVD vs АКС vs гранаты, расчёт ПТ.

### Почему разделение принципиально

Игрок интуитивно понимает разницу: «настройки» (всегда такие) vs «команды» (сделай вот это). Их UI surface'ы **физически разделены**: квик-бары toggle'ятся в Inspector'е, команды выдаются ПКМ на карте/3D. Если их смешивать — игрок не понимает, почему юнит «забыл» приказ (а он не забыл, он просто перешёл в другой режим RoE).

Это разделение также облегчает per-role default'ы: при `RoleService.AssignRole(unit, RoleSniper)` мы устанавливаем standing rules (HoldFire RoE, Crouch stance default), но **не выдаём Orders**. Юнит начинает в правильном режиме без команд.

---

## §3. Order graph: per-owner chain + barriers

Ордер-таксономия в §3 GAMEDESIGN — линейная (chain через `Chain.Next`). Реальные тактические сценарии — DAG'и: «грузовик едет, высаживает пехоту, едет к базе Б; пехота тем временем штурмует». Это две **независимые ветки**, синхронизованные одной точкой (Dismount).

### Модель данных

Tree — это **проекция**, не структура хранения. На уровне ECS:

```go
// На Order-entity (Phase 11 + расширение для Phase 17):
type Order struct{}
type OrderOwner struct{ Entity ecs.Entity }     // squad ИЛИ vehicle ИЛИ unit
type OrderChain struct{ Next ecs.Entity }       // следующий в собственной queue
type OrderGroup struct{ GroupID uint32 }        // общий ID для joint orders (Phase 19)
type OrderJoint struct {                         // joint-ордер (Phase 17, новое):
    Participants [4]ecs.Entity                  // entity'и участников
    PartCount    uint8
    BarrierKind  BarrierKind                    // ArriveAll / EnterVehicle / ExitVehicle
}
```

Каждая entity (squad / vehicle) держит свою линейную queue через `OrderQueueHead.First`. **Joint orders ссылаются на множество participant'ов**, и каждый participant имеет указатель на этот же Order в своей queue (один Order entity — несколько queue'ов на него ссылаются).

### Barrier semantics

Joint Order — это **synchronization barrier**. Семантика:
- Все participants должны достичь pre-condition'а (например, грузовик прибыл к точке высадки + дверь открыта).
- Когда все ready — barrier crosses, Order переходит в `Completed`.
- Каждый participant продвигается по своему `Chain.Next` (для каждого свой own next).

Phase 11 такого нет — все Order'ы single-owner. Joint Orders с barrier'ом — Phase 17 (Vehicles+Infantry interop), где Embark/Dismount естественно требуют синхронизации.

Примеры barrier kinds:
- `BarrierArriveAll` — все participants пришли в Order.Target.Pos. Используется для concentrate-and-attack.
- `BarrierEnterVehicle` — пехота села в технику. Embark.
- `BarrierExitVehicle` — пехота высадилась. Dismount.

### Tree visualization (UI projection)

Селекция игрока определяет, что рисовать. Алгоритм:

```
1. selectedSet := игроком выделенные entity'и
2. Для каждой entity ∈ selectedSet:
     отрисовать её queue как линейную цепочку Order-маркеров
3. Для каждого joint-Order, где participants ∩ selectedSet ≥ 2:
     отрисовать как «слитый узел» с branching после
```

Визуально: множественные ветки сходятся к barrier-узлу, продолжаются после него. Это и есть git-tree.

Для одной entity в selectedSet — видна только её ветка (joint-Order рисуется как «передача в/из» с placeholder'ом для другого participant'а). Для всех participants выделено — полный tree.

Пример сценария (грузовик + пехота):

```
Грузовик:  M1 ──── M2(грунт) ──── M3(точка X) ┐
                                              ├── [Dismount, Joint(truck, squad)]
Пехота:    Inside ─── Inside ─── Inside ──────┘
                                              ↓
Грузовик:                                 M4(база Б)
Пехота:                                   M5(марш) ─── M6(crouch+cover) ─── M7(arrival)
```

Селекция = {truck} → видна верхняя ветка + Dismount как «передача пехоты». Селекция = {squad} → видна нижняя ветка + Dismount как «выход из техники». Селекция = {truck, squad} → полный tree.

### Edit operations (Phase 21)

Маркер Order'а на 2D-карте (или в 3D) кликабелен. ПКМ-tap на маркер → mini-pie-menu:
- **Insert before/after** — вставить новый Order рядом
- **Delete** — удалить из chain
- **Edit params** — изменить per-Order параметры (Pace override, Stance, PathStyle)
- **Promote to joint** — превратить в joint Order (требует другого selected participant'а)
- **Drag to relocate** — переместить waypoint в пространстве

Это явно Phase 21 (UI expansion) — требует pie-menu infrastructure, marker hit-testing, editing widgets. Архитектурно — манипуляция с linked-list (Insert: новая Order entity вставляется между prev и next, перенаправляя `Chain.Next`).

### Cancel cascade

Cancel chain'а — каскадный по `Chain.Next`. Cancel joint Order'а — отменяется у всех participant'ов одновременно (барьер «провален» для всех). UI: shift+click на Order marker → cancel этот Order; ctrl+click → cancel этот + все после в chain.

---

## §4. Standing rules: что куда живёт

Полная таблица standing-rules компонентов. Каждый — отдельный компонент, отдельный UI quick-bar в Inspector'е, отдельный per-role default через `RoleService`.

### EngagementRules (Squad / Unit override)

```go
type EngagementRules struct {
    Mode           EngagementMode  // HoldFire / ReturnFire / FreeFire
    FireOnInf      bool
    FireOnArm      bool
    FireOnAir      bool
    FireOnStruct   bool
    Standoff       StandoffPolicy  // Close/Med/Long/Any (Phase 14+)
    SectorYaw      float32         // optional cone center (Phase 14+)
    SectorHalfDot  float32         // optional cone half-angle (Phase 14+)
}
```

Phase 13 запекает Mode + 4 toggle'а. Standoff/Sector — Phase 14, когда weapon ranges и aiming имеют смысл.

UI: `[Hold | Return | Free]` row + `[ ] Inf [ ] Arm [ ] Air [ ] Struct` toggle row. Per-unit override — Phase 21.

### MovementProfile (Squad / Unit override)

```go
type MovementProfile struct {
    Pace       Pace        // Walk / Run / Sprint
    Stance     StanceCode  // Stand / Crouch / Prone (default «к чему возвращаться»)
    Posture    Posture     // Standard / Quiet
    PathStyle  PathStyle   // Direct / RoadPrefer / RoadAvoid / CoverSeek
}
```

Phase 13 включает все 4 рычага. Posture без эффекта до Phase 15 (audio detection). Stance в MovementProfile — это «default к чему возвращается squad когда action queue пустая»; Action.Stance (Phase 7) — это разовое переключение для текущей задачи.

UI: 4 dropdowns + 6 preset chips («Default», «Cautious», «Rush», «Sprint», «Stealth», «Prone Crawl»).

**Sneak — это preset, не Order kind.** В §3 GAMEDESIGN таксономии Order kinds есть `Sneak` — после Phase 13 он удаляется как Order kind и становится **preset для MovementProfile** (Walk + Crouch + Quiet + RoadAvoid + EngagementRules.Mode=HoldFire). Hotkey `Ctrl+RMB` применяет этот preset перед `MoveTo` через `OrderParamMovementProfile` override.

### BehaviorRules (Squad)

```go
type BehaviorRules struct {
    AllowAutoReposition  bool     // двигаться к лучшему cover'у при подавлении (Phase 15)
    AllowAutoStance      bool     // авто-падать в Prone под огнём (Phase 15)
    HoldUntilOrdered     bool     // ничего не делает без явного приказа
    AllowReturnFire      bool     // допускать ли реактивный огонь (доп к EngagementRules)
    SuppressionThreshold float32  // 0..1, при каком уровне starts overriding
}
```

Phase 13 включает поля как scaffold, реальные читатели — Phase 15 (Tactical AI). Это **gate'ы** для реактивного поведения: если `HoldUntilOrdered=true`, SurvivalInstinct не ставит TacticalOverride маркер. Если `AllowAutoReposition=false`, ScatterProtocol не работает.

UI: 4-5 toggle'ей в Inspector'е, секция «Behavior».

### VehicleSystems (Vehicle)

```go
type VehicleSystems struct {
    ActiveRadar     bool  // Phase 16+
    AutoSmoke       bool  // авто-постановка дымовой завесы при попадании
    ReverseToCover  bool  // авто-отъезд назад при подавлении
    EngineIdle      bool  // двигатель включён (audio sig + готовность)
}
```

Только на Vehicle entity'ях. Phase 16+ scaffold, реальные читатели — Phase 16/17.

### Per-role default'ы

`RoleService.AssignRole(unit, role)` устанавливает standing rules согласно таблице:

| Role | EngagementRules.Mode | Targets | MovementProfile.Stance | BehaviorRules notes |
|---|---|---|---|---|
| Leader | FreeFire | Inf+Arm | Stand | AllowAutoReposition |
| Rifleman | FreeFire | Inf+Arm | Stand | AllowAutoReposition + AllowAutoStance |
| MachineGunner | FreeFire | Inf | Crouch | !AllowAutoReposition (deployed) |
| Grenadier | FreeFire | Inf+Arm+Struct | Stand | AllowAutoReposition |
| Sniper | HoldFire | (none auto) | Prone | HoldUntilOrdered |
| ATGunner | HoldFire | Arm | Crouch | HoldUntilOrdered |
| Medic | ReturnFire | Inf | Crouch | AllowAutoStance, !AllowAutoReposition (если рядом раненый) |
| RadioOperator | ReturnFire | Inf | Crouch | !AllowAutoReposition |
| Engineer | ReturnFire | Inf | Stand | AllowAutoStance, !AllowAutoReposition (если строит) |
| DemoMan | ReturnFire | Inf | Crouch | AllowAutoStance |

Squad-level default берётся из template'а (агрегировано от командира + tactical-priority бойцов). Per-unit override остаётся в данных, но UI surface'ится только в Phase 21.

**`AssignRole` не overwrite'ит уже set'нутые standing rules.** При первом spawn'е юнита — устанавливает defaults. При reassign (Engineer → Sniper) — *не* меняет уже отредактированные игроком настройки. Это предотвращает неожиданный сброс настроек при role-change.

**Что НЕ делаем в Phase 13:**
- Doctrines как сложные standing-rule presets (Patrol/Assault/Stealth/Defense) — Phase 15.
- Per-unit toggle UI — Phase 21.
- VehicleSystems scaffold — Phase 16+.
- Standoff / Sector в EngagementRules — Phase 14+.

---

## §5. Per-weapon control: weapon-bar и intent

### Концепция

В Sea Power каждый клик «оружие → цель». В нашем тактическом RTS у пехоты обычно 1-2 ствола, у техники 3-5 систем. Поэтому per-weapon control — **опциональный override** над unit-level Order'ом, не замена.

### UI: weapon-bar в Inspector'е

Снизу Inspector panel'а (или отдельной секцией) — горизонтальная панель оружия всех selected entity'ей. Каждое оружие — row или icon с:
- WeaponKind icon (или text label)
- Owner-unit ShortLabel (R3, MG, SN — чтобы понимать «какой именно из 4 АК»)
- Ammo bar / count
- Status: Ready / Reloading / Out (Phase 14 — реальные значения)

Группировка: одинаковые `WeaponKind` объединены в строку «AK-74 ×4 [4 owners]» с раскрытием на клик. Различные системы (Pistol vs Rifle vs AT) — отдельные rows.

### Targeting flow

1. Игрок кликает weapon row в weapon-bar.
2. Cursor переходит в **aim mode** для этого оружия (визуально: специфический cursor + отображается weapon range circle на карте).
3. Следующий ПКМ-click на цели → создаётся `AttackTarget` Order на owner-unit с `OrderParamWeaponPref{WeaponEntity}`.
4. Если цель — unit / vehicle: standard target Order с weapon-pref.
5. Если цель — точка: `SuppressFire` Order с weapon-pref (стрелять в этом направлении).
6. ESC отменяет aim mode.

### Fall-back semantics

Если `OrderParamWeaponPref.WeaponEntity` стал invalid (ammo вышел, оружие сломано / lost'но при смерти юнита) — WeaponSystem fallback'ит на default weapon из Equipment.Primary. Order не cancel'ится, просто weapon-pref ignored.

### Multi-tank example

Player выделяет 2 танка → weapon-bar: `[Танк-A: 125mm HE ×8 / 7.62 ×500] [Танк-B: 125mm HE ×6 / 7.62 ×400]`. Sequential targeting:
- Click `Tank-A 125mm HE` → aim mode → click target X → Order issued.
- Click `Tank-B 125mm HE` → aim mode → click target Y → Order issued.
- Real-time или с паузой — никакой разницы для логики.

Concentrate-fire (оба танка по одной цели): click `Tank-A 125mm HE` → click target → click `Tank-B 125mm HE` → click тот же target. Два Order'а на разных tank'ах, оба с weapon-pref'ом 125mm, target — один и тот же.

### Когда weapon-bar появляется

- **Phase 13**: weapon-bar НЕ нужен. Phase 13 — только standing rules, без огня.
- **Phase 14 (Combat)**: weapon-bar появляется как часть Inspector'а вместе с реальной WeaponSystem. Это естественно — вместе с огневой логикой.
- **Phase 21 (UI expansion)**: расширение weapon-bar — multi-target queue, ammo display polish, reload feedback, weapon-disable toggle (вкл/выкл оружия как standing rule).

### `OrderParamWeaponPref` в Phase 14

```go
type OrderParamWeaponPref struct {
    Weapon ecs.Entity  // конкретное оружие
}
```

Опциональный компонент на Order. WeaponSystem при resolve'е цели проверяет: если есть pref + weapon valid + ammo > 0 → использует его; иначе — default selection.

---

## §6. UX-механизмы input'а

### ПКМ-grammar (расширение §4 GAMEDESIGN)

| Действие | Эффект |
|---|---|
| `RMB-tap` | Smart default Order (через resolver §3 GAMEDESIGN) |
| `RMB-hold > 200ms` | Pie menu — полный список Order kinds |
| `RMB-drag` | Vector order — направление + длина (facing / spread / sector) |
| `Shift+RMB-tap` | Append в queue |
| `Shift+RMB-drag` | Append vector order |
| `Alt+RMB-tap` | Force AttackMove |
| `Ctrl+RMB-tap` | Force Sneak preset (MovementProfile override через `OrderParamMovementProfile`) |
| `Double-RMB` | MoveTo + Pace=Sprint override |
| `RMB-tap on Order marker` (Phase 21) | Marker context menu (insert/delete/edit) |

### Ghost preview (Total War-style)

При hover'е ПКМ (без нажатия) на местности — отображается ghost-расстановка юнитов в текущей формации:
- Squad из 8 человек в Line формации → 8 ghost-cube'ов выстроенных в линию перпендикулярно direction-from-squad-center-to-cursor.
- При зажатии ПКМ + drag → линия rotate'ит вместе с курсором, формация ориентируется явно. Отпускание → committed.
- Если Order — Garrison (cursor на здании) → ghost'ы распределены по окнам здания (per-window slot из CoverDirection).
- Если Order — OccupyTrench → ghost'ы вдоль polyline'а окопа.
- Если Order — DefendPosition → ghost'ы в формации + большая arc (sector facing'а).

Реализация: при hover вычисляется `FormationData.ResolveSlots(centerWorldPos, facingYaw)`, draw'им ghost-meshes / ghost-circles. Дёшево (formation resolve уже existing функция).

**Phase 13.6 candidate** — отдельная маленькая под-фаза после UI L2 (Phase 13.5). Не блокирует Combat.

### Pie menu

ПКМ-hold > 200мс → полупрозрачный круг сегментов вокруг курсора. Каждый сегмент = Order kind. Курсор на сегмент + отпустить = выбор. В центр = cancel.

Дизайн pie menu (сегменты):
- 4-8 сегментов в зависимости от контекста
- Иконки + tooltips
- Disabled segments (серые) — например, Build не показывается без Engineer в squad'е

Pie menu **контекстно-зависим**:
- Hover на враждебной entity → segments: AttackTarget, SuppressFire, Capture, Mark
- Hover на дружественной vehicle → segments: Mount, Escort, Repair (если Engineer)
- Hover на здании → Garrison, ClearBuilding, Demolish, Defend
- Hover на земле → MoveTo, AttackMove, Sneak, Sprint, DefendPosition, Patrol

### Marker context menu (Phase 21)

ПКМ-tap на existing Order marker → mini pie-menu:
- Insert before / after
- Delete
- Change params (movement profile override для этого Order'а)
- Promote to joint
- Drag-mode: следующий drag перемещает waypoint

Пример из реального сценария: игрок поставил `MoveTo(end of forest)`, потом кликнул на этот marker → pie → «Insert after» → выбрал `DisperseOnLine` → drag по границе леса → второй Order появился в chain. Это и есть «нажал на маркер, добавил действие» из жизненного сценария.

Phase 21 candidate — требует marker hit-testing + pie infrastructure. Не блокирует базовое управление.

### Polyline / arc placement (Phase 19/21)

«Распределить по линии» — ПКМ-hold-drag по местности рисует arc, юниты распределяются равномерно по точкам. Используется для:
- Disperse along forest edge
- Defensive line на гребне холма
- Skirmish line при approach

Архитектурно: при drag'е сэмплируется N точек по направлению (или по surface-projected curve), генерируется N sub-Order'ов (по одному на slot ростера). Это `OrderKind=DisperseOnLine`, новый kind.

### Hotkey shortcuts (presets для standing rules)

Для quick-toggle MovementProfile presets — отдельный hotkey-namespace, не пересекающийся с `Ctrl+1..5` (squad recall):
- `[`, `]` — prev/next preset в MovementProfile
- `'` (или подобное) — toggle Posture между Standard и Quiet
- Z / X / C — Stand / Crouch / Prone (если не зарезервированы)

Окончательный mapping — Phase 13 при build'е UI.

---

## §7. Реактивное поведение: фактический контракт (актуально с Phase 19.5)

Реактивное поведение живёт на трёх ярусах, и все три меняют **как** исполняется
приказ, никогда — **что** это за приказ. Голова `OrderQueueHead` не переписывается
автоматикой ни при каких условиях (P1 фазы 19.5).

### Цепочка за тик

1. `contact` — сенсорный детект; пишет `Awareness.LastSeen` (**только чужие**:
   FIFO это память о противнике, а не перепись — своими она переполнялась) и
   апсертит `Contact`-сущности.
2. `weapon` — попадания и промахи рядом порождают `DangerEvent`
   (`DangerBulletImpact`, `DangerDamageTaken`, `DangerExplosion`),
   `applySplashDamage` дополнительно ставит `BlastMark`.
3. `threat` — сворачивает `DangerBuffer` в `Threat.Total` + **кластеры**
   `Threat.Clusters[3]{Dir,Dist,Weight,LastAt}` (угловое слияние ~60°, затухание
   0.08/с); агрегация ≥2 взрывов в радиусе поднимает зону `UnsafeArea` с TTL.
4. `squad_brain` — **операционный ярус**: раз в ~1.5 с пишет `SquadPlan`
   {Mode, Phase, Floor, WaveMask, Timer, Anchor} на Squad. Режимы:
   `Bounding` (марш под огнём волнами), `Relocate` (ростер в зоне обстрела),
   `ClearSeq` (секвенсор ClearBuilding: StackUp → Enter → Sweep этажа k).
5. `survival_instinct` — **тактический ярус на юнита**: скорит ПОЗИЦИИ (cover-слоты,
   клетки траншей, терренная дефилада через `CoverMap.DirMask`, тень корпуса
   техники) против всех живых кластеров и ставит `TacticalOverride`
   {Reason, CoverPos, AssignedSlot, SavedKind/SavedTarget}. Нет позиции → фолбэк
   «выйти из полосы огня» перпендикулярно главному кластеру.
6. `stance_controller` — стойка по `Threat.State`; пока боец не дошёл до выбранного
   места, стойка не ниже crouch (в зоне обстрела — стоя): к укрытию бегут, а не ползут.
7. `formation` / `unit_movement` — исполнение: члены с `TacticalOverride` пропускаются
   (их очередью владеет инстинкт), корпуса с активным `VehicleOverride` — тоже
   (рефлекс владеет драйвером). `SquadPlan` меняет цели слотов: волна прикрытия
   стоит, движущаяся идёт на `Anchor`, ClearSeq подставляет стек/этаж.

### Гейты (стоячие правила — это гейты, не поведение)

- `HoldUntilOrdered=true` ⇒ инстинкт не ставит маркеров (стойка остаётся), и мозг
  не берёт Bounding/Relocate. `AllowAutoReposition=false` ⇒ то же плюс выключенный
  ScatterProtocol.
- `AllowReturnFire` читается в `shouldFire`: при доктрине HoldFire боец отвечает на
  огонь, который реально принимает.
- **Грейс-окно 4 с** после свежего приказа: новый override не ставится (исключение —
  squad Scrambling). Игрок только что сказал — автоматика молчит.
- Секвенсирование явного приказа (ClearSeq) стоячими правилами НЕ гасится: это
  исполнение приказа, а не автономия.
- Боец **внутри футпринта здания** инстинктом не перемещается: все кандидаты скорера
  выведены из рельефа, поэтому «укрытие» для него означало сойти с этажа или выйти
  из комнаты, куда его привёл приказ. Интерьерные позиции — хвост P4.

### Приказ переживает перехват

`TacticalOverride` запоминает вытесненное действие (`SavedKind`/`SavedTarget`) и
возвращает его при снятии маркера; Order всё это время остаётся `InProgress`.
Отменяет приказ только его собственная терминальная логика (Completed / Failed —
например, watchdog техники репортит «no path»), а не реактивное поведение.
Освобождение корпуса из рефлекса и выход из Relocate ставят
`FormationData.ReformPending` — отряд переформировывается там, где оказался, и
продолжает тот же приказ.

### Что видит игрок

Строка override'а в инспекторе юнита (Reason ASCII), строка `Plan:` в инспекторе
отряда (для ClearSeq — фаза и этаж), полоса режима на групповом боксе squad bar,
амбер-плашка рефлекса на карточке корпуса, событие в EventLog при взятии управления.

### Мёртвые поля (читателя нет — в UI не показываем)

- `EngagementRules.Standoff` — потребителя нет; чип убран из Behavior-панели
  (19.5 P2), вернётся вместе с TargetPriority.
- `Occupancy` на дверях/окнах — Smart-Object данные пишутся, но занятость укрытий
  инстинкт ведёт приватным `occupancyClaim`; публичным ledger'ом `Occupancy`
  станет, когда появится второй писатель (Phase 21).
- `EngagementRules.SectorHalfDot/SectorYaw` — читаются в `shouldFire`, но редактор
  сектора приезжает с DefendPosition; в панели строка read-only.

---

## §8. Notification system: ping + event log

Замена auto-pause'а. Принцип: события не ломают поток, но видны.

### Map ping

Кратковременная пульсация на 2D-карте в точке события. Длительность 1-2 секунды, fade-out. Цвет:

| Цвет | Тип события |
|---|---|
| Bright red | Enemy contact (first sight) |
| Dark red | Friendly hit / death |
| Yellow | Friendly suppression / pinned |
| Cyan | Order completed |
| Green | Arrival at waypoint |
| Magenta | Building entered / cleared |
| White | Manual mark by player |

Ping — это short-lived ECS entity (`MapPing{Pos, Color, SpawnTime, Duration}`), MapRender system рисует все active ping'и. Реализация — Phase 21 (вместе с event log).

### Event log

Автоскроллящаяся лента в panel'е. Каждая строка:
```
[12:34:56] Squad Alpha: enemy contact at grid 142, 87
[12:35:01] Squad Bravo: arrived at waypoint
[12:35:14] Truck-3: under suppressive fire
```

Хранится последние N=200 событий в ring buffer. Фильтры (по squad / по типу / по severity). Click на строку → camera focus на pos события + ping replay.

Реализация — Phase 21.

### Opt-in auto-pause matrix (опционально)

В options panel'е:
- `[ ] Pause on first enemy contact (any squad)`
- `[ ] Pause on friendly KIA`
- `[ ] Pause on order completed (any squad)`
- `[ ] Pause on order failed`
- `[ ] Pause on building entered`

Дефолт — все unchecked. Игрок включает по своему предпочтению. Имплементация: при event'е, который должен ping'нуть, проверяется matrix → если соответствующий toggle on → `app.TimeScale = 0`.

Реализация — Phase 21 / 25 (вместе с options panel).

---

## §9. Фазовая раскладка

Команд-модель распределена по фазам. Это ре-структурирует ROADMAP (см. отдельный update там).

### Phase 13 — Movement & Engagement standing rules

- `MovementProfile` component на Squad (Pace, Stance default, Posture, PathStyle)
- `EngagementRules` component на Squad (Mode + 4 target toggles, без Standoff/Sector)
- `BehaviorRules` component на Squad (4 toggle'я + suppression threshold) — scaffold, читателей нет до Phase 15
- `Stamina` component на Unit (Current, Max, RecoverRate)
- Per-role default'ы через `RoleService.AssignRole` (таблица из §4)
- NavService.FindPath принимает PathStyle, NavGrid bake расширяется distance-to-coverslot
- UnitMovementSystem читает Pace × Stance × Stamina
- Inspector quick-bars: 6 movement preset chips + 4 dropdowns + 3-button RoE Mode + 4 target toggles + 4 BehaviorRules toggles
- `OrderParamMovementProfile` опциональный компонент на Order — override squad default на длину Order'а
- Sneak removed as Order kind (становится preset для `Ctrl+RMB`)

### Phase 13.5 — UI L2 polish (scrollable + resizable panels)

Bumped перед Ghost preview в ответ на Inspector overflow после Phase 12/13. Минимум: scrollable содержимое + resizable splitter'ы между панелями + мини-persistence сплит-пропорций. Без новых панелей (L3 = Phase 21), без split/dock (L4 = Phase 22). См. `GAMEDESIGN.md` §9.

### Phase 13.6 — Ghost preview & facing-drag

Маленькая под-фаза после L2. Total-War style ghost при ПКМ-hover, facing-drag для DefendPosition / arrived-facing для MoveTo, formation-aware ghost (Garrison → распределение по окнам, OccupyTrench → вдоль polyline'а).

### Phase 14 — Combat (изменения от текущего ROADMAP)

В дополнение к запланированному:
- Weapon-bar в Inspector'е — projection всех Equipment.Primary/Secondary selected entity'ей
- `OrderParamWeaponPref` опциональный — per-weapon targeting
- WeaponSystem читает `EngagementRules` при decision-making
- Map ping system + event log scaffolding (если не выделено в Phase 21)

### Phase 15 — Tactical AI (изменения от текущего ROADMAP)

В дополнение к запланированному:
- SurvivalInstinct читает `BehaviorRules` как gate'ы
- Posture получает реальный effect (audio detection, vision modifier)
- Standoff / Sector в EngagementRules становятся «живыми» (читатель — TargetPriority)
- Doctrines (Patrol/Assault/Stealth/Defense) реализуются как macro-presets для standing rules + reactive override modulation

### Phase 17 — Vehicles+Infantry interop (изменения)

В дополнение к запланированному:
- Joint orders как multi-owner concept (`OrderJoint{Participants, BarrierKind}`)
- Synchronization barriers: BarrierEnterVehicle / BarrierExitVehicle / BarrierArriveAll
- `OrderQueueHead` теперь может содержать ссылку на joint Order (один Order — несколько queue'ов на него)
- Embark / Dismount как первые joint Order kinds

### Phase 19 — Multi-squad coordination (изменения)

В дополнение к запланированному:
- Joint orders расширяются на N squad'ов
- `OrderKind=DisperseOnLine` для polyline placement
- Vector orders определяют ширину фронта
- Pie menu для multi-squad context

### Phase 21 — UI expansion (изменения)

В дополнение к запланированному:
- Tree visualization Order'ов в Inspector (git-tree projection из selectedSet)
- Marker context menu (insert / delete / edit / drag-relocate)
- Map ping system + event log panel
- Opt-in auto-pause matrix
- VehicleSystems UI panels (Phase 16-зависимое)
- Per-unit standing rules override surface

---

## §10. Открытые вопросы

То, что пока **не решено окончательно** — записываем здесь, чтобы вернуться при имплементации.

1. **Weapon-bar UX detail**: групповая агрегация одинаковых WeaponKind'ов vs raw list. Если в squad'е 4 АК-74 — показать одну строку «AK-74 ×4» или 4 отдельные? Recommendation: aggregated с раскрытием на клик, но решим в Phase 14 при первом playtest'е.

2. **Stamina visual**: bar над unit'ом всегда, или только когда < 100%, или вообще нигде (только в Inspector single-unit view)? Зависит от того, насколько Stamina critical для UX. Recommendation: bar при < 80% + per-unit row в Inspector. Финализируем в Phase 13.

3. **DemoMan vs Engineer overlap** (carry-over из Phase 12): нужны ли две отдельные роли или одна с flag'ом `CanDemolish`. Зависит от Phase 18 (Engineering) — там увидим, будет ли distinct поведение.

4. **HoldUntilOrdered vs MovementProfile.Pace=Walk**: пересекаются ли семантически? Например, sniper с HoldUntilOrdered=true и Pace=Sprint — что значит? Recommendation: HoldUntilOrdered overrides Pace (если HoldUntilOrdered, юнит не двигается без Order'а вообще; Pace применяется когда Order активен).

5. **Cancel cascade UX**: shift+click на Order marker = cancel этот Order, ctrl+click = cancel этот + все после. Или ctrl+click = cancel весь chain (включая текущий)? Recommendation: shift = single, ctrl = all-from-here. Финализируем в Phase 21.

6. **Joint Order ownership semantics**: один Order entity — несколько queue'ов на него ссылаются. Что если один participant потерян (squad уничтожен)? Joint Order переходит в Failed для остальных, или продолжает с уменьшенным participant count? Recommendation: если barrier requires all → Failed. Если barrier — «достаточно одного» → продолжает. Финализируется в Phase 17.

7. **PathStyle CoverSeek implementation**: bake distance-to-nearest-coverslot в NavGrid (extra byte per cell), или query CoverMap on FindPath call? Recommendation: bake (no runtime overhead, +1 KB per chunk). Фиксируем в Phase 13.

8. **Pie menu в touch / controller input**: пока не предусмотрено, не приоритет. Фиксируем как mouse+keyboard only.

9. **Ghost preview performance**: на 8 squad'ах × 8 человек = 64 ghost'а одновременно при multi-select hover. Если это медленно — culling по distance / только squad-center marker без per-unit ghosts. Финализируется в Phase 13.6.

10. **Hotkey collision**: `Ctrl+1..5` уже занят squad recall. MovementProfile presets нужны новые namespace (предложение: `[`, `]`, `'`). Финализируется при build'е Inspector quick-bar в Phase 13.

11. **Stance hotkeys**: Z/X/C для Stand/Crouch/Prone — традиционно для FPS. В RTS могут быть зарезервированы под другое. Решим в Phase 13.

---

## Связь с остальными документами

- `GAMEDESIGN.md` — корневой топик-doc. Этот файл — углубление §3 / §4 / §5 / §6 / §8 / §9.
- `ROADMAP.md` — порядок фаз. Phase 13 / 13.6 / 14 / 14.5 / 15 / 17 / 19 / 21 ссылаются сюда за деталями.
- `PHASE-N.md` — рабочий план фазы. P-decisions фазы строятся на основе этого документа.
- `CLAUDE.md` — entry point AI-ассистентам. Технический стек.
- `.claude/old/` — закрытые фазы.
