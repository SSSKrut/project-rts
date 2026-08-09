Привет! В целом направление очень сильное: ты правильно пришёл к тому, что игра должна быть не про “кликни юнитом 20 раз”, а про **планирование намерений**: маршрут, правила поведения, реакция на контакт, оружейные override’ы, дерево приказов. Это хорошо ложится на твой `COMMAND-MODEL.md`: **Orders / Standing Rules / Per-weapon Intent** — это, на мой взгляд, правильный фундамент.

Но есть риск: если дать игроку все рычаги сразу, интерфейс станет похож на cockpit самолёта. Поэтому главный UI/UX-принцип я бы сформулировал так:

> Игрок должен управлять не каждым действием, а уровнем автономности и намерением. Детальные настройки нужны, но должны раскрываться постепенно.

Ниже — что я бы изменил, что могло быть забыто, что важно с точки зрения игрока, и в конце — черновик отдельного файла, который можно добавить в проект.

---

# 1. Главная мысль: тебе нужен не “интерфейс приказов”, а “интерфейс плана”

То, что ты описываешь с грузовиком, пехотой, высадкой, движением через лес, реакцией на контакт и разветвлением — это не просто queue приказов. Это **план операции**.

Поэтому UI должен отвечать на вопросы:

1. **Что сейчас делает отряд?**
2. **Что он сделает дальше?**
3. **Почему он остановился / отклонился от плана?**
4. **Что ему разрешено делать самому?**
5. **Где план можно быстро отредактировать?**

Сейчас у тебя уже есть хорошая основа: order chain, joint orders, barriers, tree visualization. Я бы усилил это ещё сильнее и сделал “план” центральным объектом UI.

---

# 2. Что я бы изменил в UI/UX

## 2.1. Сделать тактическую карту главным рабочим местом

3D-сцена нужна для наблюдения, красоты, локальной оценки укрытий и боя. Но сложные приказы на километровых картах игрок будет почти всегда ставить на карте.

Я бы явно принял такой принцип:

> Все длинные маршруты, цепочки приказов, высадки, патрули и координация нескольких отрядов должны быть удобнее на 2D-карте, чем в 3D.

3D — для:
- проверки видимости;
- оценки укрытий;
- точного выбора окна / здания / техники;
- наблюдения боя;
- красивой обратной связи.

Карта — для:
- маршрутов;
- order tree;
- боевой обстановки;
- зон контроля;
- событий;
- состояния отрядов;
- планирования.

Это особенно важно, если карты будут 1–5 км и больше.

---

## 2.2. Не показывать игроку “все кнопки”, пока они не нужны

У тебя много параметров:

- stance;
- pace;
- posture;
- path style;
- RoE;
- fire target types;
- behavior rules;
- auto-reposition;
- auto-stance;
- active radar;
- weapon selection;
- formation;
- facing;
- suppressive fire;
- mount/dismount;
- vehicle systems;
- repair/refuel/rearm.

Если всё это одновременно вывести в Inspector, игрок утонет.

Я бы сделал 3 уровня раскрытия:

### Уровень 1 — простые пресеты

Например:

- **Move**
- **Cautious**
- **Stealth**
- **Assault**
- **Defend**
- **Hold Fire**
- **Free Fire**
- **Return Fire**

Игроку чаще всего нужен именно этот уровень.

### Уровень 2 — быстрые панели

Если игрок хочет точнее:

- Pace: Walk / Run / Sprint
- Stance: Stand / Crouch / Prone
- Path: Direct / Road / Cover / Avoid Road
- RoE: Hold / Return / Free
- Behavior: Auto-cover / Auto-stance / Hold position

### Уровень 3 — подробный режим

Для продвинутого игрока:

- per-unit overrides;
- per-weapon targeting;
- sector fire;
- standoff distances;
- vehicle subsystems;
- radar;
- ammo rationing;
- advanced order parameters.

То есть интерфейс должен быть **progressive disclosure**: сначала намерение, потом детали.

---

## 2.3. Добавить явный “Autonomy Level”

Сейчас у тебя есть `BehaviorRules`, но с точки зрения игрока это может быть неочевидно. Я бы поверх них сделал простой UX-слой:

### Autonomy

| Режим | Смысл |
|---|---|
| **Strict** | Выполнять приказ буквально. Не менять позицию, не отходить, не стрелять без разрешения. |
| **Cautious** | Можно залечь, отвечать на огонь, искать ближайшее укрытие, но не уходить далеко. |
| **Adaptive** | Можно временно прервать приказ, занять укрытие, подавить угрозу, затем продолжить. |
| **Survival** | Самосохранение важнее приказа. Отходить, рассредотачиваться, искать cover. |

Это будет намного понятнее игроку, чем набор галочек:

- AllowAutoReposition
- AllowAutoStance
- HoldUntilOrdered
- AllowReturnFire
- SuppressionThreshold

Галочки можно оставить в advanced view, но основной интерфейс должен говорить человеческим языком.

---

## 2.4. Order tree лучше показывать не только линиями на карте, но и как timeline

Твой “git tree” — хорошая идея. Но на карте сложное дерево может быстро стать визуальным шумом.

Я бы сделал два представления:

### На карте

- waypoint markers;
- линии маршрута;
- joint/barrier node;
- branch split;
- иконки приказов.

### В Inspector / Orders Panel

Вертикальный или горизонтальный timeline:

```text
Truck-1
[Load Squad A] → [Move: road] → [Move: dirt] → [Dismount] → [Return to Base]

Squad A
[Mounted]      → [Mounted]    → [Mounted]    → [Dismount] → [Fast Move] → [Cautious Advance]
```

Или tree:

```text
Operation Plan
├─ Truck-1
│  ├─ Move to Point A
│  ├─ Move to Drop Zone
│  ├─ Dismount Squad A  🔗
│  └─ Return to Base
└─ Squad A
   ├─ Mounted in Truck-1
   ├─ Dismount  🔗
   ├─ Fast march to forest
   └─ Cautious advance through forest
```

На карте — пространственное понимание.
В панели — логическое понимание.

Игроку нужны оба.

---

## 2.5. Очень нужен Undo / Redo для приказов

Это, на мой взгляд, важная вещь, которую легко забыть.

В игре с паузой и сложными цепочками игрок будет часто ошибаться:

- поставил waypoint не туда;
- случайно заменил queue вместо append;
- задал неправильный stance;
- не туда кликнул pie menu;
- сдвинул marker.

Поэтому нужно:

- `Ctrl+Z` — отменить последнее изменение плана;
- `Ctrl+Y` — вернуть;
- возможно, история только для UI-команд, не для всей симуляции.

Это сильно снизит страх перед сложным планированием.

---

## 2.6. Нужно явно показывать “почему юнит не делает то, что я хочу”

Это критично для UX.

Игрок должен видеть причину:

- “No path”
- “Waiting for truck”
- “Waiting for squad to embark”
- “Suppressed”
- “Pinned”
- “No ammo”
- “Hold Fire prevents AttackMove”
- “Target out of LOS”
- “Weapon reloading”
- “Radio contact lost”
- “Order blocked by enemy contact”
- “Vehicle full”
- “No engineer in squad”
- “Cannot build here”

Для этого в Inspector нужен блок:

```text
Current state:
Moving to DZ

Temporary override:
Taking cover under fire

Reason:
Suppression 0.72 > threshold 0.45

Will resume:
When suppression < 0.20 and no enemy seen for 10s
```

Это суперважно. Без такого игрок будет думать, что AI тупит.

---

# 3. Что важного могло быть забыто

## 3.1. Видимость и дальность как preview до выдачи приказа

Перед тем как поставить DefendPosition или SuppressFire, игроку важно видеть:

- сектор обзора;
- сектор огня;
- дальность оружия;
- зоны, закрытые рельефом;
- потенциальные укрытия;
- danger area.

Минимально:

- при наведении ghost показывает facing;
- при выбранном оружии показывает range circle;
- при DefendPosition показывает сектор;
- при SuppressFire показывает конус.

Позже:

- projected LOS;
- вероятные линии огня из позиции;
- “эта точка плохая: нет укрытия / нет LOS”.

---

## 3.2. План должен иметь ETA

Для больших карт это очень важно.

Игрок ставит маршрут и должен видеть:

```text
Truck-1:
Base → Point A: 02:30
Point A → DZ: 01:15
Dismount: 00:20
Return: 03:10

Squad A:
Dismount → Forest: 01:40
Forest → Objective: 04:20
```

Даже если ETA приблизительный, это помогает координации.

Особенно важно для joint orders:

```text
Squad B arrives in 01:20
Tank-2 arrives in 02:10
Attack synchronized: waiting for Tank-2
```

---

## 3.3. Нужны “условия продолжения” после контакта

Ты уже описал: пехота столкнулась с врагом, остановилась, ведёт бой, потом продолжает. Но UI должен дать игроку понять, какое условие считается “всё хорошо”.

Например:

```text
Resume movement when:
[✓] No visible enemies for 15s
[✓] Suppression below 0.25
[ ] Ammo above 50%
[ ] Squad cohesion restored
```

На первом этапе можно не давать это редактировать, но логика должна быть понятна.

---

## 3.4. Нужна разница между “Stop”, “Hold”, “Cancel” и “Pause order”

Это частая UX-ловушка.

Я бы разделил:

| Команда | Смысл |
|---|---|
| **Stop** | Немедленно остановиться и очистить текущие movement actions, но не обязательно удалить весь план. |
| **Cancel Order** | Удалить текущий приказ. |
| **Cancel Chain** | Удалить текущий и все следующие. |
| **Hold Position** | Стоять здесь как новый приказ/режим. |
| **Pause Plan** | Временно заморозить выполнение очереди, но сохранить её. |
| **Resume Plan** | Продолжить сохранённый план. |

Игроку будет важно временно остановить отряд, не уничтожая сложную цепочку.

---

## 3.5. Нужен “Plan Lock” или защита от случайной перезаписи

В играх с Shift-queue легко случайно дать обычный RMB и стереть цепочку.

Можно сделать:

- если у squad есть длинный план, обычный RMB показывает small confirmation:
  - Replace plan?
  - Append?
  - Insert after current?
- или настройка:
  - RMB replaces
  - Shift+RMB appends
  - Ctrl+RMB inserts
- или “lock plan” toggle.

Не обязательно сразу, но это UX-защита.

---

## 3.6. Нужно различать “приказ” и “микро-действие”

Например, “сменить stance” может быть:

1. постоянным standing rule;
2. временным параметром конкретного order;
3. реактивным действием AI под огнём;
4. ручным мгновенным приказом.

Чтобы игрок не путался, UI должен явно показывать источник:

```text
Stance: Crouch
Source: Order override "Cautious Advance"
```

Или:

```text
Stance: Prone
Source: Tactical override: under fire
```

Это очень поможет отладке и игроку.

---

# 4. По конкретным вопросам

## 4.1. “Стоит ли делать переключатель разрешить/запретить свободное перемещение?”

Да, но я бы не называл это “свободное перемещение”. Лучше:

- **Auto-reposition**
- **Seek cover**
- **Hold exact position**
- **Weapon may relocate for LOS**

Например для снайпера:

```text
Sniper behavior:
[ ] Hold exact position
[✓] May adjust within 5m for line of sight
[ ] May relocate to better cover
[ ] May retreat if suppressed
```

Для обычного UI это можно свести к Autonomy:

- Strict: не двигаться;
- Cautious: двигаться в радиусе 3–5 м;
- Adaptive: можно искать укрытие дальше;
- Survival: можно отступать.

---

## 4.2. “Если выбрать два танка и сделать одновременные выстрелы?”

Пауза + два приказа — рабочий вариант. Но позже я бы добавил отдельное намерение:

### Salvo / Coordinated Fire

```text
Concentrate Fire:
- Tank A: main gun
- Tank B: main gun
- Fire mode: simultaneous
- Trigger: all ready / immediately / on player command
```

То есть не обязательно в MVP, но для UX это сильная фича.

В weapon-bar можно сделать:

- выбрать несколько одинаковых weapon rows;
- кликнуть цель;
- создаётся grouped AttackTarget с `OrderGroup`.

---

## 4.3. “Как удобно делать последовательные действия?”

Я бы поддержал 4 способа:

### 1. Shift+RMB

Быстро добавить waypoint/order в конец.

### 2. ПКМ по marker → Insert after / before

То, что ты уже описал. Очень нужно.

### 3. Orders Panel timeline

Выбрать order в списке и нажать:

- insert;
- delete;
- edit;
- duplicate;
- convert to joint.

### 4. Drag marker на карте

Самый визуальный способ редактировать маршрут.

Важно: редактирование должно работать на паузе идеально. В real-time можно оставить только базовые быстрые команды.

---

# 5. Если бы я был игроком — что было бы важнее всего

Мой приоритет как игрока был бы такой:

## 1. Я должен доверять отрядам

Если я дал приказ “двигаться осторожно через лес”, я не хочу микрить каждого бойца. Они должны:

- не идти строем под пулемёт;
- залегать под огнём;
- искать укрытие;
- отвечать на контакт, если разрешено;
- не забывать основной приказ.

## 2. Я должен понимать, что происходит

Если отряд остановился — я хочу сразу видеть почему.

## 3. Я должен быстро редактировать план

Перетащить waypoint, вставить приказ, удалить ветку, изменить stance/pace конкретного участка.

## 4. Пресеты важнее ручных галочек

Мне чаще нужны “Stealth”, “Assault”, “Defend”, “Road march”, чем ручная комбинация из 10 параметров.

## 5. Карта должна быть мощной

Большую часть времени я бы играл через карту, особенно при нескольких отрядах и технике.

## 6. Персональное управление оружием — круто, но не должно быть обязательным

Я хочу иметь возможность кликнуть “снайпером сюда” или “танком HE туда”, но обычный бой должен работать и без этого.

---

# 6. Что я бы добавил в roadmap раньше

С учётом текущего состояния проекта я бы подумал о переносе некоторых UI-фич раньше.

## Очень желательно раньше Phase 21

### 1. Event log + pings

Даже простой вариант:

```text
[12:04] Alpha: enemy contact
[12:05] Bravo: suppressed
[12:07] Truck-1: arrived
```

Это нужно уже для Tactical AI и Combat.

### 2. Reason/status line

В Inspector:

```text
State: Moving
Override: Taking cover
Reason: Suppressed
```

### 3. AttackMove + HoldFire conflict warning

Ты уже отметил TODO. Это важно:

```text
AttackMove issued, but RoE = Hold Fire
Unit will move without engaging
```

### 4. Basic order editing

Хотя бы delete/drag waypoint.

---

# 7. Черновик нового файла

Ниже предлагаю файл, например:

```text
.claude/UI-UX-COMMAND-NOTES.md
```

---

```md
# UI/UX Command Notes

Рабочий документ по интерфейсу управления, планированию приказов и снижению когнитивной нагрузки. Дополняет `GAMEDESIGN.md` §4/§9 и `COMMAND-MODEL.md`.

---

## 1. Главный UX-принцип

Игрок управляет **намерением и уровнем автономности**, а не каждым микро-действием.

Игра допускает глубокий контроль, но базовый путь должен быть простым:

1. Выбрать отряд.
2. Выбрать намерение: Move / Cautious / Assault / Defend / Stealth.
3. Указать точку / цель / сектор.
4. Наблюдать выполнение.
5. Реагировать на события через pause, pings, event log и редактирование плана.

Подробные настройки доступны через progressive disclosure, но не обязательны для базового геймплея.

---

## 2. Три слоя управления

Сохраняется модель из `COMMAND-MODEL.md`:

### 2.1. Orders

Дискретные приказы с lifecycle:

- MoveTo
- AttackMove
- DefendPosition
- Garrison
- OccupyTrench
- SuppressFire
- Mount / Dismount
- Build
- Patrol

UI surface:

- RMB tap
- RMB hold pie menu
- RMB drag vector order
- Shift+RMB append
- Marker context menu
- Orders timeline panel

### 2.2. Standing Rules

Постоянные настройки:

- MovementProfile
- EngagementRules
- BehaviorRules
- VehicleSystems

UI surface:

- Inspector quick-bars
- presets
- advanced toggles

### 2.3. Per-weapon Intent

Опциональное целеуказание конкретному оружию:

- weapon row click
- aim cursor
- target click
- creates AttackTarget / SuppressFire with weapon preference

Не должно быть обязательным для обычного боя.

---

## 3. Progressive Disclosure

Интерфейс делится на три уровня сложности.

### Level 1 — Intent Presets

Кнопки/чипы:

- Default
- Road March
- Cautious
- Stealth
- Assault
- Defend
- Hold Fire
- Return Fire
- Free Fire

Игрок управляет “человеческими словами”, не внутренними флагами.

### Level 2 — Quick Controls

Раскрываемые быстрые настройки:

- Pace: Walk / Run / Sprint
- Stance: Stand / Crouch / Prone
- Posture: Standard / Quiet
- PathStyle: Direct / RoadPrefer / RoadAvoid / CoverSeek
- RoE: Hold / Return / Free
- Target filters: Inf / Armor / Air / Structures
- Behavior: Auto-cover / Auto-stance / Return fire / Hold exact

### Level 3 — Advanced Controls

Редко используемые настройки:

- per-unit overrides
- per-weapon orders
- sector restrictions
- standoff distances
- radar/smoke/engine toggles
- ammo rationing
- vehicle subsystem states
- conditional order parameters

---

## 4. Autonomy Level

Поверх `BehaviorRules` вводится простой UX-слой:

| Autonomy | Meaning |
|---|---|
| Strict | Выполнять приказ буквально. Не менять позицию. Минимальная реакция. |
| Cautious | Можно залечь, отвечать на огонь, искать ближайшее укрытие в малом радиусе. |
| Adaptive | Можно временно прервать приказ, занять укрытие, подавить угрозу, затем продолжить. |
| Survival | Самосохранение важнее приказа. Разрешён отход и сильное рассредоточение. |

Пример mapping:

- Strict:
  - HoldUntilOrdered = true
  - AllowAutoReposition = false
  - AllowAutoStance = false
  - AllowReturnFire = false

- Cautious:
  - HoldUntilOrdered = false
  - AllowAutoReposition = true, limited radius
  - AllowAutoStance = true
  - AllowReturnFire = true

- Adaptive:
  - AllowAutoReposition = true
  - AllowAutoStance = true
  - AllowReturnFire = true
  - SuppressionThreshold = medium

- Survival:
  - AllowAutoReposition = true
  - AllowAutoStance = true
  - AllowReturnFire = true
  - SuppressionThreshold = low
  - Retreat/Scatter allowed

Advanced UI может показывать реальные поля `BehaviorRules`.

---

## 5. Plan UI

Сложные цепочки приказов воспринимаются как **план операции**.

План должен отображаться двумя способами:

### 5.1. Spatial View on Map

На карте:

- route lines
- waypoint markers
- order icons
- joint/barrier nodes
- branch lines
- ETA labels optionally
- current active order highlight
- blocked/failed markers

### 5.2. Logical View in Orders Panel

В Inspector или отдельной панели:

```text
Truck-1
[Load Squad A] → [Move: road] → [Move: dirt] → [Dismount] → [Return to Base]

Squad A
[Mounted]      → [Mounted]    → [Mounted]    → [Dismount] → [Fast Move] → [Cautious Advance]
```

Для joint orders общий barrier node помечается link-иконкой.

---

## 6. Order Editing

Минимально необходимые операции:

- append order
- insert before
- insert after
- delete order
- delete chain from here
- drag waypoint
- edit order parameters
- convert to joint order
- pause/resume plan

Hotkeys / gestures:

| Input | Action |
|---|---|
| Shift+RMB | Append |
| RMB on marker | Context menu |
| Drag marker | Relocate waypoint |
| Shift+click marker | Cancel this order |
| Ctrl+click marker | Cancel this and following |
| Ctrl+Z | Undo last plan edit |
| Ctrl+Y | Redo |

Undo/Redo applies to player-authored plan edits, not necessarily full simulation state.

---

## 7. Status and Reason Feedback

Every selected squad/unit should expose:

```text
Current order:
MoveTo Forest Edge

Current state:
Moving

Temporary override:
Taking cover

Reason:
Suppression 0.72 > threshold 0.45

Resume condition:
No visible enemy for 10s and suppression < 0.20
```

Common reasons:

- No path
- Waiting for transport
- Waiting for passengers
- Suppressed
- Pinned
- Under fire
- No ammo
- Reloading
- Target out of LOS
- Target out of range
- Hold Fire prevents engagement
- Vehicle full
- No engineer available
- Radio link lost
- Order blocked
- Order failed

This is critical for player trust.

---

## 8. Preview Requirements

Before committing orders, UI should preview:

### MoveTo

- formation ghost
- path route
- ETA
- path style hint: road/direct/cover

### DefendPosition

- formation ghost
- facing arrow
- sector arc
- likely cover slots

### SuppressFire

- target point/sector
- weapon range
- ammo estimate if available
- danger/friendly fire warning if possible

### Garrison

- window slots
- assigned unit ghosts
- visible arcs from selected windows

### OccupyTrench

- positions along trench
- facing direction
- cover direction if known

### Weapon Targeting

- selected weapon range
- ready/reload status
- ammo count
- expected target validity: in range / out of range / no LOS

---

## 9. Notifications

Default: no forced auto-pause.

Instead:

- map pings
- event log
- optional sound
- optional auto-pause matrix

Important event types:

- first enemy contact
- friendly hit
- KIA
- suppression/pinned
- order completed
- order failed
- vehicle arrived
- mount/dismount completed
- ammo low
- radio lost
- no path
- building entered/cleared

Event log row click should focus camera/map on event position.

---

## 10. Player Priorities

From player perspective, most important:

1. Trust autonomous behavior.
2. Understand why units deviate from plan.
3. Quickly edit plans.
4. Use presets instead of many toggles.
5. Operate mostly from tactical map.
6. Have optional deep control for weapons and vehicles.
7. Avoid accidental plan destruction.
8. Receive clear warnings about contradictions.

Examples of contradictions:

- AttackMove + HoldFire
- SuppressFire with no ammo
- Sprint + Prone if disallowed by implementation
- Mount order but vehicle has no seats
- Build order but no engineer
- Garrison but no accessible entry
- Weapon target outside range
- Active radar enabled while stealth preset active

---

## 11. Suggested Early UX Features

These are worth implementing earlier than full UI expansion if possible:

1. Basic event log.
2. Map pings.
3. Selected unit/squad reason line.
4. AttackMove + HoldFire warning.
5. Basic waypoint drag/edit.
6. Basic order delete.
7. ETA labels for long movement.
8. Weapon range preview.
9. Undo for plan edits.

---

## 12. Design Risks

### Risk 1: Too many buttons

Mitigation:
- presets first
- advanced settings collapsed
- role/default doctrine handles most cases

### Risk 2: Player does not understand AI autonomy

Mitigation:
- Autonomy Level
- reason feedback
- visible TacticalOverride status

### Risk 3: Map becomes unreadable

Mitigation:
- filter layers
- selected-only order tree
- fade non-selected plans
- collapse branches
- show detailed tree in panel, not only on map

### Risk 4: Per-weapon control becomes mandatory micro

Mitigation:
- good default WeaponSystem target selection
- weapon-bar as override only
- group/salvo orders for repeated tasks

### Risk 5: Accidental order replacement

Mitigation:
- Shift append convention
- replace confirmation for long plans
- undo/redo
- plan lock optional

---

## 13. Open Questions

1. Should `Autonomy Level` be stored as its own component or only as UI preset over `BehaviorRules`?
2. Should ETA be computed from current route only or include stance/terrain/stamina?
3. How much conditional logic should orders expose to player?
4. Should “Pause Plan” exist as separate command from Stop?
5. Should weapon-bar support salvo/group selection in Phase 14.5 or later?
6. Should event log be implemented before Phase 21 because combat already needs it?
7. How to visualize plan branches without clutter on small map zoom?
8. Should orders have names/labels set by player for complex operations?
```

---

# 8. Короткий итог

Я бы сфокусировался на таком UX:

1. **Карта — главный инструмент планирования.**
2. **Inspector — объясняет выбранное.**
3. **Orders panel — показывает логику плана.**
4. **Event log + pings — сообщают о важном.**
5. **Пресеты — основной способ управления.**
6. **Детальные галочки — advanced режим.**
7. **Autonomy Level — человеческий слой поверх BehaviorRules.**
8. **Reason feedback — обязательно, иначе игрок не будет доверять AI.**
9. **Undo/Redo и безопасное редактирование — очень желательно.**
10. **Per-weapon control — мощный override, но не основной способ играть.**

Если совсем коротко: твоя модель команд уже правильная, но следующий большой UX-шаг — сделать так, чтобы игрок видел не “очередь команд”, а **понятный боевой план с причинами, статусами и безопасным редактированием**.