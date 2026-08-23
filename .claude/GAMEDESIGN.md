# Game Design

Корневой документ проекта: что мы строим, какими принципами это формируется, и какие архитектурные развилки уже залочены. Любая новая фаза начинается с прочтения этого документа целиком — без него непонятно зачем мы делаем то, что делаем.

`ROADMAP.md` — когда и в каком порядке. `COMMAND-MODEL.md` — углубление §3/§4/§5/§6/§8/§9 этого документа: подробная модель команд (Orders / Standing rules / Per-weapon intent), order graph + barriers, weapon-bar UX, ghost-preview, реактивное поведение, notification system. `PHASE-N.md` — рабочий план активной фазы. `.claude/old/` — архивы и черновики (включая `DESIGN-pre-vision.md` — предыдущий topic-doc, актуальные тезисы которого перенесены сюда). `CLAUDE.md` — entry point для AI-ассистентов, описывает технический стек.

---

## §1. Жанровое позиционирование

**Что мы строим.** Тактический реал-таймный симулятор «холодной войны» на уровне отделения / взвода. Игрок управляет 3-10 отрядами пехоты + поддерживающей техникой на участке местности 1-5 км. Глубина — между Combat Mission и Wargame: Red Dragon, с уклоном в реализм Steel Beasts по детальности управления юнитом. Базовая референс-сетка:

- **Combat Mission Cold War / Black Sea** — масштаб сражения, детальность местности (здания с интерьерами, окопы, поля), глубина моделирования боя (баллистика, мораль, видимость).
- **Steel Beasts Pro** — глубина управления техникой (системы наблюдения, оружие, движение по дорогам / off-road), эталон детализации.
- **Sea Power: Naval Combat in the Missile Age** — эталон **управления через карту**, RoE-матрицы (Hold Fire / Free Fire по типам целей), settings-driven поведение юнитов.
- **Wargame Red Dragon / Steel Division II** — vector orders, многоотрядное управление, RoE.
- **Men of War / Call to Arms** — Direct Control, per-юнит inventory, lootable equipment.
- **Squad / Foxhole** — RoE, постройка фортификации игроками, координация ролей.

**Что мы НЕ строим (и сознательно отбрасываем).**

- Не аркадный RTS (нет 200-юнитного микро-spam'а в стиле StarCraft).
- Нет ресурсной экономики, base-building, tech-tree (это другой жанр; местность даётся фиксированной, отряды — pre-deployed).
- Нет heroes / unique characters (юниты — взаимозаменяемая пехота с ролями).
- Нет realtime-multiplayer как первичной цели (single-player + AI opponent; multiplayer когда-нибудь как опция).
- Нет cinematic-камеры / выраженной story (sandbox-режим, миссии — позже).

**Темп.** Real-time continuous с **pause** и **speed compression** (1×/2×/4×/8×). Игрок проводит большую часть времени в планировании (поставил приказы, нажал play, наблюдает выполнение, паузит при контакте, выдаёт реакцию). Это **не click-spam RTS**.

**Платформа.** Solo dev, Go + Ark ECS + raylib-go, целевой релиз — Linux primary, Windows secondary. Mac best-effort.

**Архитектурные принципы.**

1. **ECS-first.** Все геймплейные сущности — ECS entity'и с component'ами; никаких god-классов, никаких arrays внутри component'ов (см. §2 unit composition).
2. **Маркер-компоненты как state.** Processed-флаги, override-флаги, role-маркеры — отдельные пустые маркер-компоненты, переключение архетипа = переключение поведения. Cache-friendly итерация по фильтрам. (Заметка: бывшие LODActive/LODRelevant/LODDormant теперь применяются только для render/streaming, симуляция их не читает — см. принцип 6.)
3. **Service objects для on-demand operations** (Stamper / NavService / SquadService). Не System'ы — вызываются из input handlers и других систем после закрытия Filter Query.
4. **Singleton resources через `ecs.Resource[T]`** для глобального состояния (RoadGraph, TerrainChunkIndex, BuildingChildIndex, TransitionRegistry, etc.).
5. **Hybrid render.** 3D-сцена через `rl.RenderTexture2D`, все 2D-панели — direct overlay в clipped screen-rect. Карта — 2D-абстракция, не top-down 3D-render. Все raylib-вызовы (DrawMesh, LoadTexture, UploadMesh) — **строго main thread**, никаких background-горутин в OpenGL контекст.
6. **Универсальная симуляция, без LOD-gating.** Тактический симулятор требует persistent симуляции — отряд на другой стороне карты продолжает выполнять приказы, бой идёт даже если игрок смотрит на другой сектор. Системы движения / vision / AI обрабатывают **все** entity, без branching по LODActive/LODRelevant/LODDormant. LOD-tier маркеры остаются на entity для будущего render-LOD и streaming (Phase 24+), но **симуляция их игнорирует**. См. cross-cuts ниже про time-slicing.
7. **Time-slicing для дорогих систем через hash-bucket distribution.** Дорогие per-entity проверки (vision raycast, tactical AI utility, MacroPath replan) не выполняются на каждом entity каждый кадр — entity распределяются по bucket'ам через `entity.ID % BucketCount`, в текущий тик обрабатывается один bucket. Цикл = `BucketCount × tickInterval` секунд. Per-system bucket counts: vision @ 1 сек, AI @ 5-10 сек, macro replan @ 3-5 сек. Дешёвые системы (движение, проверка ActionQueue) работают **каждый кадр на всех** без time-slicing'а.
8. **Worker pool concurrency.** Дорогие системы параллелятся через persistent worker pool (`runtime.NumCPU()` default, CLI flag `-workers=N` override). Паттерн **fork-join per system**: serial pre-pass (snapshot mutable inputs) → parallel main (read-only / write-own-only) → serial post-pass (apply archetype mutations). Archetype-mutirующие операции (Add/Remove component) собираются в local buffer per worker, мержатся в serial post-pass'е (как `leaveBuffer` в FormationSystem). Конкурентный read из Ark `ecs.Map[T].Get` safe; archetype changes — strictly serial. Determinism — не требуется (single-player, no replay/MP).
9. **Координаты через `WorldPos{Chunk, Local}`.** Никакого глобального float32; precision не теряется на больших дистанциях. Render приводит к view-space относительно `CurrentOriginChunk`.
10. **Real terrain-ready.** Procgen — placeholder; формат данных (heightmap-чанки + сериализуемые resource'ы для дорог / рек / зданий) спроектирован так, чтобы заменить процедурную генерацию на реальные данные (OSM + DEM + Landsat) без правки геймплея.

---

## §2. Юнит и его роли

**Юнит = композиция компонентов**, не один монолитный struct. Это уже залочено в Phase 7 и продолжает быть основой:

- **Spatial**: `WorldPos`, `Collider`, `Stance` (Stand/Crouch/Prone), `Motion` (Yaw, Speed).
- **Senso-Cognitive**: `Vision` (Range, AngleDot), `Suppression` (Level, ThreatDir), `Awareness` (LastSeen FIFO).
- **Execution**: `ActionQueue` (микро-приказы), `LocalBlackboard` (per-unit AI state — currently empty placeholder, наполнится в Phase Tactical AI).
- **Equipment**: `Equipment{Primary, Secondary, Active ecs.Entity}` — references на отдельные ECS-сущности оружия / гранат / рации. Никаких arrays внутри component'а.

**Роли (`UnitRole`).** Каждый юнит имеет роль, заданную при спавне. Роль определяет дефолтное `Equipment`, поведение в AI (Phase Tactical AI), визуальную отметку в 3D и на карте.

Базовый набор (Phase Roles):

| Роль | Назначение | Equipment default | Особенности |
|---|---|---|---|
| `Leader` (Командир) | Slot 0 ростера, инициатива решений | AK + бинокль | Бонус cohesion-радиуса, видимый «командирский» маркер |
| `Rifleman` (Стрелок) | Базовая пехота | AK | Дефолт; роль для большинства слотов |
| `MachineGunner` (Пулемётчик) | Source of Suppression | PKM | Медленный, требует deployment-time для огня |
| `Grenadier` (Гранатомётчик) | AT/AP гранаты, подствольник | AK + RPG-7 или GP-25 | Может вести «зональный огонь» по зданию |
| `Sniper` (Снайпер) | Дальний точный огонь | SVD + оптика | Низкая скорострельность, высокая точность, далёкий Vision |
| `ATGunner` (Расчёт ПТ) | Анти-танк | RPG / АГС | Парная роль (наводчик + помощник в идеале) |
| `Medic` (Медик) | Лечение / эвакуация | АКС + сумка | Реакция на «ранен» событие, идёт к раненому |
| `RadioOperator` (Радист) | LW radio к штабу | АКС + Р-159 | Без него Squad теряет `RadioNetwork.HQReachable` |
| `Engineer` (Сапёр) | Постройка фортификации, разминирование | АКС + лопата | Исполняет Build-приказы (см. §7) |
| `DemoMan` (Подрывник) | C4, мины | АКС + заряды | Сильно пересекается с Engineer; возможно объединим |

**Слот в ростере = слот в формации**. `CommandRoster.Members[i]` определяет позицию в формационном offset'е (slot 0 = центр-командир). Роль слота — отдельное поле, **не связанное** с slot index. То есть пулемётчик может быть в slot 3 ростера.

**Шаблоны отрядов** (`SquadTemplate`). При создании отряда (Phase Roles, hot-key или UI) игрок выбирает template, ECS-сущность Squad'а получает соответствующее распределение ролей. Базовые templates:

- **Motor Rifle Squad (Motostrelki, СССР)** — 8 чел: Leader + 4 Rifleman + MachineGunner + Grenadier (RPG) + Medic.
- **Infantry Squad (NATO base)** — 9-10 чел, ужмём до 8: Leader + 4 Rifleman + MachineGunner + Grenadier + Medic.
- **Recon Squad** — 4-6 чел: Leader + Sniper + 2-3 Rifleman + RadioOperator.
- **Engineering Squad** — 4-6 чел: Leader + 2 Engineer + DemoMan + 2 Rifleman support.
- **Anti-Tank Team** — 2-4 чел: ATGunner + Loader + 1-2 Rifleman security.

**Equipment как ECS-сущности**. Каждая единица снаряжения — отдельная entity с `OwnedBy{Owner}` (на юните-владельце) + `WorldPos` (синхронно с юнитом для рендера). Это позволяет:

- Уронить оружие при смерти юнита → entity остаётся в мире с `OwnedBy{0}`, любой другой юнит может подобрать.
- Magazines / гранаты как inventory — добавится в Phase Combat.
- Замена placeholder-меша на загружаемую модель не затрагивает геймплей.

**Что НЕ делаем в роле-фазе:**

- Per-роль AI behaviors (Medic ходит к раненому, MG выбирает позицию для огня) — Phase Tactical AI.
- Stamina / fatigue — Phase Movement&Engagement.
- Lootable inventory — Phase Combat.
- Visual differentiation через анимации / уникальные модели — Phase Polish; placeholder = разноцветные «caps» на cube-юнитах + role-icon в HUD.

---

## §3. Приказы: таксономия и lifecycle

**Order — отдельная ECS-сущность**, не поле Squad'а. Решение залочено. Обоснование:

> **Углубление модели команд — `COMMAND-MODEL.md`.** Этот раздел описывает Order taxonomy и lifecycle. Полная модель команд (три слоя: Orders / Standing rules / Per-weapon intent), order graph + barriers (joint orders, Embark/Dismount synchronization), tree visualization при multi-select, реактивное поведение через TacticalOverride — в `COMMAND-MODEL.md`.


- Сложные приказы (Garrison, Build) несут sub-state — какой юнит у какого окна, прогресс постройки. Хранить это в Squad'е загромождает архетип.
- Цепочки приказов с условиями (`do A, then B if condition`) естественно строятся как linked-list entity'ей.
- Replan / abort / replay приказа = манипуляция с entity ID, понятная семантика.
- Множественные владельцы одного приказа (joint order для multi-squad) — естественны через cross-references.

**Структура.**

```go
// Базовые компоненты на Order-entity:
type Order struct{}                                  // маркер
type OrderKind struct{ Code OrderKindCode }          // MoveTo / Garrison / Defend / ...
type OrderState struct{ Code OrderStateCode }        // Issued/InProgress/Blocked/Completed/Cancelled/Failed
type OrderOwner struct{ Squad ecs.Entity }           // владелец-отряд
type OrderTarget struct {                            // куда указывает
    Pos    WorldPos       // координата (если есть)
    Entity ecs.Entity     // целевая сущность (если есть): здание, окоп, отряд, машина
}
type OrderChain struct{ Next ecs.Entity }            // следующий приказ в цепочке (0 = конец)
type OrderIssued struct{ Time float32 }              // session-time, когда выдан
type OrderProgress struct{ Value float32 }           // 0..1, для long-running (Build, Garrison settle)

// Per-kind дополнительные параметры (опциональные компоненты):
type OrderParamFacing struct{ YawRad float32 }       // для DefendPosition
type OrderParamSuppress struct {                     // для SuppressFire
    SubKind SuppressSubKind  // Entity / Sector / Building / Window
    Sector  Sector           // конус, если SubKind=Sector
}
type OrderParamBuild struct{ Kind FortificationKind } // Trench/Sandbag/Mines
type OrderParamPatrol struct{ Loop bool }            // циклически или одноразово
```

Squad держит **очередь приказов** через указатель на head:

```go
// На Squad-entity:
type OrderQueueHead struct{ First ecs.Entity }       // first-pending order; nil = idle
```

Очередь = linked list через `OrderChain.Next`. Head pop'ается когда состояние `Completed/Cancelled/Failed`.

**Lifecycle states:**

- `Issued` — приказ создан, ещё не подобран SquadOrderSystem'ом.
- `InProgress` — текущее активное состояние, AI выполняет.
- `Blocked` — выполнение приостановлено (нет пути / нет цели / другой override). Будет авто-retry или ждёт ручного вмешательства.
- `Completed` — успешно выполнен. Head очереди продвигается на `Chain.Next`.
- `Cancelled` — отменён игроком / другим приказом.
- `Failed` — провал по тактическим причинам (юниты погибли / не нашли target / timeout).

**Базовая таксономия Order Kinds (Phase Orders):**

| Kind | Semantics | Target | Параметры | Lifecycle |
|---|---|---|---|---|
| `MoveTo` | Идти в точку, не вступая в бой по дороге | `Pos` | — | Complete при arrival |
| `AttackMove` | Идти в точку, engaging встреченных врагов | `Pos` | — | Complete при arrival |
| `Garrison` | Занять здание, распределиться по окнам | `Entity`=Building | — | Complete при размещении ростера; в InProgress пока внутри |
| `OccupyTrench` | Занять окоп, распределиться по длине | `Entity`=Trench | — | Complete при размещении |
| `DefendPosition` | Стоять в точке лицом в направлении | `Pos` | `Facing` | Никогда не Complete сам; cancel only |
| `Patrol` | Цикл по waypoint'ам | `Pos` (плюс цепочка через `Chain.Next`) | `Loop` | Complete если `!Loop` и прошёл все |
| ~~`Sneak`~~ | Удалён как Order kind после Phase 13 — стал preset для `MovementProfile` (Walk + Crouch + Quiet + RoadAvoid + RoE=HoldFire), применяется через `OrderParamMovementProfile` override на обычный `MoveTo` (hotkey `Ctrl+RMB`). См. `COMMAND-MODEL.md` §4. | — | — | — |
| `SuppressFire` | Подавлять цель / сектор / здание | `Entity` или `Pos` | `SubKind, Sector` | Никогда не Complete сам; cancel by ammo / timer |
| `Retreat` | Отступать в направлении, по дороге | `Pos` | — | Complete при arrival |
| `Mount` | Сесть в технику | `Entity`=Vehicle | — | Complete при посадке |
| `Disembark` | Высадиться из техники | — | — | Complete при высадке |
| `Build` | Возвести фортификацию | `Pos` | `Kind` | Complete при завершении постройки |

Не в MVP, но запланировано: `Follow` (за отрядом), `Escort` (сопровождать машину), `Heal` (медик к раненому), `Repair` (инженер к машине), `LayMines`, `Demolish`, `ClearBuilding` (room-by-room sweep).

**Hit-test resolver — превращение клика в приказ.** ПКМ-tap всегда даёт *один* приказ, но какой — зависит от того, куда попал курсор. Resolver проверяет в порядке приоритета:

1. Курсор на сущности → разрешитель смотрит kind:
   - Building → `Garrison` (или контекстно: если враждебный → `ClearBuilding`)
   - Trench → `OccupyTrench`
   - Squad (свой) → `Follow`
   - Squad (враг) → `AttackMove` к его позиции (или `SuppressFire` если RoE=FreeFire)
   - Vehicle (свой) → `Mount`
2. Курсор на координате внутри footprint полилинии окопа → `OccupyTrench`.
3. Иначе → `MoveTo` (или `AttackMove` если зажат Alt, или `Sneak` если зажат Ctrl).

ПКМ-hold (>200ms) открывает **pie menu** с полным списком приказов независимо от контекста; игрок выбирает явно.

**Shift+ПКМ** аппендит приказ в очередь (chain через `OrderChain.Next`). Это даёт цепочки: «сначала MoveTo A, потом Garrison Building B, потом DefendPosition».

**Условные триггеры** (вроде «on_contact → switch to AttackMove») — не в Phase Orders, это Phase Tactical AI. Архитектурный задел: на Order можно навешать `OrderTrigger{OnEvent, ThenOrder}` компоненты, которые TactSurvSystem читает.

**Что НЕ делаем в Phase Orders:**

- Реальный AI per-kind (как Garrison распределяет по окнам, как Build управляет временем) — будет реализован в соответствующих фазах. Phase Orders пишет только caркас и одну-две демо-реализации (MoveTo + DefendPosition + Garrison базовая).
- Multi-squad joint orders — Phase Multi-squad coordination.
- Action masking при потере радиосвязи — Phase Comms.

---

## §4. Грамматика взаимодействия

Идея: **игрок выражает намерение, не последовательность кликов**. «Один клик = одно намерение». Если намерение сложное (контекст-зависимое или с параметрами) — есть phyzically separate UI mode (pie menu, drag, hotkey-modifier).

**Selection.**

| Действие | Эффект |
|---|---|
| `LMB-tap` на сущности | Заменить selection на эту сущность (или её отряд, если юнит) |
| `LMB-tap` в пустоту | Очистить selection |
| `LMB-drag` (marquee) | Выделить всё в прямоугольнике |
| `Shift+LMB-tap` | Toggle сущности (или отряда) в selection |
| `Shift+LMB-drag` | Union marquee с текущим selection |
| `Ctrl+LMB-tap` | Subtract из selection |
| `Ctrl+1..5` | Bind текущий selection в hotkey slot |
| `1..5` | Recall hotkey slot |

**Selection — *глобальное* состояние**, одно на сессию, sync'ится между всеми view'ми. Кликнул юнита на карте → подсвечен и в 3D. Кликнул отряд в 3D → подсвечены маркеры в карте и его состав в Inspector'е.

**Hover — также глобальное** состояние. Наведение на сущность подсвечивает её во всех панелях. Полезно при координации между картой и 3D.

**Order issuance (ПКМ).**

| Действие | Эффект |
|---|---|
| `RMB-tap` на цели | Smart default (по hit-test resolver §3) |
| `RMB-hold` (>200ms) | Pie menu с полным списком приказов |
| `RMB-drag` | Vector order: направление + ширина (для DefendPosition facing'а или multi-squad spread'а) |
| `Shift+RMB-tap` | Append приказ в очередь |
| `Alt+RMB-tap` | Принудительный AttackMove (даже на пустую точку) |
| `Ctrl+RMB-tap` | Принудительный Sneak |
| `Double-RMB` | Sprint to (MoveTo + Pace=Sprint) |
| `Triple-RMB` | Attack-move (равно `Alt+RMB`) |

**Принципиально**: ПКМ-tap всегда даёт один приказ. Если нужны параметры — hold даёт pie menu, drag даёт вектор. Никаких «обязательных меню после каждого клика».

**Идентичность 3D и карты.** Любой клик доступен в обеих view'ах. Resolver — общий код, разница только в проекции (perspective vs orthographic 2D mapping). Это **архитектурное требование**: order issuance не должен знать, из какой view пришёл клик.

**Hotkeys для глобальных действий.** Не привязаны к view, работают всегда:

| Hotkey | Действие |
|---|---|
| `Space` | Pause / Resume |
| `+` / `-` | Speed up / down (1 / 2 / 4 / 8×) |
| `Tab` | Toggle UI preset Field ↔ Command |
| `H` | Stop order для выделенного |
| `T` | Form squad (если ≥2 выделено) |
| `U` | Ungroup (leave squad для каждого выделенного) |
| `F1..F4` | Formation kind (Line / Column / Wedge / Loose) |
| `Q/W/E/R` или подобные | RoE quick-toggle (отдельно решим в Phase RoE) |
| `K` (hold) | Squad linkage overlay |
| `N/C/V/F/Y/G` (hold) | Existing debug overlays |
| `P` / `Ctrl+P` | Profiler HUD toggle / snapshot |

**Pie menu** появляется как полупрозрачное колесо вокруг курсора при hold. Сегменты — типы приказов из §3. Выбор: курсор уходит в сторону нужного сегмента, отпускание ПКМ = выбор. Cancel = курсор в центр + отпустить. Иконки сегментов — пиктограмма kind'а. Активные/неактивные сегменты подсветка'ются: если выделенный отряд не имеет сапёра, сегмент Build серый.

**Vector orders.** `RMB-drag` интерпретируется по контексту:

- Selection = один Squad, target = пустая точка → стандартная `MoveTo`, но направление drag'а определяет **facing** при прибытии (полезно для approach mode).
- Selection = один Squad, target = здание → drag-вектор определяет с какой стороны подходить (полезно для штурма дверью с фланга).
- Selection = ≥2 Squad'а → drag-вектор определяет ширину фронта; auto-distribute (Phase Multi-squad).
- Selection = Squad с doctrine Defense → drag-вектор определяет основной сектор обороны.

**Build mode** (Phase Engineering). Игрок выбирает иконку фортификации (Trench / Sandbag / Mines) на toolbar → курсор переходит в build-режим (cursor показывает призрачный outline) → LMB-click ставит план-маркер → ECS-сущность плана появляется → ближайший доступный Engineer-юнит исполняет `Build` приказ автоматически (или вручную через Order issuance).

**Что НЕ делаем в Phase UI:**

- Перетаскивание бойцов между отрядами через drag-and-drop в Inspector'е — Phase UI expansion.
- Direct Control mode (взять одного бойца за плечо, как в Men of War) — отдельная фаза polish, не в MVP.
- Macro-recording (записать последовательность приказов и replay'нуть) — никогда (не вписывается в жанр).
- Voice commands / chat — никогда (single-player проект).

---

## §5. Профиль движения

Движение юнита — **4 независимых рычага**, каждый может быть переключён независимо. Это даёт 4D-куб возможных стилей передвижения; на старте мы не запрещаем «нереалистичные» комбинации структурно (sprint+prone). UI может скрывать невозможные комбо.

> **MovementProfile — это standing rule на Squad / Unit, не Order kind.** `Sneak` (раньше Order kind в §3) после Phase 13 — это preset для `MovementProfile` (Walk + Crouch + Quiet + RoadAvoid), применяемый через `OrderParamMovementProfile` override на обычный `MoveTo`. Полная модель — `COMMAND-MODEL.md` §4 (standing rules taxonomy) + §6 (UX hotkeys: `Ctrl+RMB` = Sneak preset).


**`Pace` — темп.**

| Pace | Speed mul | Stamina cost | Detection | Tactical use |
|---|---|---|---|---|
| `Walk` | 1.0 | 0 | High audio sig | По умолчанию, длительные переходы |
| `Run` | 1.6 | 0.1/s | Higher audio | Маневрирование под огнём |
| `Sprint` | 2.2 | 0.5/s, depletes fast | Loud | Перебежки от cover к cover |

После того как `Stamina.Current` достигла 0 → автоматическое падение в Walk, до полного восстановления. **Восстановление** идёт когда `Pace == Walk` и `Stance != Prone` (или другая логика — в фазе уточним).

**`Stance` — стойка.** Уже есть в Phase 7: `Stand` / `Crouch` / `Prone`. Влияет на:

- Speed multiplier (стандартный 1.0 / 0.6 / 0.3 — уже в UnitMovementSystem).
- Силуэт / коллайдер высота (Phase Combat — что попадает).
- Detection (Crouch / Prone снижают auditory + visual sig).
- Accuracy при стрельбе (стоя меньше точности; Phase Combat).

**`Posture` — шумность.**

| Posture | Audio sig | Visual sig | Behavior |
|---|---|---|---|
| `Standard` | Нормальный | Полный | По умолчанию |
| `Quiet` | -60% | -20% | "Stealth movement" — связан с `Sneak` order; auto-clears на контакте |

`Posture = Quiet` обычно идёт в паре с `Stance = Crouch/Prone` и `Pace = Walk`, но структурно независимы.

**`PathStyle` — стиль маршрута.** Это параметр **NavService.FindPath**, не post-processing. NavGrid cell costs уже умеют road-bias (Phase 6); добавляем модификаторы:

| PathStyle | Behavior | Implementation |
|---|---|---|
| `Direct` | Кратчайший путь | Default cell costs |
| `RoadPrefer` | Сильное предпочтение дорог | Road cells × 0.5 cost; off-road × 1.5 |
| `RoadAvoid` | Избегать дорог и открытой местности | Road cells × 2.0; open ground × 1.3; cover-tagged cells × 0.8 |
| `CoverSeek` | Через cover slots, даже с обходом | Cells near cover slots × 0.7 |

PathStyle хранится на Squad'е или Unit'е (squad default + per-unit override). Vehicle имеет свой PathStyle (типично `RoadPrefer`). Пехота на технике (`Mounted`) использует Vehicle's PathStyle, dismounted — свой.

**Stamina** — новый компонент:

```go
type Stamina struct {
    Current     float32   // 0..MaxLevel
    MaxLevel    float32   // зависит от Stance + Equipment weight
    RecoverRate float32   // per-second when Walk + Stand
}
```

`MaxLevel` зависит от снаряжения: тяжёлый MG/AT снижает максимум. Per-role default.

**Стандартные пресеты** (для UI quick-bar):

| Preset | Pace | Stance | Posture | PathStyle |
|---|---|---|---|---|
| `Default` | Walk | Stand | Standard | Direct |
| `Cautious` | Walk | Crouch | Standard | CoverSeek |
| `Rush` | Run | Stand | Standard | Direct |
| `Sprint` | Sprint | Stand | Standard | Direct |
| `Stealth` | Walk | Crouch | Quiet | RoadAvoid |
| `Prone Crawl` | Walk | Prone | Quiet | CoverSeek |

UI показывает 4 dropdowns / icon-rows для каждого рычага и presets как горячие кнопки.

**Что НЕ делаем в Phase Movement:**

- Реальная баллистика «силуэта зависит от Stance» — Phase Combat.
- Audio detection model — Phase Tactical AI (Vision уже есть, audio добавится).
- Animation transitions — Phase Polish.
- Vehicle-специфичные path styles (cross-country / forest avoid / minefield avoid) — Phase Vehicles.

---

## §6. Правила огня (RoE)

**`EngagementRules` на Squad'е** (per-unit override опционален):

```go
type EngagementRules struct {
    Mode             EngagementMode  // HoldFire / ReturnFire / FreeFire
    FireOnInfantry   bool
    FireOnArmor      bool
    FireOnAircraft   bool
    FireOnStructures bool             // редко true — артиллерия / разрушаемость
    Standoff         StandoffPolicy   // Close / Medium / Long / Any
    SectorRestriction Sector          // optional cone — only fire within
}

type EngagementMode uint8
const (
    HoldFire    EngagementMode = iota  // не стреляет вообще
    ReturnFire                         // стреляет только если по нему стреляют
    FreeFire                           // стреляет по всему разрешённому
)
```

**Дефолт по doctrine** (Phase Doctrine):

| Doctrine | Mode | Targets allowed | Standoff |
|---|---|---|---|
| `Patrol` | ReturnFire | Inf+Arm | Any |
| `Assault` | FreeFire | Inf+Arm+Struct | Close+Med |
| `Stealth` | HoldFire | (none) | (n/a) |
| `Defense` | FreeFire | Inf+Arm | Med+Long (далеко не пускать) |

**Quick-toggle на UI** — RoE-bar:

- Mode row: Hold / Return / Free (3 кнопки, mutually exclusive).
- Targets row: 4 toggle (Inf / Arm / Air / Struct).
- Standoff row: 4 toggle (Close / Med / Long / Any).
- Optional «Engage that» режим: выделил target → `E` или специальный приказ `SuppressFire` (engagement без movement).

**SuppressFire (как order, см. §3)**. Sub-targeting:

- `Entity` — стрелять по конкретной цели до уничтожения / приказа stop.
- `Sector` — заливать огнём конус территории (RMB-drag для определения конуса). Pure suppression: ammo flies, hopefully scares enemies in the sector.
- `Building` — стрелять по любому видимому источнику огня из здания (через окна).
- `Window` — конкретное окно (для precision).

**Что НЕ делаем в Phase RoE:**

- Per-unit RoE override через UI — Phase UI expansion (на API уровне есть).
- Ammo conservation / rationing logic — Phase Combat.
- Target priority по type / threat-level — Phase Tactical AI.

---

## §7. Инженерное дело

**Build orders** — расширение Order taxonomy (см. §3). Игрок ставит план, инженер исполняет.

**Что строится в MVP:**

| Kind | Effect | Time | Requires |
|---|---|---|---|
| `Trench` | Polyline cut через `Stamper.Trench` | 60s per 10m | Engineer + лопата |
| `Sandbag` | Prop entity, blocks LOS+movement low, провaides cover | 30s per piece | Engineer + supplies (Phase Logistics) |
| `LogBarrier` | Prop entity, blocks movement, тонкий cover | 20s per piece | Engineer |
| `MineField` | Mine entity'и в зоне | 10s per mine | Engineer + мины |
| `ATObstacle` | Чешские ежи / надолбы | 40s per piece | Engineer |

Не в MVP, планируется: разминирование, демонтаж, ремонт техники, постройка bunker (multi-stage).

**Lifecycle постройки:**

1. Игрок выбирает иконку в build-toolbar → курсор entrails build-mode.
2. LMB ставит план: ECS-сущность с компонентами `BuildPlan{Kind}`, `WorldPos`, `BuildProgress{Value=0}`, `BuildPlanGhost` (для рендера полупрозрачного outline).
3. Если у выделенного Squad'а есть Engineer и они получили `Build` приказ → MoveTo к плану + start building.
4. EngineerSystem каждый тик увеличивает `BuildProgress.Value`. При >= 1.0 — план commit'ится: для Trench применяется `Stamper.Trench`, для Sandbag/etc. спавнится real prop entity.
5. План-сущность удаляется.

**Multiple engineers** ускоряют: `effective_rate = base_rate * sqrt(engineer_count)` (так, чтобы 4 инженера давали 2× ускорение, не 4×).

**Maps**: планы видны на карте как полупрозрачный outline в squad-color. Игрок видит queue работ.

**Что НЕ делаем в Phase Engineering:**

- Стоимость в ресурсах — у нас нет ресурсной экономики. Постройка стоит только время.
- Demolition (взрывная разборка зданий) — Phase Combat / Polish.
- Repair vehicles — Phase Vehicles+.
- Carry sandbags as inventory — Phase Polish (сейчас «магически появляются»).

---

## §8. Координация отрядов

**Multi-squad selection** — выделение нескольких отрядов через marquee. Уже работает в Phase 9 на низком уровне (`selected []ecs.Entity`).

**Joint order — общий приказ нескольким отрядам.** Реализация:

```go
// На каждом из участвующих Orders:
type OrderGroup struct{ GroupID uint32 }  // одинаковый для join'd orders
```

Когда игрок выделил N Squad'ов и дал общий приказ, **resolver спавнит N Order'ов** (по одному на Squad), все с одинаковым `GroupID`. Каждый Squad'у — свой target (auto-distributed) — но они **знают друг про друга** через GroupID, что позволяет:

- AI читать «соседний squad по моему GroupID» для координации (Phase Tactical AI).
- UI рисовать joint-arrow с разветвлением.
- Cancel all через `GroupID`.

**Auto-distribution** для multi-squad → одна цель:

- Target — точка: N squad'ов разворачиваются полукругом вокруг точки. Spacing зависит от количества (например, 8 м per squad).
- Target — здание: N squad'ов распределяются по сторонам здания (по 2 на стороне максимум). Каждому свой own approach direction.
- Target — line (по RMB-drag): squad'ы выстраиваются по линии равномерно.

**Pie menu для multi-squad**: показывает только приказы, которые имеют смысл (multi-squad version):

- Joint MoveTo (auto-distribute по фронту)
- Joint AttackMove
- Joint Garrison (распределение по разным зданиям если их несколько)
- Concentrate Fire (один target, все стреляют) — спецприказ через `OrderKind=SuppressFire` + `OrderGroup`
- Flank (vector-drag даёт направление; одни идут frontal, другие охват)

**Infantry+Vehicle interop** (Phase Vehicles+):

- `Mounted` (внутри техники): Squad-entity получает `EmbarkedIn{Vehicle ecs.Entity, Mode=Mounted}`. Юниты-члены: `Visible=false`, `Collider` off, `WorldPos` синхронизуется с vehicle. Pathfinding — через vehicle. Защищены бронёй.
- `Riding` (на броне, советский «tank desant»): то же `EmbarkedIn{Mode=Riding}`, но `Visible=true`, юниты видны и уязвимы. Лимит по типу техники.
- `Following` (за бронёй пешком): `EscortPair{With=vehicle, Mode=Following}`. SquadMacroPath держит центр отряда на N м позади/сбоку от vehicle, со стороны, противоположной от `ThreatDir`. Cover Shadow от техники (Phase Tactical AI).
- `Escorting` (техника прикрывает пехоту): `EscortPair{Mode=Escorting}`. Vehicle подстраивает скорость под пехоту, идёт рядом.

**Деталь — Cover Shadows.** Vehicle с компонентом `DynamicCoverEmitter` каждый тик пересчитывает 6-8 «теневых» cover slot'ов вокруг своей оси (DESIGN-pre-vision.md §169). Front/Side/Rear оценка от глобального ThreatMap. Following infantry в этой группе выбирает highest-scored shadow. При смене ThreatDir — перебегают на другую сторону техники.

**Platoon / Company hierarchy** — не в MVP. Если когда-нибудь понадобится — добавляем `Platoon` entity с `[]Squad children`, иерархические приказы. Phase Strategic AI или Phase Multi-squad expansion.

**Что НЕ делаем в Phase Multi-squad:**

- Авто-сводные группы (динамически объединять-разделять отряды) — Phase UI expansion.
- AI assignment of role to squad in joint order (одни flank, другие frontal — без явного указания игроком) — Phase Tactical AI.
- Ad-hoc «сводная огневая группа» из членов разных Squad'ов — DESIGN-pre-vision уже предлагает через `T` (создаёт новый squad, разрывает старые). Это есть в Phase 9, не требует доработки.

---

## §9. Интерфейс: панели и view'ы

**Дисплейная архитектура — equal-dual.** 3D-сцена и 2D-карта оба first-class. Default layout (preset «Field»):

```
+---------------------------------------+----------------+
|                                       |                |
|                                       |   Inspector    |
|             3D scene (60%)            |   (15%)        |
|                                       |                |
+---------------------------------------+----------------+
|                                       |                |
|             3D scene cont.            |   2D map       |
|                                       |   (25%)        |
+---------------------------------------+----------------+
                Time controls (bottom bar)
```

Preset «Command» (Tab toggles):

```
+----------------+--------------------------------------+
|                |                                      |
|   Inspector    |                                      |
|   (15%)        |             2D map (60%)             |
|                |                                      |
+----------------+--------------------------------------+
|                |                                      |
|   3D scene     |             2D map cont.             |
|   (25%)        |                                      |
+----------------+--------------------------------------+
                Time controls
```

Tab = swap (3D ↔ map в основном слоте). Игрок может resize splitter'ы между панелями. L4-полная-свобода drag-undock — отдельная фаза.

**L1->L4 внедрение поэтапно (после reorganization 2026-05-18):**

| Этап | Фаза | Содержание |
|---|---|---|
| L1 | Phase 10 (Interface MVP) ✅ | Фиксированный layout, 4 панели, без drag/resize/scroll |
| L2 | Phase 13.5 ✅ | Scrollable + resizable splitter'ы, без новых панелей |
| L3 (foundation) | Phase 15.C | Reason feedback / event log panel / map pings / AttackMove warning / inline order timeline. Базовый UI/UX layer над Phase 14 combat. Использует существующие L1 slots через Tab swap. |
| L4 (full) | Phase 18 | Полный Blender-стиль: splittable + dockable + floating + tabs + presets save/load. Animated git-tree timeline panel. Undo/Redo plan edits. PanelManager refactor на tree-of-splits. |
| L3 (advanced panels) | Phase 21 | После L4 - новые специализированные панели в новой framework: Orders queue / Formation / Equipment / Build toolbar / RoE quick-bar. Tree visualization Order'ов. Marker context menus. Auto-pause matrix. |

**Reorganization rationale.** Изначально L4 был в Phase 22 (после всех геймплейных subsystems). После Phase 14 (Combat) выяснилось что нет normального UI для отслеживания "что squad делает + почему" - игрок не понимает AI behaviour. L3 foundation (Phase 15.C) дает базовый reason feedback / event log параллельно с tactical AI. Полная L4 (Phase 18) - после buildings (Phase 16) и visual fidelity (Phase 17), когда UI работа имеет critical mass content'а для отображения.

**Render-архитектура:**

- **3D-сцена** через `rl.RenderTexture2D`. Своя камера (perspective), все 3D-draw calls идут в RT. RT-текстура копируется как картинка в panel-rect.
- **Карта** — 2D drawing напрямую в panel-rect через `rl.DrawLine`, `rl.DrawCircleV`, `rl.DrawRectangle`, `rl.DrawTextEx`. Scissor / viewport clip ограничивает рисование панелью. **Никаких RT, никакого ortho-3D-render.**
- **Inspector / Time controls / future panels** — direct 2D draw + raygui-style immediate widgets, ограничено scissor'ом.

**Карта в деталях.**

**Principles (после reorganization 2026-05-18).**

Карта - 2D **символическая абстракция**, не top-down 3D-render. Объекты представляются геометрическими примитивами + letter/word labels. Никаких textur'ных PNG для terrain - hill-shade + biome tint процедурно генерируются (см. ниже).

Статический pre-baked layer (генерируется при загрузке мира):

- **Terrain underlay**: hill-shaded greyscale heightmap + per-cell biome tint. Sample каждые 4-8 м из `GroundHeight` + `BiomeAtlas` (если присутствует, Phase 17+; иначе только hill-shade). Topology lines (contour) - опционально, при zoom-in. Generated, не static PNG.
- **Roads**: цветные линии по `RoadGraph.Edges`. После Phase 17 - smooth curves (Catmull-Rom tesselation), не straight segments.
- **Rivers**: голубые линии. Smooth curves параллельно с дорогами после Phase 17.
- **Buildings**: filled rectangles с outline + letter glyph центрированно (см. ниже).
- **Forest regions**: green polygon outlines + light-green fill, не индивидуальные деревья. Aggregated из tree props через convex hull или alpha-shape (Phase 17 work).

Динамический layer (рендерится каждый кадр):

- **Squad markers**: круг радиуса 6-12 пикселей (зависит от zoom), цвет = squad-color (palette), внутри commander's short label (Phase 12). Для multi-select подсветка cyan outline.
- **Unit markers** (при zoom-in): letter / word per unit role (R, L, MG, etc.).
- **Order markers**: линия от squad-center к Order.Target. Иконка типа приказа над target. После Phase 14.5 - read'ится из `OrderKindSpec.MapIconGlyph` (single rune label).
- **Path markers**: точки waypoint'ов из `MacroPath` соединены тонкой линией.
- **Map pings** (Phase 15.C+): pulsing circles на key events (EnemyContact / KIA / OrderCompleted / OrderFailed). TTL-driven despawn.
- **Vision** (Phase 13+): silhouette видимой зоны (alpha mask). Невидимая часть — приглушённый terrain.
- **Plans** (Phase 21 Engineering): полупрозрачный outline для in-progress построек.
- **Selection rectangle** (marquee): обычный 2D-rect overlay поверх.

**Object symbolic representation (Phase 17+).**

Каждый game object с map presence имеет ECS-компонент описывающий symbolic shape:

- `MapShape{Kind, Outline}` - геометрия (Rect / Circle / Polygon / Line).
- `MapLabel{Letter rune, Color rl.Color}` - текстовая метка (один rune, e.g. "T" для tower).
- `MapTint{rl.Color}` - fill tint.

Building → Rect + per-kind letter (H = house, T = tower, B = barracks, F = factory, etc.). BuildingKind enum обновится с letter-spec mapping.
Forest cluster → Polygon (convex hull / alpha-shape over tree props) + green fill.
River → Line series + blue tint.
Road → Line series + grey tint, varying width by RoadKind.

Это вместо textur'ных PNG. Дороги/реки/forests/buildings - всё procedurally / symbolically. Texture-PNG для terrain underlay - тоже generated (hill-shade + biome composition), не static asset.

**Map камера:**

- Orthographic top-down (no perspective).
- Pan: middle-mouse-drag или WASD (когда фокус на карте).
- Zoom: mouse wheel (continuous, 0.5×-8× от reference).
- Default: центрирована на anchor (как 3D-камера).

**World ↔ screen conversion на карте:**

```go
// 2D map имеет origin/scale поверх WorldPos space.
func mapScreenToWorld(screen rl.Vector2, mapCam MapCamera) components.WorldPos
func mapWorldToScreen(wp components.WorldPos, mapCam MapCamera) rl.Vector2
```

Реализация — обычное scale + translate. Никаких 3D-матриц.

**Mouse routing.**

`PanelManager` каждый кадр определяет:

1. `focusedPanel`: панель под cursor'ом (top-Z в layout).
2. Routing: глобальные обработчики input (`IsMouseButtonPressed`, etc.) проверяют `if focusedPanel == X { handle for X }`. Это самый дешёвый паттерн для immediate-mode.
3. Selection / hover state — глобальные переменные, panel'и читают их для отрисовки.

Скрытое замечание: WASD-anchor сейчас работает безусловно. Когда фокус на map-панели, WASD должен двигать map-камеру, не anchor. Решается через `focusedPanel == map ? pan_map : move_anchor`.

**Time controls.**

| Element | Action |
|---|---|
| Pause/Resume | Space |
| Speed compression | `+` / `-` (циклит 1 → 2 → 4 → 8 → 16 → 32 → 1) |
| Display | Текст "PAUSED" / "1×" … "32×" (+ `(cpu)` когда сим не успевает) |

Implementation: `app.TimeScale float32` = число фиксированных тиков `SimDt` за кадр (шаг симуляции неизменен никогда). При TimeScale=0 — paused (input принимается, физика стоит).

**Компрессия выше 8× не существует без авто-реакции** (Phase 19.7 P1). Смысл 32× — проскочить марш, а марш — это ровно то место, где случается первый контакт; скорость без механизма «игра сама заметила» покупает скуку ценой пропущенного боя. Поэтому `AttentionMatrix` (`EventKind → Ignore/Notify/Slow/Pause` поверх `EventLog`) и высокие скорости — одна фича, а не две. Правило: **автоматика только понижает скорость, повышает только игрок** — авто-возврат к компрессии превращается в йо-йо и отнимает у игрока часы. Верхняя граница — не константа скорости, а бюджет `core.AdvanceBudget`: не успели — тиков за кадр меньше, и топбар говорит об этом честно.

**Inspector содержание (Phase Interface MVP):**

- Если `selected` пустой: «No selection» + список текущих squad'ов с состоянием (для navigation).
- Если выделен 1 Squad: имя (хэш), членов (avatars + role icons), состояние (SquadState.Code: Idle/Moving/Engaging/Pinned/Scrambling), текущий приказ (kind + target + progress), формация (kind + spacing), doctrine, RoE summary, comm status.
- Если выделено несколько Squad'ов: суммарная статистика (общее число членов, roles distribution, оrders pending).
- Если выделен 1 юнит: role, stance, motion (speed, yaw), suppression level, awareness count, equipment list, parent squad link.

**Что НЕ делаем в Phase Interface MVP:**

- Movable panels (L3) — отдельная фаза.
- Splittable panels (L4-split) — отдельная фаза.
- Dock zones / floating windows (L4-polish) — отдельная фаза.
- Orders queue panel — Phase UI expansion (после Orders foundation).
- Formation / Equipment / Log / quick-bar panels — Phase UI expansion.
- Tactical map с FoW — Phase Tactical AI.
- Layout persistence — Phase UI polish.
- Кастомные палитры цветов, скины — Phase Polish.
- Tooltip system — Phase UI expansion.

---

## Архитектурные cross-cuts (не привязаны к одной секции)

**Universal simulation, time-slicing для дорогого.** Тактический симулятор моделирует мир целиком — отряды и техника продолжают исполнять приказы независимо от того, куда смотрит игрок (это базовое требование жанра, см. Combat Mission / Sea Power). Конкретные правила:

- **Cheap-системы (movement step, ActionQueue consume, OrderResolver lifecycle)** — работают каждый кадр на всех entity, без LOD-фильтров. Per-frame стоимость на 1000 юнитов ≈ 1-2 мс, не блокирует.
- **Expensive-системы (vision raycast, tactical AI utility, MacroPath A* replan)** — time-sliced через hash-bucket distribution. `bucket = uint32(entity.ID()) % bucketCount`; в текущий тик обрабатывается один bucket; цикл = `bucketCount × tickInterval`.

System-specific bucket counts (предварительно, тюнинг при появлении больших сцен):
- Vision @ 60 buckets ≈ 1 сек cycle.
- Tactical AI utility @ 300 buckets ≈ 5 сек cycle.
- MacroPath replan @ 180 buckets ≈ 3 сек cycle (на squad'ах, не units).

Helper в `core/`:
```go
// ShouldProcessBucket возвращает true если entity попадает в bucket
// текущего тика. Worker-loop-friendly (no shared state).
func ShouldProcessBucket(entityID uint32, bucketCount uint32, frameIdx uint32) bool {
    return entityID % bucketCount == frameIdx % bucketCount
}
```

**Worker pool concurrency.** Persistent pool в `core/worker_pool.go`, размер = `runtime.NumCPU()` дефолт, CLI flag `-workers=N` override. Hot-tune (рантайм изменение размера) пока **не делаем** — но `WorkerPool.Resize(n int)` API оставляем как заглушку для будущего in-game settings.

Per-system threading policy:
- **Movement (всегда parallel)**: snapshot serial (neighbours list) → parallel step (chunk units across workers) → no post-pass (movement writes own WorldPos only).
- **Vision (parallel)**: snapshot serial (units + walls by chunk) → parallel per-seer (raycast, awareness write) → no post-pass.
- **Formation (parallel)**: snapshot serial → parallel per-squad (ActionQueue writes) → serial post-pass (cohesion-leave из leaveBuffer).
- **SquadMacroPath (parallel)**: parallel per-squad (independent A*) → no post-pass.
- **OrderResolver (serial)**: archetype mutations (Completed → cleanup). Parallel split не даёт выигрыша + удобнее держать сериально.
- **Particle simulation (Phase 14+, parallel)**: trivially embarrassingly parallel.
- **TerrainGen/Load/Mesh (parallel)**: per-chunk independent.

Constraints:
- Все **raylib calls — main thread only**. Никаких background-горутин с DrawMesh / UploadMesh / LoadTexture / etc.
- Ark `ecs.Map[T].Get` — concurrent reads safe (если нет concurrent writes на ту же entity).
- Ark `Add/Remove` (archetype mutation) — strictly serial. Buffer + apply pattern.
- Determinism не требуется (single-player sandbox, нет replay / MP). Если когда-нибудь понадобится — добавится через sorted-order pre-pass.

**Hybrid bullet physics** (Phase 14 Combat). Каждый выстрел = **один instant raycast** в момент огня (от ствола к точке прицеливания). Hit/miss/penetration/cover вычисляется по результату raycast против сцены + dynamic entity collider'ов. Бесплатно поддерживает: cross-LOS через окна, cover-физику, дружественный огонь, penetration через тонкие стены. Бесплатно НЕ поддерживает: реальную баллистическую траекторию с гравитацией / drag — это apporximируется через aim-adjustment пред-raycast'ом (не in-flight). Tracer'ы (визуальные «пули в полёте») — отдельная particle система, к симуляции не имеет отношения.

Параллелизация: snapshot serial (target positions) → parallel per-shot raycast → serial apply damage. Culling: out-of-range / out-of-LOS / RoE-disallowed = instant miss без raycast.

**Spatial hashing** (Phase 14 Combat). Для vision query'ей / separation neighbour list / combat target search на больших сценах. Grid 32×32 м поверх мира, в каждой ячейке — list of entity IDs. Queries «entity в радиусе R от X» становятся O(R²) cells × ~10 entities/cell вместо O(N²). Без него Vision на 1000 юнитах = 1M pair-check'ов = катастрофа.

Сейчас (Phase 11) у нас 12 юнитов — O(N²) ok. Phase 14 наступит когда добавим combat и vision начнёт реально работать.

**Transient physics для wreckage / debris / ragdoll** (Phase 14+ / Phase 25 Polish). Объекты с активной физикой живут 5-30 секунд, потом либо despawn'ятся, либо freeze'ятся в `PhysicsFrozen` маркер (становятся статикой). Никаких permanent physics-объектов. Wreckage уничтоженной техники после settling = обычный `Prop`. Это держит количество active physics-объектов на десятках, не тысячах.

**Marker-component pattern для AI override.** SurvivalInstinct ставит `TacticalOverride` маркер на юнит → FormationSystem фильтрует `Without[TacticalOverride]` → SurvivalInstinct владеет ActionQueue до тех пор пока маркер на месте. Тот же паттерн для других препимптеров: `Reloading`, `MountingVehicle`, `Healing`, `PhysicsFrozen`.

**Aviation** — Phase 20, детальный план в `PHASE-20-AIR.md` (проектная сессия 2026-08-23). Воздушные юниты — отдельный locomotion class. Архитектурно: Movement system **не должна** предполагать «entity всегда на ground» — Y-координата всегда free, GroundStick применяется только к entity с маркером `OnGround`. Три решения, зафиксированные разбором и переопределяющие ранние наброски: управление **приборное** (маршрут + липкие дилы скорости / высоты / излучения), а не рулевое — иначе получается click-spam, отброшенный в §1; **высота непрерывна**, полосы (NOE / низкая / средняя / транзит) — производный классификатор, иначе полёт на бреющем невыразим; обнаружение воздуха — **aircraft-major**, терраин-проба нужна только на бреющем. Ядро фазы — управление излучением: активный сенсорный канал видит дальше и поднимает собственную заметность, и это решение принимают обе стороны (`FireOnAir` и слоты `SensorRadar` / `SensorAcoustic` заведены заранее именно под это).

**Pre-baked map data** vs procgen — переход. Сейчас местность процедурная (`GroundHeight(x,z)`). Когда мы перейдём на pre-baked carte (Phase Content pipeline), интерфейсы остаются те же: `GroundHeight` становится «sample из текстуры», `RoadGraph` загружается с диска, etc. Map view (§9) уже сейчас должен поддерживать оба источника, потому что pre-baked underlay-текстура карты генерируется один раз при старте мира.

**Save/Load** — пока за рамками. В Phase 25 (Polish) добавляется полный save/load (Squad-state, Order-queues, Vehicle-state). Сейчас сохраняются только Modified terrain chunks.

---

## Что в этом документе НЕ описано

Список того, что *требует отдельной разработки и описания* в дальнейшем, но не входит в текущую базу:

- **Стратегический ИИ** (operational + strategic уровни, фракции противника, decision-making outside the player's view) — нужно отдельное описание, базовая идея в DESIGN-pre-vision §Стратегический ИИ (Utility AI + опционально нейросеть). После того как тактика работает.
- **Контент-пайплайн** (OSM, Landsat, DEM, замена placeholder моделей) — отдельный документ или раздел. Сейчас держим placeholder, формат не меняем.
- **Сценарии / миссии / кампания** — какие миссии играются, как они генерируются / задаются. Sandbox в первую очередь, нарратив — позже.
- **Звук** — пока silence. Phase Polish добавляет SFX + музыка.
- **Multiplayer** — потенциальная далёкая опция, не приоритет.
- **Modding / scripting API** — никогда (single-dev project).

---

## Связь с остальными документами

- **`ROADMAP.md`** — порядок фаз, derived from this document.
- **`PHASE-N.md`** — рабочий план активной фазы. Каждая фаза имеет своё P-decisions (фиксированные решения) и M-milestones (мильстоуны разбивки).
- **`CLAUDE.md`** — entry point для AI-ассистентов, технический стек (Ark ECS, raylib-go), архитектурные паттерны, как добавлять системы. Игровая стратегия — здесь, в GAMEDESIGN.md.
- **`.claude/old/`** — закрытые фазы, архивные черновики (`DESIGN-pre-vision.md` — предыдущий topic-doc; `PHASE-N.md` закрытых фаз; `RTS.md` / `RTS2.md` — самые ранние дизайн-намётки; `PHASE-3-portals.md` — отвергнутый портал-based дизайн).
