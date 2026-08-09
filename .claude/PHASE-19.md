# PHASE-19 — Техника (наземная)

Статус: **в работе с 2026-07-19**. GAME-VISION M1. Дизайн шасси — сессия 2026-07-06
(4 слоя, различия классов = данные). Prep = REFACTOR-PLAN WS-E ш.1.
**DP-4 решён 2026-07-19: co-op в горизонте 1–2 лет → Controller≠Faction входит в M0.**

**M0 закрыт 2026-07-19** (гейты: replay 22/22 PASS+HASH, saveload 4/4 OK, core tests).
Controller{Owner} на всех комбатантах/отрядах, фабрики штампуют явную принадлежность,
селекция (клик/рамка/карта/таймлайн) гейтится по Controller — попутно закрыта дыра
«рамка выделяет вражеских юнитов»; HitTester.OwnFaction вместо литерала. OnGround +
Wheeled/Tracked + core.VehicleSpatialHash (обёртка — ресурсы типо-ключёвые).
VehicleSpec 5 классов, Vehicle/Turret/RoadFollower/SmokeField, entities.VehicleFactory,
5 машин в песочнице, рендер корпус+башня+ствол, save_spec строки, containsVehicle ожил.

**M1 закрыт 2026-07-19** (гейты на финальном билде: replay 23/23 PASS+HASH, saveload
5/5 OK — включая новую ai_vehicle_move в обоих). `systems/vehicle_driver.go`: kinematic
arc-steering по ActionQueue, yaw-rate = |Speed|/TurnRadius + pivot (Tracked 0.6 rad/s),
уклон вперёд по HeightSampler режет скорость (пол 0.25), конверт торможения к точке,
реверс при цели позади (< 4×TurnRadius) с гистерезисом; **передача = знак Motion.Speed —
приватного cross-tick состояния нет, save-continuity бесплатна** (SAVELOAD OK с сейвом
посреди езды с первой попытки). Сцена ai_vehicle_move: грузовик+танк, 4 плеча, плечо 4
на ~160° позади — грузовик пятится, танк пивотится; ранний вердикт по прибытии (~40 s).
Техника включена в ReplayHasher (pos/yaw/speed/hp/queue). Отклонение от плана: путь =
прямые waypoint'ы, БЕЗ NavGrid A* — вся маршрутизация (RoadGraph A* + NavGrid-фоллбек +
клетки-запреты) уезжает целиком в M2, где у неё появляется реальный потребитель.

**Интерлюдия movement-polish закрыта 2026-07-19** (перед M2, по owner-фидбеку):
ISSUES #12/#13 закрыты (корни и фиксы — в ISSUES.md Closed), плюс лидер-якорь строя
с догоняющим бегом (SquadAnchorPos + bb.CatchUp) и двухфазный заход в здание
(standoff MoveTo → interior-фаза чейном). Инварианты 6-12 — в memory
movement-invariants. Suite вырос до 25 сцен (ai_march_line/slope — метрические:
side/churn/clearance). Гейты 25/25 + saveload 6/6.

**Дев-блок закрыт 2026-07-19** (перед M2, по owner-решению «сначала дев-блок»):
Debug-виджет дорос до воркбенча-лайт — Step 1/10/60 (пауза + точные тики через
`App.StepOnce`), спавн-палитра (Rifleman/Enemy/5 классов техники — LMB в 3D),
9 sticky-оверлеев (включая близнецов hold-G/hold-K), живой raw-дамп компонентов
выделенной сущности через reflect. Panic-guard: паника в тике → recover, стек в
`$TMPDIR/rts-panic-<pid>.txt`, фатальный экран вместо краха (headless: exit 2);
самопроверка `RTS_PANIC_AT=N`. Детали — CLAUDE.md Controls. Полный воркбенч-цикл
(world-lifecycle, манифест сценария, меню) отложен в фазу «Workbench = каркас
Операции v0» после Phase 19.

**M2 закрыт 2026-07-20** (гейты на финальном билде: replay 26/26 PASS+HASH,
saveload 7/7 OK — включая новую ai_vehicle_road в обоих). Маршрутизация:
`systems/road_router.go` — ленивый adjacency + Dijkstra с виртуальными off-road
съездами ко всем узлам, цена ребра = длина/скорость класса — спековые скорости
сами кодируют предпочтение дороги (грузовик 16/6 делает крюк, танк 17/11
срезает; грунтовка ×0.8), отдельных коэффициентов нет. `RoadRoute` (POD,
[32]uint16 узлов; Planned+Goal гейтят replan) планируется один раз для
головного MoveTo; на рёбрах круиз MaxSpeedRoad, `RoadFollower{Edge,T}` пишется
каждый тик (проекция корпуса на ребро), sticky-rejoin при сносе с полосы.
Мосты: GroundStick берёт Y с настила — лерп высот узлов + bridgeYOffset с
4-метровыми рампами на концах, max() с поверхностью. makeStartingRivers теперь
отдаёт реки ai-сценам со своим манифестом карты (мост valley обязан
существовать). Сцена ai_vehicle_road: два грузовика патрулируют valley
запад↔восток; PASS = прибытие И bridge_ticks>0 (фактический проезд по мосту —
54 тика). ReplayHasher дополнительно хеширует RoadRoute.Head/Count/Planned +
Follower.Edge/T: обнулённый маршрут replan'ится в почти то же руление и не
виден по одной позиции. Отклонение от плана: NavGrid-фоллбек оффроуд-плеча и
клетки-запреты снова отложены (плечи = прямые, как M1) — до реального
потребителя; on/off-ramp = ближайший-по-времени узел, не проекция на ребро.
Дальше M5-lite (селекция + RMB MoveTo + ghost для техники), затем M3 Gunner.

**M3 закрыт 2026-07-20** (гейты: replay 28/28 PASS+HASH, saveload 9/9 — сцена
ai_vehicle_combat в обоих, сейв на тике 1000 попадает в разгар боя).
Weapon-vs-class: WeaponSpec += VsSoft/VsLight/VsHeavy, ArmorClass в
VehicleSpec (Truck=Soft, BTR/BMP/ATC=Light, Tank=Heavy); pickTarget скорит
кандидатов классовым множителем (пушка предпочитает броню, coax — пехоту),
класс с множителем <0.05 не выбирается вовсе — ПКМ не тратит ленты на танк.
Секторная броня: armorSectorMul в resolveShot (нос/борт/корма по cos между
направлением выстрела и яу корпуса, порог ±0.5). Новые WeaponKind: 125mm /
30mm AC / KPVT / ATGM (append после пехотных — сохранённые Kind стабильны);
лоадаут в VehicleSpec (WeaponKinds[2]), фабрика спавнит weapon-сущности с
OwnedBy и ставит Equipment.Active (нулевой Active ронял applyCombatEvidence —
alive-check добавлен). Gunner (weapon_gunner.go): пер-ствольный pickTarget,
башня слюится к цели primary (TurretSlewDps), огонь только в допуске 0.06
рад; coax стреляет, когда башня уже смотрит близко к ЕГО цели; стабилизация =
+80% дисперсии на ходу. RoE: shouldFire различает цели — FireOnInf для
пехоты, FireOnArm для техники (тумблер обрёл читателя; ATGunner-пресет
«только по броне» теперь работает как задуман). Сплэш бьёт технику через
vehicle-хеш (класс × бортовая броня). ContactSystem: техника = цель
(DimVehicle, concealment=DetectMul, шум NoiseRadiusM/×0.25 на холостых) И
наблюдатель (свои Sensors) — вражеская техника получает контакты/FoW.
Башня визуализируется (drawVehicleBox вращает верхний блок Turret.Yaw),
Inspector техники показывает стволы и боезапас. Сцена: танк+БТР vs БМП+ПТ на
~45 м, бой 17.5 с — БТР гибнет от ATGM, танк переживает фронтальный обстрел
(336/500) и сносит обоих. Хвост: пехотный РПГ угрожает технике через ту же
таблицу (VsLight 1.2), отдельной сцены нет — покроется смешанным боем M4/M5.
Дальше M4 (рефлексы FaceThreat/SmokeAndReverse/Flee).

**M4 закрыт 2026-07-21** (гейты: replay 29/29 PASS+HASH, saveload 10/10 —
сцена ai_vehicle_reflex в обоих). Поведение техники = приоритетный арбитраж
веток в драйвере (не BT): Reflex (FaceThreat/SmokeAndReverse владеют
локомоцией) > Flee (через очередь) > Orders (ActionQueue) > Idle; Gunner
ортогонален. `VehicleOverride` — новый POD, всегда на технике (Kind=None в
покое; строка save_spec; хешируется в ReplayHasher — LastAt/Retreat/ThreatYaw
переживают save/load byte-exact). Пререквизит угроз: фабрика добавляет
DangerBuffer+Threat+VehicleOverride, ThreatSystem получил второй Vehicle-проход
(тот же tickThreat), propagateSuppression шлёт DangerBulletImpact и по
vehicle-хешу. VehicleReflexSystem (после threat_decay, перед order_resolver,
Active-only) при Threat.Total ≥ 0.4 и вне кулдауна (10 с) ставит спековый
ReflexKind (танк FaceThreat, БТР/БМП/ПТ SmokeAndReverse, грузовик Flee).
FaceThreat пивотит корпус к угрозе (cruise 0, выход |err|<0.1 или Until~6с);
SmokeAndReverse спавнит SmokeField (r8/TTL8) и пятится ЛИЦОМ к угрозе (гунер
держит лучший сектор), выход при откате ≥8 м или Until~7с; Flee толкает MoveTo
на pos+ThreatDir·40 в обычную очередь. SmokeField: ContactSystem режет
concealment ×0.35 внутри живого поля, ThreatDecay деспавнит по ExpiresAt,
рендерится полупрозрачной сферой. **Развилка «выстрел важнее вздрагивания»**:
рефлекс НЕ срабатывает, пока в Awareness есть живой враг (≤3 с) — рефлексы
только для огня, на который нельзя ответить (невидимый стрелок/засада). Это
восстановило ai_vehicle_combat (M3-гуннери-тест регрессировал, когда вражеские
лёгкие корпуса начали уклоняться дымом-реверсом) без пер-сценового хака и
тематически идеально ложится на ai_vehicle_reflex (синтетический огонь без
видимого врага). Сцена: танк (боком) доворачивает лоб, БМП дымит и откатывается
≥8 м, грузовик уходит ≥30 м — три проверки в одном 7.2-с прогоне. Хвост: лёгкая
техника пока НЕ рвёт контакт с ВИДИМЫМ превосходящим врагом (v0) — break-contact
по низкому HP оставлен на будущий тюнинг. Дальше M5 (закрытие: карта-символ,
V-превью, доки/ROADMAP/GAME-VISION, memory).

**M5-lite закрыт 2026-07-20** (гейты: replay 26/26 PASS+HASH, saveload 7/7 OK —
селекция/ghost/Inspector не трогают headless-сцены, прогон подтвердил).
Вытянут вперёд по плану «ездить руками до M3». Селекция: pickUnitFromMouse /
collectUnitsInRect / hover получили второй пул — техника, радиус клика =
спековый ColliderR, ближайший из обоих пулов побеждает; hover-кольцо и рамка
работают. RMB: изменений не потребовалось — соло-техника уже шла через
pushSoloMove → ActionQueue → Driver/RoadRouter; H-Stop и T-merge работали с M0.
Ghost: соло-техника превьюится полупрозрачными hull-box'ами (drawVehicleBox с
альфой, wire-альфа = body-альфа) шеренгой у курсора, вращается facing-drag'ом;
squadded-техника остаётся на formation-ghost. Inspector: selSingleVehicle +
ui/inspector_vehicle.go — класс/HP/скорость с передачей [D/R/-]/Road-статус
(highway/local/dirt/BRIDGE + edge и %)/Faction/Squad; InspectorCtx.RoadGraph
для расшифровки Edge→Kind. Остаток полного M5 (APP-6 символ техники на карте,
проверка V-превью колец, доки/ROADMAP/закрытие) — при закрытии фазы.
Дальше M3 (Gunner + броня: weapon-vs-class, Turret slew, секторные множители,
RoE-гейт; сцена ai_vehicle_combat).

## P-решения

- **P1. Кинематика, без Jolt.** Jolt decision point из ROADMAP закрыт: поза техники =
  kinematic arc-steering на фиксированном тике. Аргументы: replay/saveload-гейты (float-
  недетерминизм Jolt между сборками), масштаб игры (оперативный, не танковый симулятор),
  memcpy-политика save_spec (POD-состояние). Крен/подвеска — render-only lean по нормали
  террейна, позже (17.5-визуал).
- **P2. 4-слойное шасси; классы = данные.** Driver (локомоция: arc-steering с ограничением
  yaw-rate = Speed/TurnRadius, реверс, road/offroad скорости) / Gunner (per-weapon:
  pickTarget по таблице weapon-vs-target-class, башня со slew rate, стабилизация =
  модификатор dispersion на ходу) / Рефлексы (TacticalOverride-паттерн: FaceThreat,
  SmokeAndReverse, Flee) / Operational — Phase 20+, здесь не строим. Классы v0 —
  **грузовик / БТР / БМП / танк / ПТ-носитель** — различаются только строкой VehicleSpec
  и набором weapon-сущностей.
- **P3. VehicleSpec — spec-table** (паттерн WeaponSpec/StanceSpec, compile-time
  exhaustiveness): Kind, Name, HP, MaxSpeedRoad/Offroad/Reverse, TurnRadiusM, SeatCount
  (читатель — Phase 20), Armor{Front,Side,Rear} множители (полная модель пробития
  отложена — GAME-VISION §10), TurretSlewDps (0 = безбашенный), Locomotion
  (Wheeled/Tracked), DetectabilityMul + NoiseRadius (сигнатуры для Detection 2.0 /
  audio_detect), Sensors-профиль (VehicleOpticalProfile уже в components).
- **P4. Vehicle = сущность из мелких компонентов** (как Unit, никакого мега-компонента):
  `Vehicle{Kind}` + WorldPos + Motion (Yaw = корпус) + Collider (крупный радиус) + HP +
  Faction + Sensors + Awareness + Equipment (оружия — отдельные сущности с OwnedBy, как у
  пехоты) + `Turret{Yaw, SlewRate}` на башенных + `RoadFollower` + OnGround. Экипаж НЕ
  моделируется отдельными юнитами (абстракция внутри Vehicle); посадка/высадка пехоты,
  Embark/Dismount, joint barriers — Phase 20. Spawn через `entities.VehicleFactory`.
- **P5. Навигация.** `components.Locomotion` расширяется (Foot / Wheeled / Tracked).
  Маршрут: узловой A* по RoadGraph + `RoadFollower` (edge + прогресс, sticky-to-edge) —
  hard-preference для Wheeled, Tracked охотнее срезает; NavGrid-фоллбек для оффроуд-плеча
  со slope-limit и запретом NavInBuilding по классу (техника в здания не входит).
  Мосты: на Bridge-ребре Y берётся с настила (лерп высот узлов), GroundStick пропускает.
  **Prep WS-E ш.1:** `OnGround` marker (GroundStick работает только по маркеру — задел под
  авиацию Phase 20); **второй SpatialHash для техники** (один хеш на locomotion class —
  инвариант из memory). Техника НЕ участвует в ORCA (неголономна): пехота уступает —
  ORCA читает vehicle-хеш как препятствия; Driver объезжает торможением/дугой.
  applySplashDamage/propagateSuppression опрашивают оба хеша.
- **P6. Боёвка.** WeaponSpec получает эффективность по target-class (ПКМ не вредит танку,
  РПГ/пушка — да); броневой множитель по сектору попадания front/side/rear от яу корпуса.
  RoE — существующие EngagementRules (FireOnArm тумблер уже есть, обретает читателя).
  Gunner при нескольких стволах (танк: пушка + пулемёт) выбирает per-weapon цель — первый
  реальный потребитель слоя per-weapon intent из COMMAND-MODEL §4 (минимум: авто-выбор,
  weapon-bar UX не строим).
- **P7. Рефлексы через TacticalOverride** (паттерн Phase 15): FaceThreat (довернуть лоб к
  ThreatDir на месте — для танка/БМП), SmokeAndReverse (дымовые гранаты + задний ход по
  своей колее — общий спек с игроцким emergency verb, GAME-VISION §5), Flee для грузовика
  (полный газ от угрозы). Дым v0 = particle burst + короткоживущая SmokeField-сущность:
  concealmentMul для целей внутри радиуса; ray-марш дыма вдоль LOS — отложен.
- **P8. Управление/UI минимум.** Техника выделяется и приказывается как юнит (MoveTo /
  AttackTarget / Stop, ghost-превью), в отряд через T (смешанный merge уже готов:
  FormationCustomSlots + OrientNorth), Inspector-строки (класс/HP/Ammo/RoadFollower
  статус), APP-6 символ на карте, кольца оружия в V-превью работают из коробки
  (Equipment→Weapon.RangeM). Отдельного vehicle-UI (weapon-bar, VehicleSystems-панель) не
  строим.
- **P9. DP-4 = да: `Controller{OwnerID}` ≠ `Faction.ID` в M0.** Явный Faction на каждом
  комбатанте — zero-value трюк «нет Faction = игрок» умирает; литералы FactionPlayer
  (грепнуть: contact_detect / main / command + спавн-сайты) переводятся на Controller-чек
  для селекции/приказов и Faction-чек для hostility. Ключ ContactRegistry (faction,
  tracked) не трогаем — это начало Phase 22 (WS-G ш.3).
- **P10. Детерминизм и save — гейты обязательны.** Все новые компоненты POD → строки в
  save_spec.go (VerifySaveSpec поймает); private cross-tick состояние новых систем →
  PostLoadHook. Новые ai_vehicle_* сцены добавляются в replay_gate.sh И saveload_gate.sh;
  полный прогон обеих — критерий каждого милстоуна с M1.

## M-милстоуны

- **M0. Prep + Faction-развилка.** Locomotion расширение; OnGround marker на юниты/якорь +
  gate в GroundStick; второй SpatialHash (resource + rebuild-pass); Controller компонент +
  явный Faction на комбатантах + перевод литералов; components.Vehicle/Turret/RoadFollower/
  SmokeField + VehicleSpec таблица; entities.VehicleFactory; спавн в тестовой сцене,
  рендер-бокс по габаритам класса; save_spec строки. Гейты 22/22 + 4/4 green (пехота не
  должна измениться поведенчески).
- **M1. Driver оффроуд.** Kinematic arc-steering, MoveTo по NavGrid с slope-limit,
  скорости по спеку, разворот через реверс при цели позади (three-point turn упрощённый).
  Сцена ai_vehicle_move (грузовик + танк, серия точек по пересечёнке) → в оба гейта.
- **M2. Дороги.** A* по RoadGraph, RoadFollower sticky-to-edge, on/off-ramp к ближайшему
  узлу, road/offroad скорость, мосты (Y с настила). Сцена ai_vehicle_road (пара грузовиков
  патрулирует между точками карты через мост) → в гейты.
- **M3. Gunner + броня.** Weapon-vs-class таблица, Turret slew + огонь только в допуске,
  секторные множители брони, RoE-гейт, стабилизация-модификатор. Пехотный РПГ начинает
  реально угрожать технике. Сцена ai_vehicle_combat (танк+БТР vs БМП+ПТ) → в гейты.
- **M4. Рефлексы — ЗАКРЫТ 2026-07-21.** FaceThreat / SmokeAndReverse / Flee по классам,
  VehicleOverride-арбитраж в драйвере, SmokeField concealment ×0.35, ReflexSystem после
  threat_decay, гейт «engaging beats flinch» (рефлекс только на неотвечаемый огонь).
  Отдельная сцена ai_vehicle_reflex вместо расширения combat (чище: combat = гуннери,
  reflex = засада). Детали — параграф «M4 закрыт» вверху и «M4: бренчи действий техники» ниже.
- **M5. Управление + закрытие.** Селекция/приказы/ghost/T-merge/Inspector/карта; проверка
  V-превью; полные гейты; доки (CLAUDE.md компоненты+системы+pipeline, ROADMAP статус,
  GAME-VISION M1 галка, memory).

## M4: бренчи действий техники (план, согласован 2026-07-20)

Никакого BT (решение в хвостах): поведение = **приоритетный арбитраж веток**,
каждая ветка — «кто владеет рулением/ActionQueue в этот тик». Сверху вниз:

1. **Disabled** (обездвижен/уничтожается) — заглушка, не в M4.
2. **Reflex** — активный `VehicleOverride` (новый POD-компонент, строка в
   save_spec): `{Kind uint8, Until float32, ThreatYaw float32, RetreatTarget
   WorldPos, LastAt float32}`. Владеет драйвером вместо очереди.
3. **Orders** — ActionQueue (MoveTo/Stop), как сейчас: RoadRoute и т.д.
4. **Idle** — тормоз.

Gunner ортогонален веткам — стреляет в любой (FaceThreat лишь улучшает сектор).

**Пререквизит: угрозы для техники.** У техники нет DangerBuffer/Threat —
фабрика добавляет оба (компоненты уже в save_spec). propagateSuppression
дополнительно шлёт события по vehicle-хешу (паттерн applySplashToVehicles);
ThreatSystem получает второй проход по Vehicle-фильтру (те же tickThreat).

**VehicleReflexSystem** (новая, после `threat_decay`, перед `order_resolver`;
Active-only): по классу из спека (`ReflexKind` в VehicleSpec) при
`Threat.Total > порог` (или Suppression-спайк) и отсутствии активного
override ставит VehicleOverride:
- **FaceThreat** (танк): драйвер пивотит корпус к ThreatDir (cruise 0, только
  yaw), выход по |err| < 0.1 рад или Until (~6 с).
- **SmokeAndReverse** (БТР/БМП): спавн SmokeField-сущности (радиус ~8 м,
  TTL ~8 с) + реверс по своей колее: RetreatTarget = pos − fwd·15 на входе;
  драйвер ведёт реверсом (гир уже в знаке Speed). Выход: доехал/Until.
- **Flee** (грузовик): MoveTo от угрозы (pos + away·40) ЧЕРЕЗ обычную очередь
  (роутер сам найдёт дорогу для бегства) — ветка Orders, override только
  помечает Reason и глушит игроцкие MoveTo до Until.
Повторный вход не раньше `LastAt + cooldown` (~10 с). Выход из override →
голова очереди на месте, маршрут перепланируется сам (Goal-сравнение).

**SmokeField потребители:** ContactSystem — цели внутри радиуса живого поля
получают concealment ×0.35; ThreatDecay деспавнит поля по TTL (тот же тонкий
pass, что ThreatSource). SmokeField уже в save_spec.

**Драйвер:** step() получает ветку Reflex перед очередью (по аналогии с
route-веткой); никакого приватного состояния — всё в VehicleOverride.

**Сцена ai_vehicle_reflex** (в оба гейта): три проверки в одном прогоне —
танк, заспавненный БОКОМ к угрозе, доворачивает лоб (|yaw−bearing| < 30° к
+5 с); БМП под огнём оставляет SmokeField и смещается назад ≥ 8 м; грузовик
удаляется ≥ 30 м от стрелка. Угроза — синтетические DangerEvent-пульсы (как
ai_cover_side) или реальный стрелок, если стабильно.

**Inspector:** строка Reason для VehicleOverride (паттерн TacticalOverride).

**M6 закрыт 2026-07-31** (гейты на финальном билде: replay 37/37 PASS+HASH —
включая 4 новые сцены, saveload 14/14 OK; -race прогон ai_vehicle_yield с
SerialThresholdHint=0 чист). Реализация — `systems/vehicle_avoid.go` +
правки driver/unit_movement/orca: (1) steering `avoid()` до `drive()` —
футпринты Building-корней (объезд по углам, достижимость угла проверяется
по СЫРОМУ футпринту — проверка по инфлейченному дедлочит корпус на середине
грани) + корпуса из VehicleSpatialHash (попутный медленный → match speed,
встречный → младший ID держит курс, стоящий → касательная); (2)
`resolveOverlaps()` после advance — гарантия непроницаемости (техника-техника
полупроникновение с капом 3 м/с, здания — полный не-капнутый выброс: капнутый
проигрывает «толкучку» драйверу 6 м/с и корпус продавливается сквозь буфер);
(3) пехота уступает — shove при пересечении + боковой шаг из коридора
движущегося корпуса (1.5 с проекция, детерминированная сторона по ID) для
ЛЮБЫХ юнитов включая idle, ORCA-соседи из vehicle-хеша с Resp=1.0; (4)
реверс-гистерезис требует удержания условия 0.4 с (`RoadFollower.RevHold`) —
владельческий баг «спонтанный разворот в группе» закрыт (rev=0 в
ai_vehicle_group), arrival масштабируется с TurnRadius + `goalCrowded`
паркует рядом с занятой общей целью (допуски сцен 6→13 м). Сцены
ai_vehicle_avoid (лоб-в-лоб BTR, minPair −0.08 м) / _building (объезд дома
14.3 с, 0 тиков в футпринте) / _yield (цепь стрелков, 0 нарушений) / _group
(3 грузовика, общая цель, 0 реверсов) — в обоих гейтах. Хвосты: пропсы
BlocksMove (давимость по классам), давящий урон пехоте, avoid в
reflex-ветках (SmokeAndReverse пятится вслепую — прикрыт resolve-слоем).
Дальше M7 (отряд: owner-репро 2026-07-31 «одни укатывают вперёд» = нет
squad-pacing у техники).

## M6: Коллизии и избегание (план, 2026-07-31)

Мотив (owner-фидбек): техника проезжает сквозь здания и другую технику;
«клетки-запреты» дважды откладывались до реального потребителя — вот он.
Реализуем обе половины P5, которые остались на бумаге: driver объезжает
торможением/дугой, пехота уступает через vehicle-хеш. Без физики (P1),
всё детерминированное, порядок обхода = порядок filter-итерации.

Два слоя на каждый тик драйвера:

1. **Steering-слой** (`vehicle_avoid.go`, до `drive()`): probe вперёд на
   тормозной путь (`v²/(2·vehDecel) + BoxLen/2`, минимум ~6 м).
   - **Здания**: снапшот футпринтов живых Building-корней раз в тик
     (AlwaysActive → видны всегда, их десятки — плоский слайс). Если отрезок
     к точке прицеливания пересекает инфлейченный (`+ColliderR+0.5`) AABB →
     доворот на касательную того угла, где суммарное отклонение меньше;
     вдоль ребра — wall-following. Уже внутри (спавн/сейв) — hard-resolve.
   - **Техника**: VehicleSpatialHash (снапшот, уже перестраивается серийно).
     Попутная медленная в headway → match speed (колонна по дороге
     бесплатно). Встречная/пересекающая → приоритет по entity ID: младший
     держит курс, старший тормозит/уклоняется вправо. Стоящая → объезд как
     статика.
   - Оба направления объезда требуют > ~100° доворота → тормоз (тупик;
     реверс-гистерезис уже есть).
2. **Hard-resolve слой** (после `advance()`): гарантия непроницаемости, когда
   рулёжка ошиблась. Техника-техника: развод вдоль дельты центров по
   полупроникновению (cap скорости коррекции ~2 м/с, чтобы не телепортить);
   техника-здание: выталкивание по минимальной оси AABB.
3. **Пехота уступает** (вторая половина P5): ORCA-сосед из vehicle-хеша с
   полной (1.0, не 50/50) ответственностью на пехотной стороне — корпус не
   «делит» уклонение. Техника пехоту НЕ объезжает; при пересечении
   hard-resolve выталкивает юнита, не корпус. Давящего урона нет (хвост).
4. **Arrival-fix**: финальный радиус прибытия масштабируется с TurnRadius
   (та же логика, что rampPopRadius) — лечит орбитирование грузовика вокруг
   точки, которую он физически не может накрыть.
5. **Баг owner-плейтеста «спонтанный разворот в группе»** (2026-07-31:
   техника, ехавшая вместе, резко разворачивалась, сдавала назад и
   продолжала, разрывая дистанции). Диагноз: реверс-гистерезис срабатывает
   мгновенно (`absErr > 2.1 && dist < 4×TurnRadius` — для грузовика 32 м);
   любой транзиентный скачок aim-точки за корму (replan, rejoin,
   слот строя) включает задний ход на один-два тика условия. Фикс:
   персистентность условия ~0.4 с до включения реверса (POD-таймер в
   RoadFollower, без приватного состояния) + найти и убрать сам источник
   скачка в группе.

**Отложено из M6 (хвосты с адресами):** пропсы с BlocksMove (дерево давимо
танком? камень = стена? — нужно решение по классам → мини-пасс после M7);
давящий урон пехоте; NavGrid-фоллбек оффроуд-плеча (полноценный объезд
рельефа) — по-прежнему до реального потребителя.

**Сцены (в оба гейта):** ai_vehicle_avoid — лоб-в-лоб + пересечение курсов,
PASS = оба прибыли И min pairwise dist ≥ ΣColliderR−ε за весь прогон;
ai_vehicle_building — приказ «сквозь» здание, PASS = прибыл И ни одного тика
внутри футпринта; пехотная поперёк курса — юниты уступают, ни одного внутри
ColliderR корпуса.

**M7-core закрыт 2026-07-31** (owner-репро «одни укатывают вперёд» в тот же
день; гейты: replay 38/38 PASS+HASH — включая новую ai_vehicle_convoy,
saveload 15/15 OK). Три корня и фиксы:
1. **Нет темпа колонны** → `systems/vehicle_pace.go`: `squadPaceCap` —
   stateless-вывод из ростера каждый тик (ничего в save, нет стейла при
   Leave): cap = min по живым членам их ДОСТИЖИМОЙ скорости (road-спек на
   ребре, offroad иначе, пехота 3 м/с); лидер держит ровно скорость
   тихохода (×0.6 при растяжке — мерится ДО ЛИДЕРА минус номинальная
   глубина слота + 1.5×Spacing, НЕ до слота: слоты висят на
   вейпоинт-фронтире и читаются как вечное отставание), followers ×1.3 на
   догон. Spacing при merge с техникой = 2×maxColliderR+1
   (CreateFromUnits).
2. **Индивидуальный роутинг рвал колонну**: колёсный planning-bias уводил
   грузовик на шоссе, гусеничный сосед резал напрямую → squad-члены едут к
   слотам НАПРЯМУЮ (planRoute только для соло). Squad-level road bias
   (колонна по мосту при сквадном марше) — хвост, адрес ниже.
3. **Лайвлок «мёртвого кольца»**: M6-радиус прибытия машины
   (rampPopRadius 3.75-6 м) > радиуса продвижения вейпоинта (2 м) — лидер
   парковался между ними и марш замерзал НАВСЕГДА (раньше спасал овершут
   на нештрафованной скорости). `SquadWaypointReach` расширяет кольцо для
   vehicle-якоря (formation + macro pop); completion MoveTo расширен на
   reach + полглубины колонны (центроид колонны не входил в пехотные
   2.5 м). Плюс Forward-snap на свежий приказ из покоя (слот, сметающийся
   на 90° за слюение, стоил грузовику петли 20 м — U-turn с радиусом 8).
Сцена ai_vehicle_convoy: БМП-лидер (11) + 2 грузовика (6), марш 110 м;
PASS = все у цели И maxSpread<30 за весь прогон (до фиксов 55.9 и/или
фриз, после 26.0, марш на полных 6 м/с тихохода). В обоих гейтах.

**Дополнение M7 (2026-07-31, второй owner-фидбек «перестают слушать приказ
о построении, разъезжаются кто первый; правка строя должна перестраивать
на ходу»):** (1) корень разъезда — T-merge на ходу: CreateFromUnits не чистил
личные ActionQueue, свежий отряд без приказа их не переписывает, и каждый
мчал к своей старой цели до ручного стопа → merge теперь стирает личные
экшены (стоим в строю, ждём приказа); (2) правка строя применяется живьём:
`FormationData.ReformPending` (взводится редактором на drag/kind/preset и
хоткеями F1-F4) — марширующий отряд подхватывает новые слоты на лету
(обычный слот-пуш), idle-отряд перестраивается НА МЕСТЕ: центр заякорен так,
чтобы слот лидера лёг на его текущую позицию (иначе строй дрейфует, догоняя
сам себя), флаг снимается, когда все ведомые члены припаркованы в допуске;
(3) пол Spacing по корпусам перенесён в FormationSystem (любой писатель —
applyKind редактора, F1-F4 — сбрасывал spacing на пехотные 2 м и слоты
слипались под резолвом). Сцена ai_vehicle_convoy трёхфазная: reform на месте (t=2, CustomSlots ±24 +
ReformPending, БЕЗ приказа) → марш с фиксированного t=12 → PASS = линия
сформирована + mergeClear + прибытие + spread<30. Порядок фаз продиктован
**контрактом saveload-гейта, вскрытым первым прогоном (SAVELOAD MISMATCH):
все sim-мутации сценарного harness'а обязаны случиться ДО SAVE_AT (тик
1000) — в загруженном прогоне harness'а нет, и пост-сейвовая мутация
никогда не реплеится.** Инвариант для всех будущих ai_* сцен.

**Хвосты M7 (адреса):** (а) squad-level road bias — сквадный macro path
по-прежнему пехотный NavGrid, колонна не пользуется дорогами/мостами →
кандидат «RoadRouter для сквадного макро-гола при vehicle-only ростере»,
после owner-плейтеста; (б) route-preview скипает squad-технику; (в) mixed
пехота+техника едет на грубых 3 м/с — честные Mounted/Following = Phase 20;
(г) начальная дуга разворота follower'а с места (реверс-выход из строя) —
feel-полировка при жалобе.

## Закрытие

1. Все ai_vehicle_* сцены в обоих гейтах, полный прогон green (пехотные сцены без
   регрессий).
2. Смешанный отряд (пехота+БТР) исполняет MoveTo без развала строя и без ORCA-дёрганья.
3. Владелец прогоняет ручной плейтест: дорога/мост/оффроуд, бой танк-vs-РПГ, рефлекс.
4. Хвосты с адресами: Embark/Mounted/joint orders → Phase 20; грузовик-логистика → M5
   Supply; weapon-bar UX → UI player-pass; Jolt — закрыт навсегда (P1), пересмотр только
   если появится физический pushback-геймплей.
5. Хвосты по owner-плейтесту 2026-07-20 (feel, не блокеры): turn-anticipation —
   начинать дугу ЗАРАНЕЕ, чтобы выйти на прямую к вейпоинту (сейчас руление
   реактивное: доворот после); arrival-alignment — вставать в точке
   пропорционально направлению подхода/исходному хедингу; визуал колёс/гусениц
   (поворот передних колёс, вращение) → 17.5-визуал. Структурирование поведения
   Driver'а (BT-вопрос owner'а): НЕ вводим — три состояния (route/direct/reverse)
   не окупают дерево; пересмотреть при M4 Reflexes, которые лягут на
   TacticalOverride-паттерн; BT на тактическом ярусе закрыт P-решением 17.8.
   Баги плейтеста — ISSUES #15 (перф чанков/деревьев), #16 (ramp к узлу вместо
   проекции на ребро), #17 (нет превью маршрута), #18 (укрытие не с той стороны).
