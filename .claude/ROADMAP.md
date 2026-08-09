# Roadmap

Канонический трекер этапов проекта. Обновляется по мере прохождения фаз. **Игровое видение и архитектурные принципы — в `GAMEDESIGN.md`.** Этот документ — только «когда и в каком порядке».

**Легенда:** ✅ сделано · 🚧 в работе · 📋 следующее / в очереди · 🔮 далёкое будущее · 🗄️ заморожено в архив

---

## Что сейчас работает

- Базовый ECS на Ark + LOD-маркеры + scheduler по тиерам — ✅
- Камера + орбита + WASD-якорь, прибит к поверхности — ✅
- `WorldPos{Chunk, Local}` + render-space через `CurrentOriginChunk` — ✅
- Heightmap-чанки с потоковой загрузкой (Active 7×7, Relevant 11×11), детерминированный fBm-Perlin, бесшовные швы, юбки — ✅
- Persistence чанков (Phase 2): бинарный формат v1, eviction/shutdown-flush, `Stamper.StampHeightmap` + `Crater`-kernel — ✅
- Props (Phase 3): `Prop` + `PropTypeRegistry`, биомные маски, реки как polyline-cut + water-props — ✅
- Roads (Phase 4): `RoadGraph` singleton, `PreprocessRoadGraph`, road/junction props, мосты, road-clearance, дебаг-overlay по `G` — ✅
- Buildings (Phase 5): `BuildingPlanList` + root-сущность с `AlwaysActive`, стены/двери/окна/полы/лестницы как дочерние сущности с Smart Object данными, окопы — ✅
- Spatial intelligence (Phase 6): per-chunk `NavGrid` + `CoverMap`, road-bias path-cost, cover slots для пропов / окон / углов стен — ✅
- Базовые юниты (Phase 7): `Unit` как набор мелких компонентов, `Weapon` как отдельная сущность с `OwnedBy`, `FloorNavGrid` per-Floor + `TransitionRegistry` для multi-floor A*, `UnitMovementSystem`, `VisionSystem`, selection + ПКМ-MoveTo + `H` Stop — ✅
- Observability (Phase 7.5): `core.Profiler` (60-кадровое окно, per-system median ms), правый HUD, `Ctrl+P` снапшот, JSONL trace за build-tag'ом `trace` — ✅
- Squads (Phase 9): `Squad` как отдельная ECS-сущность с `CommandRoster`/`FormationData`/`MacroPath`/`RadioNetwork`, `SquadMacroPathSystem` + `FormationSystem`, cohesion-leash, T/U/F1-F4/Ctrl+1..5 hotkeys, render squad-connections, HUD census — ✅
- Interface foundation (Phase 10): `ui.PanelManager` + L1 layout, Tab swap Field/Command preset, 3D-сцена через `rl.RenderTexture2D`, 2D-карта с pre-baked greyscale hill-shade underlay + MapCamera (MMB-drag pan, wheel zoom), Inspector с пустым / single-unit / single-squad / multi-select содержимым, time controls (Space pause + +/- speed 1/2/4/8x), per-frame `panelMgr.FocusedAt` mouse routing с panel-local-cursor пересчётом для raycast / marquee / picking, общий `resolveRMBOrder` для 3D-RMB и Map-RMB, selection / hover sync между панелями — ✅
- Orders foundation (Phase 11): `Order` как отдельная ECS-сущность (Order + OrderKind + OrderState + OrderOwner + OrderTarget + OrderChain + OrderIssuedAt + OrderProgress + опциональные OrderParamFacing / OrderParamPatrol), `OrderQueueHead` на Squad, `OrderResolverSystem` с lifecycle Issued→InProgress→Completed/Cancelled/Failed + per-kind completion, 5 типов в MVP (MoveTo / Garrison / OccupyTrench / DefendPosition / Patrol), `HitTester` + `resolveTargetIntoOrder` (Building → Garrison, Trench → OccupyTrench, terrain → MoveTo), pie menu (RMB-hold > 200 ms с drag-disambig от camera-orbit), Shift+RMB append через `OrderChain.Next`, multi-squad RMB через `groupSelectionByOwner` (закрывает ISSUES #3 — больше не вырываем юнитов из ростера), order markers на 2D-карте (line + per-kind icon), Inspector show head + до 2 queued orders, LOD-fix для длинных путей (UnitMovementSystem/FormationSystem/SquadMacroPathSystem Dormant tier enabled — закрывает ISSUES #4), map squad-marker smoothing через world-space lerp (закрывает ISSUES #1) — ✅
- Unit roles (Phase 12): `UnitRole` component + `UnitRoleKind` enum (10 ролей с zero-value Rifleman fallback), `RoleService.AssignRole` с per-role primary weapon (AK47/PKM/SVD/RPG7/GP25) + secondary gear (Radio/Medkit/Spade marker'ы или Makarov), 7 `SquadTemplate` шаблонов и `SquadService.CreateFromTemplate` с unitFactory callback, 3D cap-cube + screen-projected ShortLabel pill, Inspector role header + tinted roster rows с chip'ами, map commander ShortLabel внутри squad marker'а (radius 7→9), реальный `hasRadiomanInRoster` через Equipment.Secondary→Radio check — ✅
- Combat core (Phase 14): `HP` + `Faction` per-Unit (HP per-role 90/100/110), `DamageService` (Apply / ApplyDeath despawn), `WeaponSystem` (parallel snapshot → raycast → serial apply): per-shot dispersion-jittered raycast, wall LOS, friendly fire включён, RoF cooldown через `LastFiredAt`, RoE gate (`shouldFire`: HoldFire/ReturnFire/FreeFire + FireOnInf + fire-while-moving требует AttackMove flag). `OrderKindAttackTarget` (RMB на enemy → HitUnit) + `OrderKindSuppressFire` (pie menu 6th segment "Suppress" → timer 30s). `SuppressionPropagation` (impact 5m, hitMul × falloff, ThreatDir, decay 0.1/s) + `ThreatSource` short-lived entity per-shot + `ThreatDecaySystem`. `components.VisualEvents` resource → 3D `drawTracers` + `drawImpacts` (fade alpha по age). HP bar над cap'ом (mirror Stamina, +0.50m). Squad-entity Faction → split-палитры `squadColorFor` (player blue/green, enemy red/orange). Test scene: 3 player squads + 1 enemy MotorRifle с DefendPosition. — ✅
- Combat hardening (Phase 14.6): crash-on-death fix через `DamageService.sweepAwareness` + defensive alive-checks в `WeaponSystem.pickTarget` / `OrderResolverSystem.advanceOutOfRange` / `resolveTargetPos`; building no-walkthrough fix через 4-слой стек (`applyNavBuildings` stamp NavInBuilding bit'а в Pass 1 spatial_bake; `NavService.cellAt` reject surface inside-building; `FormationSystem.clampSlotXZ` spiral search для slot target; `UnitMovementSystem.reflectAgainstWalls` per-tick velocity reflection); Garrison completion через `CompletionEveryMemberOnFloor` arm (inside/alive fraction в `OrderProgress.Value`). — ✅
- Tactical AI Wave 2 (Phase 17): `Threat{Total, Suppression, ShotsFired, Endangered, Injury, State}` + `DangerBuffer[8]` ring buffer + `dangerSpecs` routing table; `ThreatSystem` (pure-data `tickThreat` + per-channel decay rates); `MicroPath{Waypoints[16], Head, Count, Dirty, GoalSnap, ReplanAt}` per-unit + `MicroPathSystem` (arrival pop / goal-drift / stuck detect / 16-replan-per-tick budget); `StanceControllerSystem` (Threat.State → Stance band с LockUntil + StanceOverride marker); FormationSystem funnel-hack удалён, retarget-in-place + MicroPath.Dirty pattern; `SurvivalInstinct.pickCover` rewrite на approach-cone (quality\*100 + facing\*20 - dist\*5 - anglePenalty\*30 - capacityPenalty\*50) + persistent `occupancyClaim`; combat-move в UnitMovement (Motion split на Yaw/VelocityYaw, turn-then-run gate при > 90°); `gen/buildings` расширен до 5 storey cascade + `Wings []WingSpec` + `DoorSides []uint8` + `GenerateOffice` / `GenerateCompound` templates. — ✅
- Архитектурный slim (post-17): новый `gen/` layer (canonical pure-data generator output — `gen/buildings/` с `GenerateHouse/Office/Compound`, `gen/props/` с `PropPlacement` + `AlongPolyline` / `Composite` primitives для future fence/lamp); `entities/UnitFactory` bundle (11 unit Map handles + `Spawn(pos)` — заменил inline-closure на 16 строк в main.go); `ui/InspectorMaps` + `NewInspectorMaps(world)` (33 Map handle бандл, embedded в `InspectorCtx` — Inspector литерал в main.go shrank ~40 строк → ~12). Monster-files порезаны по sibling-файлам в одном пакете (метод split, не публичный API): `spatial_bake.go` 1767→154+507+603+229+308, `weapon.go` 1134→552+167+318+144, `ui/inspector.go` 944→357+151+175+221+85, `render_world.go` 820→156+179+361+164, `squad_service.go` 808→172+254+157+253, `order_resolver.go` 777→301+234+249, `unit_movement.go` 742→316+334+116, `survival_instinct.go` 645→419+148+104. Main.go всё ещё ~2210 (отдельный refactor). Build/vet/tests green. — ✅
- Building interactions (Phase 17.6): `OrderKindOccupyBuilding` (новый default для RMB-tap на здании — equal-spread по этажам через NavGrid, не window-attach как Garrison); `OrderKindClearBuilding` с auto-chain в OccupyBuilding на complete; `OrderParamEngagementOverride` (per-order RoE swap для «Hidden position» — Crouch + Walk + Quiet + HoldFire); MMB orbit вместо RMB (RMB освобождён под popup); `ui.ContextMenu` generic popup-инфраструктура (секции / items / tooltip / single-gesture + two-action commit); building popup из 6 пунктов (Attack: Clear / Suppress-disabled; Interact: Garrison / Hidden / Occupy L0..LN); pie menu deprecated в коде (`ui/pie_menu.go` остался без callers); 3D order markers per Order для selected squads (cube + connector lines, depth-test off, squadColor tint); per-floor outline под cursor через ray-vs-Level.AABB pick; popup-hover ghost swap (window/floor/single-floor in single-floor); RMB-drag = facing-drag всегда (без pie-mediation); building terrain leveling под все surface buildings (`Stamper.LevelTo` extracted из `RectCut`, cosine skirt снаружи — 1 m в 17.6, расширена до 4 m в 2026-06 movement-фиксах, 5 cm Z-offset против Z-fighting). — ✅
- Tactical AI Wave 3 (Phase 17.8 + 17.9 hardening): Utility evaluator (modes + hysteresis + reason text), ORCA/RVO local avoidance, slot-aware MicroPath (FormationSystem пишет slot target, pathfinder обходит сам), multi-section building pathing (level-junction TransitionEdges), replan-on-stall, Inspector reason row. 17.9 fix-set: escape additive, NavInBuilding door-outside clear, junction cost 1→10, floor-centre AABB convention, level wall inflate. Suite 9/10 PASS (aggregate 79/80) — ✅
- UI L4 core (Phase 18, core): floating panels (title-drag, resize с 4 сторон/углов, chevron-swap контента, полный input-shield), workspace merge/split через chevron menu (Float pane / Close pane), Formation editor (`E`, floating + leaf, presets), Timeline panel. Undo/redo и layout presets — в хвостах (см. TAILS) — ✅
- FoW + Symbology + Sensors (Phase 18.5): `ContactSystem` (замена VisionSystem — детерминированные sensor-каналы `Sensors`: effective-range × Falloff × FacingProfile × concealment), `Contact` entities (fade по `ContactAgeAlpha`, не auto-delete, ручное удаление через RMB ctx-menu), `ContactRegistry`, APP-6 симвология на 2D-карте, `LevelVisibility` (пофлорный FoW), EventLog — ✅
- Movement invariants (2026-06, коммит e7a2050): 19/19 ai_* suite. Leveling skirt 4 m, stair ramps в GroundStick (ramp Y в closest-Y pool), Y-band arrival/completion (1.6 m) для этажных целей, gate-waypoints (`GateMask` — пересечение плоскости проёма, не радиус), door funnel в reflectAgainstWalls, storey-aware `resolveNode` (ближайший пол по Y), `nearestWalkable` recovery для зажатых толпой, progress-based stuck detect, наружные двери у всех крыльев compound'а, ai_main_* сцены на реальных данных главной карты — ✅
- Гигиена WS-A (2026-07-03): vision.go / pie_menu.go / .claude/group удалены (+ мёртвый `components.Vision`, pie-поля из `OrderKindSpec`), `ContactAgeAlpha` дедуплицирован в components, ContactSystem 1× clock (Active-only тик), LevelVisibility faction-гейт (враг больше не расфоговывает здания), debug-логи за env `RTS_DEBUG`, dt clamp 100 ms в `core.App.Tick` (ISSUES #2 закрыт) — ✅
- Детерминизм WS-B (2026-07-03): M1 ORCA-снапшот (data race доказан `-race` до фикса, 0 после; SpatialEntry несёт Vel/Radius), M2 детерминированный порядок (contact-мерж per-worker вместо mutex, BFS-сиды сортированы), M3 единые часы (`ctx.SimNow`/`TickIndex`, снесено 14 приватных аккумуляторов — попутно починены 2×-часы у circle_patrol/micro_path/stance_controller/threat), M4 fixed timestep 60 Hz (`App.Advance`, TimeScale = целых тиков/кадр, orbit frame-guard), M5 replay-harness (`-replay-hash`, FNV-1a каждые 100 тиков, `scripts/replay_gate.sh`; door_south + main_m1 двойные прогоны побайтово идентичны) — ✅

**Следующий шаг:** мини-трек «Карты» закрыт 2026-07-03 обеими частями: **PHASE-VISION** (terrain-LOS в детекте и огне, SampleHeight + фикс «юнит в траншее», общее зрение отряда Direct/Shared; сцены ai_los_open — contacts=1/direct=1/shared=7 — и ai_los_defilade; гейт 21/21 PASS+HASH) и **PHASE-MAPS** (`systems.TerrainParams` + world manifest `maps/<name>.json` + `-map=` + per-map SaveDir; эталоны flat/hills/mountains/valley; contactCap 128 + LRU — WS-E ш.2 закрыт). **PHASE-DETECTION2** реализована 2026-07-05 (шкала заметности: Detectability-метр, motion-mul, exposure-бар, сцена ai_los_creep; порог 0.5 заменён rate-кривой — дальность тянется к номиналу). **18.9 Save/Load закрыт 2026-07-07** (WS-F: full-pool snapshot через Ark DumpEntities/LoadEntities, remap-таблицы не существует; гейты `scripts/saveload_gate.sh` 4/4 SAVELOAD OK + replay 22/22 без регрессий; F5/F9 quicksave; symbols.json закрыл deferred 18.5; классы багов непрерывности — PHASE-18.9.md). **LOS-превью реализовано 2026-07-19** (PHASE-LOS.md: hold V — веер видимости из точки под курсором тем же кодом, что сим, + кольца сенсора/оружия, 3D + карта; гейт 22/22 без регрессий). **Phase 19 Техника стартовала 2026-07-19** (PHASE-19.md: 4-слойное шасси, kinematic без Jolt, VehicleSpec 5 классов; DP-4 решён = да → Controller≠Faction в M0). **Срез 17.5 «Дороги/реки лентами» вытащен вперёд 2026-07-28 по owner-фидбеку** (PHASE-17.5-ROADS.md): пропы-плашки заменены генератором `gen/ribbons` — сглаженный профиль по рельефу, насыпь геометрией вместо деформации heightmap'а (`RoadCarve` только режет), скруглённые углы внутри nav-коридора, настил моста с фасциями/перилами/опорами и рампами заездов, водная лента по руслу; `RoadSurface` заменил спецкейс мостового настила в GroundStick. Гейты: replay 29/29 PASS+HASH, saveload 10/10 OK. Остальная 17.5 (модели, текстуры, биомы) — на месте в очереди. 17.7 на паузе.

**Порядок дальнейших фаз.** Пройдено: 11.6 → 12 → 13 → 13.5 → 13.6 → 14 → 14.5 → 14.6 → 14.7 → 15 → 16 → 16.5 → 17 → 17.6 → 17.8/17.9 → 18-core → 18.5 → movement-hardening (2026-06) → WS-A гигиена (2026-07) → WS-B (детерминизм) → Карты (мини-трек) → 17.7 (Command Surface) → 18.9 (Save/Load) → 19 (Vehicles M0-M7) → 19.5 (Поведение: движение + Threat 2.0 + position scoring + SquadBrain, `PHASE-19.5.md`). Дальше: **22-lite (скриптовый оперативный ИИ; одноразовый бот v0 на SquadService допустим раньше — параллельно картам; исполнительный слой = SquadBrain из 19.5)** → **17.7 / 17.5 (по мере паузы — Behavior Panel split, Visual fidelity)** → **20 (Aviation + interop)** → **21 (Engineering / Multi-squad / Comms)** → **22 (Strategic AI полный, с WS-G belief store)** → **23 (Content pipeline — OSM/DEM, Bezier editor)** → **24 (Polish + data-residency)**.

**Перетасовка относительно прошлой версии ROADMAP.** (1) Visual fidelity (модели, текстуры, smooth roads) и full UI L4 подтянуты ближе - в 16/17.5/18. Раньше они были в Phase 21/22/24. Причина: после Phase 14 combat core стало видно что игра не выглядит как тактическая (cap-cubes вместо солдат, рваные дороги). Сначала "приличный визуал", потом "глубокий AI". (2) После Phase 16.B.1.d funnel-hack стало видно что юниты ведут себя по-дурацки даже когда визуально приличны — толпятся за кустом, бегут спиной, не ложатся под огнём, втыкаются в стены. Поэтому Phase 17 переехал на "Tactical AI Wave 2", а старый "Visual fidelity 1" сдвинут в Phase 17.5 (визуальный pass осмыслен только когда поведение не позорное). Vehicles (бывшая 16) сдвинуты в 19. Strategic AI (бывшая 23) и Content pipeline (бывшая 24) - без изменений по логике. (3) **2026-05-23**: после Phase 17 closure стало видно что управление зданиями нагружено (default ПКМ-tap = Garrison автоматом по окнам — не всегда то, что нужно игроку) и Inspector переполнен standing rules секциями (Movement / Engagement / Behavior / Doctrine / Autonomy + скролл). Поэтому перед Visual fidelity вставили две UI-фазы: **Phase 17.6** (контроль и здания — popup-меню вместо pie, OccupyBuilding kind, MMB orbit, 3D order markers) и **Phase 17.7** (Behavior Panel split — вынос standing rules в отдельный widget по паттерну Formation editor). 17.5 (Visual fidelity) сдвигается после 17.7 — визуал делать на финальной semantics приказов и UI разделения, не на промежуточной. Pie menu полностью убирается из RMB-flow в 17.6; код остаётся в `ui/pie_menu.go` deprecated (примитивы могут пригодиться для радиальных future-кейсов). (4) **2026-05-23 (после 17.6 closure)**: тестирование Phase 17.6 показало что юниты бьются в стены, не обходят препятствия, формация ломается на узких проходах, в многосекционных зданиях squad упирается в первую секцию. Phase 17 (Wave 2) дал per-unit infrastructure (MicroPath / Threat / Cover 2.0), но не финализировал поведение до состояния «отряд как серьёзная единица». Phase 17.8 вставлена СРАЗУ после 17.6 (перед 17.7 / 17.5) — серьёзная переработка tactical AI: Utility evaluator + RVO/ORCA local avoidance + slot-aware MicroPath + multi-section building pathing. Без этого Fog of War (Phase 18+ когда landed) превратится в frustrating game — игрок не видит часть карты, доверяет AI отыграть, а AI глупый. 17.8 — критический путь перед FoW. (5) **2026-07-03**: после закрытия 18.5 и июньского movement-hardening курс скорректирован по итогам аудита 2026-06-12 (`REFACTOR-PLAN.md`): вместо немедленных 17.7/17.5 — сначала **WS-B** (детерминизм — предусловие Jolt-физики Phase 19 и стенда ИИ), затем **карты** (manifest + terrain-LOS + эталоны от равнины до гор), затем **техника (19)** и **скриптовый оперативный ИИ (22-lite)**. Мотив: цель «ИИ, дающий отпор игроку» упирается в детерминированную быструю симуляцию, мир вне стрим-окна камеры, per-faction belief (WS-G) и командную воронку UICommand (WS-C) — это тот же каркас, что и у структурного рефакторинга; фичи и оздоровление совпали в один трек. 17.7/17.5 вернутся после.

**Сквозной документ управления:** `COMMAND-MODEL.md` фиксирует таксономию команд (Orders / Standing rules / Per-weapon intent), order graph + barriers, weapon-bar UX, ghost-preview, реактивное поведение и notification-system. Phase 13 / 13.5 / 14 / 15 / 17 / 19 / 21 ссылаются на него за деталями.

---

## Хвосты (TAILS)

Незакрытые остатки закрытых фаз и их адресаты. Хвост не блокирует следующую фазу, но должен иметь адрес — иначе он теряется.

- **Undo/Redo плана (Phase 18)** → `PlanOp{Apply, Inverse}` поверх UICommand-воронки — REFACTOR-PLAN WS-C, «хвост 18».
- **Полный tree-of-splits drag-dock + layout presets (Phase 18)** → Phase 24 (или по боли; floating + chevron-swap закрыли основную потребность).
- **Симвология persistence (18.5 deferred)** → `save/symbols.json` в мини-фазе 18.9 (WS-F).
- **Per-faction belief store (18.5 deferred)** → WS-G — каркас к старту скриптового ИИ; ключ реестра `(faction, tracked)` закладывается сразу.
- **Terrain-LOS** (рельеф не блокирует ни видимость `ContactSystem`, ни огонь `WeaponSystem` — LOS только по стенам) → мини-трек «Карты»; на горных картах это не полировка, а блокер FoW.
- **Suppress fire в building popup (disabled в 17.6)** → Phase 21 (fire support pass).
- **AttackMove × HoldFire — беззвучный конфликт RoE** (Alt+RMB блокируется без индикации) → закрыт в Phase 15: AT-пилюля со strike-through + предупреждающая подстрока в order-row.
- **`AttackTarget` не наводит огонь + приказы только отрядные** (ISSUES #28, вскрыто 2026-08-02) → **Phase 21**, это фундамент под per-weapon intent (17.7 M5, COMMAND-MODEL §5). Нужны: (1) приказ, адресуемый юниту, а не только отряду — сейчас `dispatchOrder` шлёт солистам `pushSoloMove`, и одиночная машина приказ атаковать не держит; (2) `pickTarget` / `weapon_gunner` должны предпочитать цель головного приказа, пока она видима и поражаема, с откатом на авто-выбор; (3) поверх этого — `OrderParamWeaponPref` + aim mode из карточки squad bar (UI-сторона уже готова: карточка знает свои стволы, клик ходит через request-паттерн).
- **Stance hotkeys Z/X/C** (заняты crater / cover-overlay) → Phase 21.
- **Garrison LODAnchor-пин мёртв** (order_resolver вешает якорь, streaming игнорирует; пин течёт по завершении приказа) → WS-E ш.3-4 (N-якорный стриминг, до Phase 22).
- **GroundStick на Modified-рельефе** (юнит в траншее стоит на до-срезной procgen-высоте) → `SampleHeight` в data-residency (WS-E ш.6, Phase 24, DP-7).
- **`OrderKindDestroyBuilding` / `BurnBuilding`** (Phase 16 deferred kinds) → Phase 21 Engineering.
- **Multi-owner / joint Orders** → design-spike Phase 20 (`COMMAND-MODEL.md` §3).
- **Дебаг-оверлеи NavGrid / CoverMap / cover-slots / FloorNavGrid** — hold-хоткеи N/C/V/F/Y сняты при переездах ввода; примитивы в `render_overlays.go` живы → ребинд по мере надобности (WS-D ш.4 boot_render_handles — удобный момент).

---

## Принятые архитектурные решения

Эти решения сквозные. Подробности — `GAMEDESIGN.md`. Здесь только список того, что зафиксировано.

1. **Гибридный терраин.** Heightmap-чанки + локальные stamp'ы (`Stamper`). SDF/воксельная деформация — `🗄️ заморожено`.
2. **Здания живут в одной системе координат с поверхностью (Layer 0).** Окно — LOS-прозрачная стена, дверь — точка traversal, бункер — заглублённое здание. Никаких портал-теле portations.
3. **Все статические объекты — `Prop`** с `PropTypeRegistry` для геймплейных атрибутов.
4. **Координаты: `ChunkCoord + Local Vec3`.** Никакого глобального float32.
5. **Чанки 64×64 м, Resolution=65** (1 м на сэмпл).
6. **Бесшовность чанков** через детерминированный procgen от `(seed, ChunkCoord)`.
7. **Дороги — `RoadGraph` singleton resource**. Влияют на heightmap (flatten), nav (road-bias), рендер.
8. **Два штамма стриминга**: `TerrainStreamingSystem` (дистанционный) + `StreamingSystem` (графовый, для smart-spaces).
9. **Иерархический ИИ**: Стратегический (10-30 с) → Оперативный (1-5 с) → Тактический (каждый тик). Нейросеть только на верхних уровнях.
10. **Юнит ≠ один компонент**: Spatial / Senso-Cognitive / Execution / Equipment. Оружие — отдельные сущности с `OwnedBy`.
11. **Отряд = невидимая ECS-сущность** с `CommandRoster`/`FormationData`/`MacroPath`/`RadioNetwork`, не группа выделенная рамкой.
12. **Smart Objects.** Пропсы и здания эмитят cover slots и shooting points.
13. **Ark ECS-паттерны**: маркер-компоненты как state, `ecs.Resource[T]` для singleton, `InitUI` для построения Filter/Map handles, deferred archetype changes (batch и apply после Filter Query).
14. **Order — отдельная ECS-сущность с lifecycle** (Issued/InProgress/Blocked/Completed/Cancelled/Failed), не поле Squad'а. Залочено в Phase 11.
15. **Equal-dual UI** (3D + 2D map) с L4-docking как цель, реализация поэтапно (L1 → L3 → L4-split → L4-polish).
16. **Карта — 2D-абстрактное представление** через иконки и стилизованную геометрию, **не top-down 3D-рендер**. Pre-baked underlay при старте мира.
17. **Real-time continuous + pause + speed compression** (1×/2×/4×/8×). Без WeGo.
18. **Time-slicing AI** через hash-bucket distribution (entity.ID % bucketCount). Дорогие системы распределяют entity по bucket'ам, обработка цикла за 10-секундное окно.
19. **TacticalOverride marker pattern**: AI-preемптеры ставят маркер на юнит, FormationSystem фильтрует `Without[TacticalOverride]`. Тот же паттерн для `Reloading`, `MountingVehicle`, etc.
20. **Generators в `gen/`**, не в `systems/`. Чистая логика без ECS: вход = параметры, выход = типизированный план (`BuildingPlan`, `[]PropPlacement`). Spawner — отдельный слой, читает план и материализует entities. Тестируется в изоляции, переиспользуется из sandbox'ов. `gen/buildings/` (дома/офисы/compounds), `gen/props/` (`PropPlacement` + `AlongPolyline` / `Composite` primitives).
21. **Spawn-фабрики в `entities/`**, не в main.go. Каждая фабрика бандлит Map handles + init defaults для одной archetype'а; `NewXxxFactory(world)` строит handles один раз, `Spawn(...)` использует их без повторного `ecs.NewMap`. Сейчас `UnitFactory`; будущие — `BuildingRootFactory`, `WeaponFactory`. Read-handles экспортируются так что Inspector / input layer не дублируют `NewMap` calls.
22. **Multi-file systems**: когда система переваливает за ~700 строк И есть ≥2 независимые фазы / domain slice, разносим в `XXX.go` orchestrator + sibling файлы `XXX_phase.go` в том же пакете. Go биндит методы поверх файлов; публичный API не меняется. Применено к `spatial_bake` (4 passes), `weapon` (snapshot/resolve/postpass), `unit_movement` (step/walls), `order_resolver` (completion/target), `survival_instinct` (scramble/cover), `squad_service` (membership/template/orders), `ui/inspector` (unit/squad/orders/override). Не разносим монолитную функцию по двум файлам — это хуже LOC-цены.

---

## Закрытые фазы

### Phase 0 — Координатный рефакторинг ✅
Перевод с `Position3D{X,Y,Z float32}` на `WorldPos{Chunk, Local}`. Архив: `.claude/old/PHASE-0-1.md`.

### Phase 1 — Terrain MVP ✅
Открытый бесшовный мир из heightmap-чанков, procgen, LOD на чанках. Архив: `.claude/old/PHASE-0-1.md`.

### Phase 2 — Persistence чанков ✅
Сохранение модифицированных чанков на диск. Архив: `.claude/old/PHASE-2.md`.

### Phase 3 — Props ✅
`Prop` + `PropTypeRegistry`, биомные маски, реки как polyline-cut + water-props. Архив: `.claude/old/PHASE-3.md`.

### Phase 4 — Дороги ✅
`RoadGraph` singleton, `PreprocessRoadGraph`, road/junction props, мосты, road-clearance. Архив: `.claude/old/PHASE-4.md`.

### Phase 5 — Здания ✅
`BuildingPlanList`, BuildingSystem, стены/двери/окна/полы/лестницы, окопы. Архив: `.claude/old/PHASE-5.md`.

### Phase 6 — Spatial intelligence ✅
NavGrid + CoverMap per-chunk, road-bias, cover slots для пропов / окон / углов стен. Архив: `.claude/old/PHASE-6.md`.

### Phase 7 — Базовые юниты ✅
12 placeholder-юнитов, multi-floor навигация, separation steering, vision, selection + ПКМ. Архив: `.claude/old/PHASE-7.md`.

### Phase 7.5 — Observability ✅
Profiler, HUD, JSONL trace за build-tag'ом. Архив: `.claude/old/PHASE-7.5.md`.

### Phase 9 — Squads + Formations ✅
Squad как ECS-сущность, FormationSystem, SquadMacroPathSystem, cohesion, selection-aware orders, hotkeys, render. Архив: `.claude/old/PHASE-9.md`.

### Phase 10 — Interface foundation ✅
`ui.PanelManager` + 4-панельный L1 layout (3D / Map / Inspector / Time), Tab swap Field ↔ Command preset, 3D-сцена в `rl.RenderTexture2D`, 2D-карта с pre-baked greyscale hill-shade underlay (2 km × 2 km @ 4 m/px), `MapCamera` с pan / zoom (cursor-relative wheel), per-frame `panelMgr.FocusedAt` mouse routing с панель-локальным cursor → `GetScreenToWorldRayEx` / `GetWorldToScreenEx` для raycast / marquee / picking, общий `resolveRMBOrder` (3D-RMB и Map-RMB сводятся к одному вызову), Inspector со scенариями empty / single-unit / single-squad / multi-select + hover-highlight roster, time controls (Space pause toggle / +/- speed cycle 1→2→4→8) с `App.TimeScale` (паузная игра ⇒ scaled-dt = 0, profiler timing остаётся в реальном времени), selection / hover sync между 3D и картой, opt-in roads / rivers / buildings overlay карты на hold-G. Архив: `.claude/old/PHASE-10.md`.

### Phase 11 — Orders foundation ✅
`Order` ECS-сущность с lifecycle (Issued / InProgress / Blocked / Completed / Cancelled / Failed) + `OrderQueueHead.First` на Squad как primary state (MacroPath derived/cached). 5 типов в MVP: MoveTo / Garrison / OccupyTrench / DefendPosition / Patrol. `OrderResolverSystem` для lifecycle transitions + per-kind completion. `HitTester` + `resolveTargetIntoOrder` для контекстного выбора kind (Building → Garrison, Trench → OccupyTrench, terrain → MoveTo). `TrenchRoot{Index}` entities для reverse-индекса к polyline data. Pie menu (RMB-hold > 200 ms с drag-disambig от camera-orbit). Shift+RMB append через `OrderChain.Next`. Multi-squad RMB → каждый squad получает свой Order через `groupSelectionByOwner` (закрывает ISSUES #3). Order markers на 2D-карте (line + per-kind icon: dot/square/triangle/diamond/circle-arrow). Inspector show head + queued orders. ISSUES #4 закрыт через UnitMovementSystem/FormationSystem/SquadMacroPathSystem Dormant tier @ 500ms/1s/5s. ISSUES #1 закрыт через world-space lerp squad-маркеров на карте. Архив: `.claude/old/PHASE-11.md`.

**Замечание о Phase 8.** В предыдущей версии ROADMAP'а Phase 8 была «Дорожная техника». В новом порядке она переехала на Phase 16 (после combat и tactical AI), потому что vehicles без тактического контекста и базовых combat-механик дают изолированную фичу без геймплейной отдачи. Старая нумерация (Phase 8) **остаётся незаполненной в новой системе** — мы её не используем, чтобы не путаться.

---

### Phase 11.5 — Architectural refactor ✅

Переоснащение симуляции: universal-simulation вместо binary LOD-gating'а + worker pool + time-slicing chassis (bucketCount=1, активация в Phase 14). `core.WorkerPool` (`runtime.NumCPU()` default, `-workers=N` flag override, `Resize()` заглушкой, `Stop()` на shutdown). `core.ShouldProcessBucket(entityID, bucketCount, frameIdx)` + `App.FrameIndex()` + `UpdateContext.FrameIndex`. Drop LOD-tier-gating'а из 5 систем (UnitMovement / Vision / Formation / SquadMacroPath / OrderResolver) — один Filter, один Update pass per tick. LOD-маркеры удалены с юнитов (`LODSystem` исключает Unit-архетип). Параллелизация hot-path систем через `WorkerPool.ParallelFor` (snapshot → parallel step → no/serial apply). `MapMarkerCacheSystem` @ 250 мс + `components.MapMarkerCache` resource, map renderer fallback'ит на live SquadCenter при cache-miss. Cohesion fix: `CohesionLeashCoeff = 8.0`, `cohesionEjectionEnabled = false` (вернётся в Phase 15 как часть doctrines). ISSUES #4 / #5 / #6 — closed.

---

### Phase 11.6 — Micro-polish after 11.5 ✅

Точечная полировка после Phase 11.5. UnitMovement / Vision / Formation / SquadMacroPath держат snapshot буферы как struct fields с reset через `buf[:0]` / `clear(map)` — per-tick `make()` allocations убраны. `core.WorkerPool` получил `SerialThresholdHint = 64` (small-N идёт serial inline в caller goroutine — на текущей сцене 12 юнитов / 3 squad'а ParallelFor-overhead исчезает). Новый `WorkerPool.ParallelForIndexed(total, fn(chunkIdx,start,end))` API; FormationSystem использует его с per-worker `workerLeaveBufs[chunkIdx]` для preemptive race-safety когда Phase 15 включит обратно `cohesionEjectionEnabled`. `core/worker_pool_test.go` пополнен `TestParallelForRaceSafe` / `TestParallelForIndexedPerWorkerBuf` / `TestParallelForSerialFallback`; `go test -race ./core/` clean. `CLAUDE.md` "Adding a new system" получил шестой пункт про race-detector workflow и snapshot-buffer pattern.

---

### Phase 12 — Unit roles ✅

`UnitRole` component на каждом юните + `UnitRoleKind` enum (10 ролей: Rifleman / Leader / MachineGunner / Grenadier / Sniper / ATGunner / Medic / RadioOperator / Engineer / DemoMan; `RoleRifleman = 0` для zero-value safety) с `String()` / `ShortLabel()` / `RoleColor()` helper'ами. `SquadTemplate` enum + `TemplateRoster(t)` для 7 шаблонов (LightInfantry / MotorRifle / NATOInfantry / Recon / Engineering / ATTeam / MGTeam). `RoleService` с `AssignRole(unit, role)` — spawn'ит Primary weapon entity (AK47 / PKM / SVD / RPG7 / GP25 placeholder stats) + Secondary gear (Radio / Medkit / Spade marker'ы или Makarov sidearm) с правильным `OwnedBy`. `SquadService.CreateFromTemplate(template, pos, formation, roleService, unitFactory)` спавнит юнитов через callback из main.go + AssignRole каждому + CreateFromUnits. Test scene переписана на 3 starter squads (LightInfantry / MGTeam / ATTeam) с разными role distribution'ами. 3D-рендер: cap-cube на каждом юните (`RoleColor`-tinted, leader cap выше) + 2D screen-projected ShortLabel pill через `GetWorldToScreenEx`. Inspector: single-unit view имеет role header (chip + name), roster rows tinted by role с ShortLabel chip'ом. Map: squad marker увеличен 7 → 9 px, внутри commander's ShortLabel с contrast-text. `SquadService.hasRadiomanInRoster` теперь реально проверяет Equipment.Secondary → Radio component (Phase 20 будет читать `RadioNetwork.HasRadioman`).

### Phase 13 — Movement & Engagement standing rules ✅

Три standing-rule компонента на Squad — `MovementProfile` (Pace/Stance/Posture/PathStyle), `EngagementRules` (Mode + 4 target toggles, Standoff/Sector — placeholder для Phase 14), `BehaviorRules` (4 toggle'я + SuppressionThreshold — scaffold для Phase 15 SurvivalInstinct/ScatterProtocol). `Stamina` per-Unit с per-role MaxLevel modifier'ом (MG/AT 0.7×, Engineer/Demo 0.8×, Radio 0.85×). `RoleService.AssignRole` идемпотентно инсталлирует Stamina (preserve'ит Current на reassign). `SquadService.CreateFromTemplate` агрегирует squad-level defaults от leader-роли + template-specific override (Recon → CoverSeek+Quiet+HoldFire, AT → HoldFire+Arm only, MG → Crouch, Engineering → ReturnFire). UnitMovementSystem читает effective MovementProfile (OrderParam override > squad standing > default) c per-tick stamina drain/regen, `StaminaExhausted` marker через per-worker buffers + serial post-pass, stance auto-transition к standing default'у. NavCell получает `CoverDistance uint8` byte (CoverDistanceFar=255 default); SpatialBakeSystem Pass 2 запекает distance-to-nearest-cover-slot в 9-chunk window. NavService.FindPath читает PathStyle через NavOpts; styleCellCost применяет таблицу модификаторов (Direct ×1.0 / RoadPrefer road×0.5 off-road×1.5 / RoadAvoid road×2.0 cover-tagged×0.8 / CoverSeek near-cover×0.7). SquadMacroPathSystem пробрасывает squad's PathStyle (или OrderParamMovementProfile override). `OrderParamMovementProfile` опциональный компонент на Order — Ctrl+RMB прикручивает PresetStealth, Double-RMB (300ms window) — PresetSprint, Alt+RMB — `OrderParamAttackMove` marker (Phase 14 scaffold). Inspector single-squad view получил три новые секции: Movement (6 preset chips + 4 cyclic field buttons Pace/Stance/Posture/PathStyle + Stamina avg bar), Engagement (3 Mode chips + 4 target toggles), Behavior (4 toggle rows + SuppressionThreshold ±-buttons). Single-unit Stamina row. Hotkey'и `[`/`]` cycle MovementProfile presets, `'` toggle Posture. Per-unit Stamina bar над cap'ом в 3D при <80% (green/yellow/red zones). Стенс-хоткеи (Z/X/C) отложены до Phase 21 из-за конфликта с crater stamp / CoverMap overlay. `BehaviorRules` — scaffold-only, читатели в Phase 15 (formal contract в COMMAND-MODEL §7).

---

### Phase 13.6 — Ghost preview & facing-drag ✅

`systems.FormationOffset` exported из package systems (раньше `formationOffset` lowercase) — позволяет main.go / ghost render reuse the same slot layout, который FormationSystem использует. `drawGhostUnit(pos, stance, alpha)` + `drawGhostArc(center, facingYaw, halfAngleRad, length, color)` рендер-хелперы в `render_world.go` — translucent body cube без role-cap (neutral grey-white alpha=80) и wedge-sector (filled fan + outline). `ghost.go` (new file, package main): `ghostContext` бандл-хендлы для render pass (world / posMap / rosterMap / formationDataMap / stanceMap / movementProfileMap / squadMemberMap / hitTester / buildingIndex / wallMap / windowMap / trenches / trenchRootMap). `drawSelectionGhost(ghostCtx, selected, cursorOver3D, target, ok, dragFacing, pieHovering)` — single render-pass entry с тремя input axes: cursor target, drag yaw override, pie hover kind override. Per-kind placement: `HitBuilding` → first-N windows greedy (walk BuildingChildIndex children, filter `Window`-bearing walls, compute opening centre as `WorldPos + (sin(Yaw), cos(Yaw)) × Length × OpeningCenterT`), `HitTrench` → equal-spaced polyline placement (`total × (i+1)/(count+1)` arc-length param with endpoint inset), `HitTerrain` → standard formation. DefendPosition (pie hover) → formation + sector arc (90° wedge, 8 m radius, squad-color tinted). `primarySquadForGhost` для P9 fallback (multi-squad → first squad). Stance: squad MovementProfile.Stance > commander's Stance > Stand default — ghost crouches when squad has stealth preset.

`PieMenu` extended: `HasSelection` (set by `Begin(origin, target, source, hasSelection)`), `InFacingDrag` bool (set when drag > DragCancelThreshold AND HasSelection — отдаёт RMB-drag в facing-input вместо camera-orbit). `HoveringKind components.OrderKindCode` + `HoveringValid bool` (populated each tick while Active). `PieTickResult.ReleasedAsFacingDrag` + `FacingYaw` — screen-space `atan2(dx, -dy)` mapping (yaw convention matches Motion.Yaw / FormationOffset: 0 = +Z, increases clockwise). `Reset` clears all 4 new fields. main.go gates: ghost rotates live during drag через `pieMenu.InFacingDrag` check + per-frame yaw recompute; ghost reads `pieMenu.HoveringValid+HoveringKind` для preview kind-specific layouts. RMB session ownership через existing `pieMenu.SourcePanel != PanelNone` chain — InFacingDrag keeps SourcePanel set, OrbitInputEnabled stays gated off.

`OrderResolverSystem` reads `OrderParamFacing` on InProgress→Completed transition (any kind — MoveTo, OccupyTrench, Garrison) — `applyArrivedFacing(squad, ord)` walks roster и instant-snaps Motion.Yaw для всех members. DefendPosition pie commit derives facing as `atan2(target - squadCenter)` (hover-style) — Phase 13.6 simple commit без требования drag. Facing-drag commit path: `resolveRMBOrderWithParams` (new variant accepts pre-formed `systems.OrderParams`) с HasFacing=true + FacingYawRad=drag-derived. `resolveRMBOrder` (existing entry) теперь wraps это через `applyModifiersToParams(...)`. Per-frame cursor target raycast (`mouseTargetWorldPos`) hoisted before render pass — single per-frame computation used by ghost preview и (in M13.6.4) facing-drag visualisation. Continuous hover preview gated по `focused == ui.Panel3D`; cursor entering Inspector → ghost disappears (P-note: desired UX — no distract while navigating chips).

---

### Phase 13.5 — UI L2 polish ✅

`PanelManager.RightColRatio` / `InspectorRatio` стали mutable struct fields (раньше package consts); `splitGrid` принимает ratios как args. `SplitterID` enum (None/Main/Right) + `SplitterAt(cursor)` hit-test (6 px grab radius). `BeginDrag`/`UpdateDrag`/`EndDrag`/`AbortDrag` state-машина в PanelManager: hover показывает resize-cursor (ResizeEW/ResizeNS), LMB-drag меняет ratio live с min-size constraints (panelMinW=180, panelMinH=100), Tab pre-empts drag через AbortDrag. `ScrollState{OffsetY, ContentHeight}` per-Panel в `Scroll [4]ScrollState`. `DrawScrollbar` / `ScrollbarRect` / `ScrollbarThumbRect` / `ClampScrollOffset` helper'ы в `ui/chrome.go` — 8 px track + thumb с `scrollbarThumbMin=30` минимумом. Inspector рефакторен: subtract OffsetY из initial y, измеряет ContentHeight в конце, передаёт через `InspectorCtx.Scroll`. Все 4 `drawInspector*` helpers возвращают финальный y. Wheel-scroll над Inspector — `wheelScrollSpeed=30 px/tick`; gated по `focused == ui.PanelInspect && !panelMgr.IsDragging()`. MapCamera wheel zoom preserved через mutually-exclusive focused-panel check. Thumb-drag: LMB-press на ScrollbarThumbRect → state-машина `scrollDragging`, cursor delta × (maxOffset / scrollableTrack) → OffsetY. `scrollDragging` гасит chip clicks через `InspectorCtx.LMBPressed`. `save/layout.json` (versioned schema, atomic write через .tmp + os.Rename) — `loadLayout` перед первым `Recompute`, `saveLayout` на `EndDrag` returning changed=true + defer на shutdown как fail-safe. `clampRatio` защита от out-of-range hand-edits. Selection / marquee gated на `!panelMgr.IsDragging()` чтобы splitter drag не triggered selection click.

---

### Phase 14 — Combat core ✅

`HP` + `Faction` per-Unit (HP per-role 90/100/110 в `HPMaxForRole`, Faction `Player`/`EnemyRed`). `DamageService` (service object, не `core.System`): `Apply(unit, dmg)` decrement → lethal-check, `ApplyDeath` clean despawn (Leave + destroy Equipment.Primary/Secondary + RemoveEntity). `WeaponSystem` parallel-aware: serial snapshot (target candidates + walls по чанкам + firing-eligible seers) → per-shot parallel raycast (dispersion-jittered aim через детерминированный SplitMix64, wall LOS через `segmentToWallsT`, unit-vs-ray в 3×3 chunk window — friendly fire включён по дизайну) → serial post-pass (DamageService.Apply + tracer/impact в `components.VisualEvents`, ThreatSource spawn, propagateSuppression). Per-weapon `LastFiredAt` + `Dispersion` (radians) — RoF cooldown gate + dispersion-jitter; effective dispersion масштабируется на movingFactor + Suppression.Level. `shouldFire` gate реализует RoE (HoldFire silent, ReturnFire/FreeFire allow), FireOnInf target-type, fire-while-moving требует AttackMove flag на active order (HoldFire wins над AttackMove — phase decision Q2). `OrderKindAttackTarget` + `OrderKindSuppressFire` в enum + `OrderParamSuppress{Radius, AmmoCap, StartTime}`; `HitTester` extended на units (1.5 m snap radius, hostile-only через FactionMap) — RMB на enemy unit → AttackTarget; pie menu получил 6-й сегмент "Suppress" → SuppressFire на terrain. `OrderResolverSystem` completion: AttackTarget = target dead/missing, SuppressFire = timer >30s. SquadMacroPathSystem short-circuit'ит оба new kind'а (squad стоит). `SuppressionPropagation` пишет первые реальные значения в Suppression.Level (`hitMul * (1 - dist/5)`, ThreatDir = от impact к unit, cap 1.0, decay 0.1/s в начале каждого tick'а WeaponSystem). `ThreatSource` short-lived entity per-shot (Severity = damage/100, TTL 3s); отдельная `ThreatDecaySystem` despawn'ит expired. Visual feedback: `components.VisualEvents` singleton resource (tracer/impact slices с TTL, Decay() per-frame в main render loop), 3D `drawTracers` (line per tracer, fade alpha по age) + `drawImpacts` (sphere 0.1m). HP bar над cap'ом в 3D (50×3 px, +0.50 m above stamina, green→yellow<60%→red<30%, hidden at full HP) — `drawUnitHPBar` mirrors Stamina pattern. Squad entity получает Faction; `squadColor` refactor на entity-aware: split-палитры — player blue/green, enemy red/orange (`squadColorFor`) — для Inspector / MapCtx / ghostContext / drawDefendPositionArc. Inspector single-unit view: HP row + Faction row под Stamina. Per-weapon stats refined per P10 (AK47 28dmg/300m/4rps/0.030, PKM 30/500/8/0.05, SVD 70/600/0.5/0.005, RPG7 200/200/0.1/0.02, GP25 50/150/0.3/0.04, Makarov 18/30/3/0.06). Test scene: 3 player squads (LightInfantry / MGTeam / ATTeam) + 1 enemy MotorRifle на (5, -90) с DefendPosition. Pipeline: `vision → weapon → threat_decay → order_resolver → ...`. Сознательно отложено в Phase 14.5: particle system (визуал — DrawLine3D + sphere только), spatial hash, weapon-bar, aim-mode cursor, `OrderParamWeaponPref` reader, map ping / event log, splash damage, armor / penetration / cover-shadow, реактивное поведение (Phase 15 SurvivalInstinct читает Suppression + ThreatSource), wounded state / corpses (Phase 25). UI-индикация конфликта AttackMove+HoldFire (когда squad's RoE затыкает Alt+RMB) — TODO для Phase 21 Inspector polish.

### Phase 14.5 — Architectural polish + particles + spatial hash + spec tables ✅

Архитектурные дельты после Phase 14. Spec table pattern (typed `XxxSpec` array indexed by enum-code, compile-time exhaustiveness) применён к трём enum'ам: `OrderKindSpec` (7 полей: Name / MapIconGlyph / InPieMenu / NeedsEntity / NeedsTerrain / OverridesHoldFire / DrivesMacroPath / Completion+ArrivalRadius+DurationSeconds+MaxOutOfRangeSeconds), `StanceSpec` (Code / Name / MaxSpeed / BodyHeight / DamageMultiplier / TargetCenterY), `WeaponSpec` (Kind / Name / Ammo / RangeM / RoF / Damage / Dispersion / TracerColor / SplashRadius / SplashFalloff). Семь читателей `OrderKindCode` switch'ей мигрированы на чтение полей spec'а; `pie_menu.pieSegments` derived runtime из `InPieMenu + PieSegmentOrder`. `CompletionRuleKind` enum (ArrivalRadius / TargetDeath / Timer / Never / EveryMemberOnFloor) - `OrderResolverSystem.evaluateCompletion` dispatch по `Spec.Completion` с tri-state outcome (Pending / Done / Failed). Issue #9 closed - `OrderKindAttackTarget.OverridesHoldFire = true` override'ит HoldFire RoE для explicit fire orders. Issue #10 closed - `OrderOutOfRangeTracker` per-order tracker copит elapsed когда вся squad'а вне effective weapon range от AttackTarget'а; после `Spec.MaxOutOfRangeSeconds` (8s) order Failed. `core.SpatialHash` (uniform-grid 32m cells, zero-alloc `ForEachInRadius` callback, packed int64 cell keys) - один экземпляр для Units, registered as resource. `SpatialHashRebuildSystem` snapshot'ит Filter2[Unit, WorldPos] каждый тик перед UnitMovement (serial pre-pass). `UnitMovement.step` separation pass мигрирован на `ForEachInRadius` + stale-entity alive-check invariant. `propagateSuppression` мигрирован на тот же хеш (O(N×shots) -> O(impact-radius×shots) - главный perf win). `WeaponSystem.resolveShot` (unit-vs-ray) и `Vision.processVisionSeer` сознательно оставлены на 3×3 chunk-window pattern - они уже chunked, миграция дала бы pillage без visible payoff. Particle system: `components.VisualEvents` resource удалён, заменён на ECS-entity-backed particles. Six ParticleKind (Tracer / Impact / MuzzleFlash / Smoke / Dust / Debris). `ParticleVisual{Kind, Color, Size, SpawnTime, TTL}` + optional `ParticleVel` + optional `ParticleEnd`. `ParticleSystem.Update` (age, integrate Vel*dt, per-kind gravity table, expire) + soft-cap eviction (2000). `SpawnParticleHandles` bundle (SpawnTracer / Impact / MuzzleFlash / Smoke / Dust / Debris helpers). WeaponSystem.serialApply теперь спавнит ECS-entity particles вместо AppendTracer/AppendImpact. `render_world.drawParticles(ParticleRenderCtx, now)` walks Filter, dispatch по `ParticleVisual.Kind`. MuzzleFlash auto-spawn'ится на каждый tracer. Splash damage: RPG7 (3.5m radius, quadratic falloff) и GP25 (2.5m radius, ~1.5 power). `shotWork` carries splashRadius/Falloff из WeaponSpec; resolveShot emits `splashEvent` в `workerSplash` buffer; serial post-pass `applySplashDamage(ev, hash)` walks SpatialHash в радиусе, computes damage с falloff (linear/quadratic), исключает direct-hit target. Per-impact-kind particle bursts: 3 dust на terrain hit, 4 debris на wall hit, 10 debris + 1 smoke cloud на splash impact. `hitKind` (Miss / Terrain / Wall / UnitFlag) carried через `impactSpec.Hit`. Audio scaffolding removed from main.go (placeholder square-wave + SpatialAudioSystem - реальный audio в Phase 25 polish). Сознательно отложено в Phase 14.6 / 15: Garrison routing real fix (`CompletionEveryMemberOnFloor` rule declared but not wired - M14.5.6 unfinished), Inspector weapon-bar + aim-mode + OrderParamWeaponPref reader (M14.5.7/8 - большой UX surface), `exhaustive` linter integration (P2 plan). Two new pipelined systems: `spatial_hash_rebuild` (before unit_movement), `particle` (after weapon). Sandbox: `cmd/particle_sandbox/` standalone main для визуальной валидации ParticleSystem без всей сцены - 1-6 spawn kind, Space full burst, A auto-emit, R reset, RMB orbit. CLAUDE.md / memory обновлены: Spec table pattern + SpatialHash invariants как канонические подходы для будущих фаз. Issue #11 (crash при смерти юнита) и Issue #7 (Garrison no-walkthrough) — обе closed в Phase 14.6.

### Phase 14.6 — Hotfixes (Issue #11 crash + Issue #7 walkthrough + M14.5.6 Garrison) ✅

Hotfix-фаза. Закрыла три блокера combat playtest'а перед Phase 15 без архитектурных дельт.

- **Issue #11 (crash on unit death) — closed.** `DamageService.ApplyDeath` теперь walks `Filter[Awareness]` first и обнуляет каждый `LastSeen` slot указывающий на dying entity (sweepAwareness). Plus defensive alive-checks в hot readers: `WeaponSystem.pickTarget` (first строкой после empty-slot guard), `OrderResolverSystem.advanceOutOfRange` (до posMap.Get target), `OrderResolverSystem.resolveTargetPos` (до switch на kind). SpatialHash callbacks (`applySplashDamage`, `propagateSuppression`) — alive-check уже first-строкой со времён Phase 14.5; подтверждено инспекцией.
- **Issue #7 (building no-walkthrough) — closed (three-layer fix).**
  1. `applyNavBuildings(grid, cc, footprints)` (`systems/spatial_bake.go`) — SpatialBakeSystem Pass 1 в дополнение к road/trench/river/wall raster'у. Для каждой `Building.Footprint` AABB, intersect с chunk → cells whose centre внутри footprint получают `NavCell.Flags |= NavInBuilding`. Cost не трогается (wall raster Cost=0 остаётся authoritative для walls; InBuilding — отдельный bit). Building filter в InitUI.
  2. `NavService.cellAt` для `NodeSurface` — reject NavInBuilding bit'ом (return cell, false). A* gridNeighbours skip эти cells; transitions через `TransitionEdge` (Door / Stairs) — единственный путь в floor-nodes внутри building'а.
  3. `FormationSystem.clampSlotXZ` (`systems/formation.go`) — после compute slot target, spiral search 8 directions × 1/2/3m if cell at slot is NavInBuilding или Cost=0. NavGrid + ChunkIndex handles в InitUI. Reflection-only fallback в UnitMovement step предотвращает edge case'ы (separation push через стену, slot clamp на загружающимся chunk'е).
  4. `reflectAgainstWalls` (`systems/unit_movement.go`) — per-tick XZ ray cast против walls в 3×3 chunk window. Up to 4 reflection passes (corner case). Open doors пропускают через opening range; window opening блокирует (movement-collide независимо от LOS). `colWall` snapshot в Update начале по pos.Chunk; workers read read-only.
- **M14.5.6 wired — Garrison CompletionEveryMemberOnFloor.** `OrderKindSpecs[Garrison].Completion` теперь `CompletionEveryMemberOnFloor` (вместо ArrivalRadius). `evaluateCompletion` arm: `countInsideBuilding(roster, footprint)` walks live roster, для каждого pointInFootprintAABB(memPos) AND `memberOnFloor(memPos)` (Floor plate check + ±1.5m Y proximity, через Filter2[WorldPos,Floor]). `inside == alive` → Done, `alive == 0` → Failed, else Pending. `OrderProgress.Value = inside/alive` — Inspector progress-bar показывает partial entry без отдельного компонента. `updateProgress` skip'ает для Garrison чтобы distance-based fraction не перезаписывал. Старый `OrderKindGarrison` short-circuit в CompletionArrivalRadius arm удалён.
- **Garrison routing fixup (post-M14.6.1 playtest, partial).** Две точки правки чтобы squad начал входить через дверь:
  1. `resolveTargetPos` для Garrison теперь ставит `target.Pos = firstFloorPos(building)` (lowest-Level Floor child из BuildingChildIndex) вместо footprint center в surface coords. `NavService.resolveNode` мапит это в `NodeFloor`, A* находит путь через `TransitionEdge` (Door / Stairs) cross-graph transitions. Fallback на footprint center если children evicted.
  2. `BuildingSystem` спавнит двери в `DoorState=DoorOpen` (раньше `DoorClosed` → cost=0 TransitionEdge → impassable). Player-driven open/close — Phase 24 polish.
  3. `FormationSystem.processSquad` skip'ает `clampSlotXZ` когда `centerTarget` (текущий macro waypoint) — NavInBuilding cell. Outside-of-building behaviour не меняется.

  **Что осталось.** Commander заходит через дверь, но остальные members с slot offset перпендикулярно к стене (line / wedge formation) — bump'аются в стены сбоку от двери, wall reflection ловит, separation force выталкивает обратно. Garrison completion (`inside == alive`) не достигается потому что 3/4 outside. Корень: FormationSystem жёстко пишет slot target = squadCenter + offset без учёта геометрии; каждый member не имеет собственного pathfinding (squad-as-unit principle - см. Phase 14.6 design discussion). **Решение - rally / FormationColumn auto-switch + sliding steering - перенесено в Phase 15.B M15.B.3-5** (это естественный fit с track 15.B "Formation slack"). Phase 14.6 ship'нул foundation (NavInBuilding bit, wall reflection, floor goal routing, open doors); 15.B полирует formation behaviour поверх.

**Pipeline.** Без новых system'ов. Изменения локальны в существующих: spatial_bake (Pass 1 + NavInBuilding stamp), nav_service (cellAt surface reject), formation (slot clamping), unit_movement (wall snapshot + step reflection), damage (Awareness filter + sweepAwareness), order_resolver (Garrison arm + helpers), weapon (pickTarget alive-check).

**Build / vet / race.** `go build ./...` clean; `go vet ./...` clean; `go test -race -count=1 ./core/` 1s green.

### Phase 14.7 — Comment polish (сквозная, частично) ✅

**Цель.** Сквозной проход через codebase для нормализации стиля комментариев. Никаких функциональных изменений - только текст в .go файлах.

**Что сделано в первом проходе.**
- **Punctuation normalization (полностью).** Perl pass по всем 108 .go файлам: em-dash / en-dash / arrows / curly quotes / ellipsis / inequality symbols / cross marks / bullets → ASCII (`-`, `->`, `"`, `'`, `...`, `<=`, `>=`, `!=`, `done`, `fail`). `rg '\x{2014}|\x{2192}|...' --type=go` пустой.
- **Foundation files trimmed (components/ + core/).** 17 файлов прошли по правилам: phase-history refs (`Phase X.Y M14.X.Y:` префиксы) удалены; multi-paragraph headers (~10 строк) сокращены до 1-2 строк; WHAT-комменты убраны (имена очевидны), WHY-комменты сохранены коротко. Sample density drop 3-5x подтверждён. Файлы: `components/{order,unit,movement_profile,engagement_rules,behavior_rules,nav,weapon,building,order_spec,role,hp,threat,faction,stamina,stance_spec,weapon_spec,squad}.go` + `core/{worker_pool,profiler}.go`.

**Что осталось.** systems/ (~30 файлов), ui/ (~10 файлов), main.go, render_world.go, ghost.go, hud.go, command.go, и др. Применять тот же pattern: phase ref strip, multi-paragraph trim, WHAT->WHY. Pacing "файл на commit" сохраняется для будущих pass'ов.

**Не трогаем.** PHASE-*.md и другие .md - они heavy-formatting by design (читают люди). CLAUDE.md / GAMEDESIGN.md / COMMAND-MODEL.md / ROADMAP.md - живые design docs, своя эволюция. Только .go файлы scope'аются.

**Closure criteria.** Build / vet / race tests green ✅. `rg unicode-punct --type=go` пустой ✅. Sample 5 файлов density drop 3-5x ✅ (на 17 файлах). Остальные файлы - открытая задача с устоявшимся pattern'ом.

### Phase 15 — Tactical AI / Formation slack / UI foundation ✅

**Three parallel tracks. Подробности в `PHASE-15.md`.**

**15.A - Tactical AI.**

- `SurvivalInstinctSystem` - при `Suppression > threshold` → cover slot scoring через `dot(slot.CoverDir, -threatDir) * distanceFalloff * (1 - occupancyPenalty)` → `TacticalOverride{Reason, Until}` маркер + ActionQueue override. Marker снимается по Suppression-decay-N-seconds, player explicit order, или safety timeout.
- `ScatterProtocol` - DeltaSuppression-окно (3s window). Trigger threshold → squad state `Scrambling` → bulk TacticalOverride на всех members. Recovery после низкого delta 15s.
- **Doctrines** - `Patrol / Assault / Stealth / Defense` как `DoctrineSpec` table (Spec pattern из 14.5) с дефолтами для MovementProfile + EngagementRules + BehaviorRules. UI chip кликом применяет.
- **Posture audible detection** - `MovementProfile.Posture=Quiet` reduces noise emission; vision system extended with audio cone detection (вторичный sensing channel).
- **Standoff / Sector в EngagementRules** - живые: Standoff auto-repositions если враг ближе threshold, Sector filters WeaponSystem targets.
- **Wait-for-stragglers** - SquadMacroPath freeze advance когда roster spread > 2×spacing. Resume on rejoin.

**15.B - Formation slack + individual positioning.**

- `IndividualPosition{Mode, AbsolutePos, RelativeOffset}` component - optional на unit'а. Absent → normal slot. Present → override.
- Two modes: Absolute (фиксированная мировая точка для статичных позиций), Relative (offset от squad center, переезжает с отрядом).
- FormationSystem split на два sub-pass'а: `Without[IndividualPosition]` slot-driven, `With[IndividualPosition]` override-driven.
- UX entry: single-unit selection + Shift+RMB → Absolute. Multi-unit subset + Shift+RMB на разные точки → per-unit Absolute. Inspector "Return to formation" chip снимает.
- Override lifecycle - default keep on new Order, Alt+RMB clear-all modifier. Unit death автоматически удаляет component.
- Wall-aware separation polish: sliding (parallel-glide), tunnel single-file collapse, adjacent slot swapping.
- Slot clamping polish (carry from 14.6).
- **Living movement polish (M15.B.5)** - acceleration/braking, smooth turning, per-unit personality (slot jitter / reaction delay / speed mul), path look-ahead, anticipatory deceleration. ~120 строк поверх UnitMovementSystem + Motion. Включает Garrison rally / column-collapse как partial-carry from Phase 14.6 (rally fix не успел в hotfix, реализуется как часть auto-file-on-building-approach + tunnel handling). Без A* per-member - squad-as-unit principle. Real animation cycles / footsteps / body-tilt - Phase 17/25 (требуют моделей).

**15.C - UI/UX foundation (UI.md core).**

- `AutonomySpec` table (Strict / Cautious / Adaptive / Survival) + `ActiveAutonomy{Code}` persist component на squad. UI chip применяет spec'и BehaviorRules / MovementProfile defaults.
- Per-field "edited by user" dirty mask - Autonomy chip skip'ает hand-tuned fields. "Reset to Autonomy default" чистит dirty + reapplies.
- **Reason feedback line** в Inspector - State / Override / Reason / Resume rows. 6 initial ReasonCode (UnderFire / LostLOS / NoPath / Reloading / OutOfAmmo / HoldFirePreventsAttack). UI.md §7 full list - Phase 21.
- **Event log panel** - new panel, initially в L1 slot через Tab swap. Event entity + ring buffer resource. 5 initial event kinds (EnemyContact / KIA / SuppressionStart / OrderCompleted / OrderFailed). Click event → map flyTo.
- **Map pings** - `MapPing` entity на map. Auto-spawn from EventLog drivers. Pulsing circle render.
- **AttackMove+HoldFire warning chip** - one-liner Inspector polish (memory TODO). Strike-through "AttackMove" chip with tooltip когда conflict.
- **Inline order timeline** в Inspector squad view - list orders с progress bars. Full git-tree timeline panel - Phase 18.

**Зависимости.** 15.A и 15.B зависят от 14.6 wall avoidance / slot clamping. 15.C самодостаточен. Tracks независимы и могут идти параллельно. Подробности - `PHASE-15.md`.

### Phase 16 — Buildings 2.0 + Asset pipeline ✅ (ядро; asset-часть перенесена)

**Итог закрытия (2026-05-20).** Ядро закрыто generator-путём: Level-модель (per-storey `Level` entities + `LevelNavGrid` + `LevelVisibility`), interior cutaway (`BuildingViewMode`), multi-chunk buildings (BuildingSystem бакетит по всем пересечённым чанкам), `OrderKindClearBuilding` заложен (реализован в 17.6). `AssetRegistry` / .glb loader / .bplan НЕ строились — генератор `gen/buildings` оказался достаточным источником контента; .glb-пайплайн уезжает в 17.5/23 (shape `BuildingPlan` совместим — см. `gen/buildings/builder.go`). Исходная спецификация ниже сохранена как референс для .glb-этапа.

**Цель.** Здания перестают быть процедурными коробками, становятся загружаемыми моделями с per-floor cover/fire maps + interior visibility UX. Параллельно вводится AssetRegistry foundation для будущих texture / model / audio assets.

- **AssetRegistry + .glb loader.** ECS resource. Lazy load. Hash-based AssetID. Async TBD - Phase 16 starts sync, добавляет async когда боль появится.
- **Building .glb import pipeline.** Один .glb = одно здание. Named meshes по prefix: `wall_*`, `door_*`, `window_*`, `floor_N_*`, `stairs_*`, `furniture_*`. Парсер walks scene tree, classify по prefix, emit BuildingPlan {Footprint, Stories, WallSegments, Floors, Stairs, Doors, Windows, Furniture}.
- **Inside-floor subdivision.** Big single-floor buildings разделять на zones. Подход: `zone_<NAME>` meshes (BBox), doors named `door_<A>-<B>` где A и B - zone names. Builder parser автоматически связывает doors с zones по name. Per-zone visibility flag, как per-floor.
- **.bplan binary cache.** Pre-process .glb → .bplan binary (faster reload, version-stable across .glb re-export).
- **Per-floor cover/fire maps.** Уже scaffolded в Phase 6 (CoverMap). Phase 16 wires per-floor variants - cover slots для walls / windows / furniture per floor. Fire arcs aware of floor walls / windows.
- **Interior view UX (Sims cutaway).** `BuildingViewMode{InteriorOpen, CurrentFloor, WallMode}` component. Render predicate hides floors above CurrentFloor + ceiling of CurrentFloor. Wall modes: AllWalls / CameraFacingOnly / Wireframe. Dynamic 3D widget рядом со зданием - floor chips ("1 2 3" vertical), wall mode toggle. Виджет screen-projected как ShortLabel pill.
- **Per-floor fog-of-war.** `FloorVisibility{Discovered, LastSeenAt}` component на Floor (или Zone). Static layout (walls / doors / windows / stairs / room sizes) - always known when building visible. Dynamic contents (furniture / enemy units / door states) - revealed только когда friendly unit на этаже/зоне. Hidden floors render как dark silhouette ("known shape, not contents").
- **Multi-chunk buildings.** Phase 5 P-deferred constraint (footprint within single chunk) убираем. Building children spawn coordinated через `BuildingChildIndex` независимо от owning chunk.
- **Building Order kinds (basic):** `OrderKindGarrison` (existing, без зачистки), `OrderKindClearBuilding` (новый - sweep по этажам + neutralize hostiles + occupy). `OrderKindDestroyBuilding` / `OrderKindBurnBuilding` отложены в 18 (Engineering).

**Зависимости.** Phase 14.6 (walkthrough fix), Phase 15.B (formation slack для navigation в interior). Возможно Phase 15.A (TacticalAI читает per-floor cover slots).

### Phase 16.5 — Building generator + standalone sandbox ✅ (генератор; sandbox не строился)

**Итог закрытия.** Генераторы shipped как `gen/buildings` (House / Office / Compound, `Validate(plan)`, determinism per (template, seed, params)) — canonical pure-data output, расширены в 17.D до 5 этажей / wings / multi-entrance. Standalone `cmd/building_sandbox` НЕ строился — итерация через headless ai_*-сцены оказалась дешевле; `cmd/particle_sandbox` остаётся единственным sandbox'ом. Исходная спецификация ниже — референс.

**Цель.** Tooling между Phase 16 (loader) и Phase 17 (visuals). Программный генератор зданий (House / Office / Compound templates) эмитит `BuildingPlan` в том же shape что Phase 16 .glb loader — test content без Blender'а, validation формата + iteration через standalone sandbox `cmd/building_sandbox/main.go`. Подробности — `PHASE-16.5.md`.

- **Generator templates.** Three initial: House (1-2 stories, прямая лестница), Office (multi-level per floor + central stair), Compound (multi-wing graph + horizontal passage + basement tunnel). Determinism per (template, seed, params); builder DSL helpers поверх declarative spec. Generator + parser share validator code (`Validate(plan)` ловит straddling levels / orphan doors / unanchored stair waypoints).
- **Standalone sandbox.** Аналог `cmd/particle_sandbox`. Boots <1s, без terrain / units / orders. Generator picker UI, cutaway widget из Phase 16.C, hotkey overlays для annotations (L levels, W waypoints, S slots, A ShootingArcs, C CoverDirections, D doors, F furniture, V LevelVisibility, G validator issues). Load .glb path параллельно для loader testing.
- **.bplan round-trip validation.** Generator emit → WriteBuildingPlan → ReadBuildingPlan → diff. Если generator корректен и format covers все поля, diff пустой. Smoke test format coverage.
- **main.go fallback wiring.** `makeStartingBuildings()` без assets/manifest падает в generator (фиксированные seeds, ground truth scene). Полная игра работает без art assets, тестовая местность всегда есть.

**Зависимости.** Phase 16 closure (loader + level/annotation model). Не параллелится — sandbox использует те же rendering paths и BuildingPlan shape.

### Phase 17 — Tactical AI Wave 2 ✅

**Цель.** Юниты ведут себя как живые. Phase 14 (combat) + Phase 15 (реактивность) + Phase 16 (здания) дают каркас, но пять болей остаются: бьются в стены при заходе в здание, бегут спиной к укрытию, торчат за стеной у которой угроза с обратной стороны, толпятся 3-4 за одним кустом, не ложатся под огнём. Phase 17 закрывает все пять под общий фундамент (Threat scalar + DangerEvent + per-unit MicroPath). Plus building generator расширяется до 3+ этажей / multi-wing / multi-entrance.

Пять параллельных tracks + один фундамент:

- **17.0 Threat foundation.** Расширение `Suppression` до `Threat{Total, Suppression, ShotsFired, Endangered, Injury, State}`. Типизированные `DangerEvent` (Gunshot / BulletImpact / Explosion / GrenadeLanding / UnknownFire / DamageTaken / Bleeding / ...) в per-unit ring buffer. Spec-таблица для decay rates + state thresholds (Safe/Vigilant/Alerted/Threatened). Источники идей - `ARMA-REFORGER-AI.md` §3-4. Снимает блокер для 17.B/C.
- **17.A MicroPath per-unit.** Каждый юнит сам строит микро-маршрут через `NavService.FindPath`. `MicroPath{Waypoints [16], Head, Count, GoalSnap, ReplanAt, Dirty}` компонент. Throttle через worker pool budget. Leader-wake bias - не-лидерам goal даётся как `leader.Waypoint[k] + lateral_offset`, не raw `center + offset`. Funnel-hack из Phase 16.B.1.d удаляется в M17.A.2.
- **17.B Cover behavior 2.0.** (1) `dot(CoverDir, ThreatDir)` gate - укрытие должно быть между юнитом и угрозой, не за спиной. (2) Approach-cone scoring - бонус за cover впереди / penalty за cover сзади (бежать спиной плохо). (3) Capacity gate - `Occupancy.Current/Max` enforced, никаких 4-в-куст. (4) CombatMove turn-then-run - `Motion.FacingYaw != VelocityYaw` под `Threatened`, разворот тела перед спринтом > 3 м. (5) Movement к cover через MicroPath, не straight-line.
- **17.C Stance autonomy.** `StanceControllerSystem` mapping `Threat.State → Stance` (Threatened→Prone, Alerted→Crouch, Safe/Vigilant→Stand). Animation lock 0.5 с. Player override (`StanceOverride{Until}` marker) 10 с. Motion gate - prone snap to crouch при speed > 1.5 м/с.
- **17.D Building generator Wave 2.** `HouseParams.Stories: 1..5` + cascade stairs (2 секции + площадка для 3+). `Wings []WingSpec` для L/U/T-shape (`Office`, `Compound` templates). Interior walls + Room concept (virtual subdivision, не отдельная иерархия). Multi-entrance (`DoorSides []uint8`). Sandbox (`cmd/building_sandbox`) расширяется picker'ом.
- **17.E Group target clusters (опционально).** Polar clustering members' Awareness в `SquadPerception` resource. Стабильный `ThreatDir` для multi-threat сцен. Может уехать в Phase 19 если в 17.0/B практика покажет что weighted-recency ThreatDir достаточно.

**Closure criteria.** `cmd/tactical_sandbox` - 1 Office + 1 Compound + Recon (5) vs MotorRifle (8). Player Garrison(Office). Pass: все 5 внутри < 15 с, никто через дверь под огнём, под огнём 3+ Crouch + 1+ Prone, нет cover-pile-up, нет cover-behind-back, `micro_path + threat + cover_select` < 8 мс median tick на 50 units.

Подробности - `PHASE-17.md`.

**Зависимости.** Phase 14 (Combat - producer DangerEvent'ов). Phase 15 (SurvivalInstinct - consumer Threat.State). Phase 16 (Buildings 2.0 + multi-chunk). Phase 16.5 (Generator + sandbox - расширяется в 17.D).

### Phase 17.6 — Building interactions + camera + 3D markers ✅

**Цель.** Управление зданиями становится контекстным и не перегруженным. ПКМ-tap на здание = `OccupyBuilding` (новый kind равномерного распределения по этажам), вместо текущего auto-Garrison у окон. ПКМ-hold > 200ms на здании = прямоугольный popup с секциями Attack / Interact. Camera orbit переезжает на MMB — RMB больше не претендует на камеру. Order-маркеры рендерятся в 3D-сцене для selected squads (depth-test off, видны сквозь стены). Surface buildings получают terrain leveling под footprint (1 m skirt + 5 cm Z-offset).

- **`OrderKindOccupyBuilding`** — новый kind. Completion = все живые в footprint. Distribution = ростер делится по этажам, MoveTo на Floor.AABB center с FormationSystem spread. Без cover-slot ranking (то у Garrison).
- **`OrderKindClearBuilding`** — новый composite kind. Completion = no hostiles inside + ≥ 1 friendly. При complete → auto-chain в OccupyBuilding (через `OrderChain.Next`).
- **`ui.ContextMenu`** — generic infrastructure (секции с хедером + items с icon/label/tooltip). Phase 17.6 использует только для здания; reuse'абельна для unit/trench/marker в будущих фазах.
- **Camera switch.** `OrbitSystem` ловит MMB вместо RMB. `OrbitInputEnabled` гейт сохраняется (focused = Panel3D / None), но pie-suppressors удаляются — pie не в RMB flow.
- **3D order markers.** Куб per Order tinted в squadColor + соединительные линии + per-kind glyph (placeholder буква). DefendPosition arc reuse через `drawGhostArc`. DepthTest off.
- **Floor section highlight.** Hover на здание + ray ∩ Floor.AABB → outline вокруг Floor (не полный footprint). Подсвечивает «куда ты целишься», ключевой UX для «Занять L*N*».
- **Ghost preview swap для OccupyBuilding.** Распределение dots по этажам (не по окнам как Garrison). Popup hover на разные items → ghost обновляется per-kind.
- **Pie deprecated.** `pieMenu.Begin/Tick/Draw` callsites удалены из main.go. `ui/pie_menu.go` остаётся в репо без вызывающих сторон (примитивы могут пригодиться для будущих радиальных кейсов).

**Closure criteria.** Test scene: 2-storey Office + 5 Recon + 2 enemy MotorRifle inside. (1) MMB вращает камеру, RMB нет. (2) ПКМ-tap на здание → 3+2 распределение по этажам. (3) ПКМ-hold → popup 6 пунктов в 2 секциях с tooltip. (4) «Зачистить и занять» → clear + auto-OccupyBuilding. (5) «Атакующая позиция» = существующий Garrison. (6) «Закрытая позиция» → Crouch + HoldFire. (7) «Занять L1» → squad на 2-м этаже через лестницу. (8) 3D markers видны сквозь стены для selected. (9) Floor outline переключается per-уровень. (10) Hover в popup → ghost swap. No regressions: trench / terrain / враг RMB-tap работают.

Подробности — `old/PHASE-17.6.md` (archived).

**Зависимости.** Phase 17 (MicroPath / Cover 2.0 — поведение внутри зданий не позорное). Phase 14.6 (Garrison completion + no-walkthrough). Phase 13.6 (Ghost preview / facing-drag механика reuse'абельна).

**Что вскрылось при тестировании.** Юниты бьются в стены не обходя их (только velocity reflection в `reflectAgainstWalls`, без replan). Squad упирается в первую секцию 3-section building'а не доходя до средней/задней. Cover-pick / formation slots иногда оказываются за стенами — clamp находит cell но юнит туда не дойдёт. Карта: ход на лестнице/в зданиях местами не гладкий — z-coord lerp работает, но при сложных layout'ах путь через wing junctions отсутствует. Перенаправлено в Phase 17.8 (Tactical AI Wave 3).

### Phase 17.8 — Tactical AI Wave 3 (Utility + ORCA + smart units) ✅

**Итог закрытия (2026-05-25, вместе с 17.9 hardening).** Suite 9/10 PASS, aggregate 79/80. Финальный fix-set 17.9: escape additive, NavInBuilding door-outside clear, junction cost 1→10, floor-centre AABB convention (`Floor.WorldPos.Local` — центр плиты, не угол), level wall inflate. Второй hardening-раунд 2026-06 (movement invariants, 19/19) — см. «Что сейчас работает». Известный дефект ORCA — data race на соседях, фикс в WS-B. Подробности — `old/PHASE-17.8.md` / `old/PHASE-17.9.md`.

**Цель.** Отряд юнитов превращается в серьёзную единицу — не цирк со застреванием в дверях. Per-unit AI получает Utility evaluator (5-7 action modes с hysteresis), ORCA local avoidance (RVO-style — обход стен и соседей без collision), slot-aware MicroPath (целью становится конкретный formation slot, не squad center), multi-section building pathing (TransitionEdge между Levels одной storey через wing junctions). Inspector показывает reason для текущего mode — игрок понимает почему юнит ведёт себя именно так.

Цель сформулирована перед Fog of War (Phase 18+): без хорошего tactical AI FoW = frustration (игрок не видит часть карты, доверяет AI отыграть, а AI глупый). 17.8 — критический путь.

- **Utility evaluator (per-unit, 0.5s cadence).** Spec table `UtilitySpec` с 5-7 modes (Following / Engaging / TakingCover / Repositioning / Reloading / Suppressed / опц. Treating). Score per mode = weighted sum of signals (threat magnitude, distance to goal, cover quality, ammo, allies nearby). Hysteresis: mode switch требует score > current + delta AND min duration since last switch. Reason text пишется в TacticalOverride.Reason / новый `LocalBlackboard.ReasonText`.
- **RVO/ORCA local avoidance.** Per-tick per-unit: neighbour set через spatial hash (Phase 14.5), half-plane constraints от neighbours + static obstacles (walls), линейная программа → new velocity. ~200-400 строк математики, neighbour cap ~15-20 для O(N) на 100 units. Заменяет `reflectAgainstWalls` bandaid.
- **Slot-aware MicroPath.** Целью становится individual formation slot (squad_center + offset), не squad waypoint. FormationSystem перестаёт быть post-hoc clamp'ом — он только writes slot target. Если slot blocked — pathfinder сам обходит. Рефактор связи FormationSystem ↔ MicroPath.
- **Multi-section building pathing.** TransitionEdge между Level entities одной storey через wing junctions (open passages / archways). Тестовый case — 3-section Office: ПКМ-tap на каждую секцию доводит squad до целевой через door нужной секции, не упирается в первую.
- **Replan-on-stall.** Stuck-detect: collision-with-wall 2-3 раза за 0.5s → MicroPath.Dirty. Альтернативный путь или Failed state.
- **Inspector reason row.** ReasonText читается из LocalBlackboard. Inspector показывает "Following waypoint" / "Taking cover from N-E" / "Repositioning to flank" / "Blocked: no path".

**Closure criteria.** Несколько сценариев. (1) **50 units squad march через узкий проход** — никто не сталкивается, спокойно проходят, ORCA разводит. (2) **3-section Office** ПКМ-tap на разные секции — squad доходит до целевой через свою дверь. (3) **Squad под обстрелом** — 80% в TakingCover, 20% Engaging если cover insufficient, Inspector показывает reasons. (4) **Squad после контакта** — все переходят в TakingCover при awareness > threshold, при threat clearing → Following. (5) **Multi-squad через одну road** — ORCA разводит без stuck'ов. (6) **Performance** — 100 units AI cost < 3 ms median tick.

Подробности — `old/PHASE-17.8.md`.

**Зависимости.** Phase 17 (MicroPath / Threat / StanceController / Cover 2.0 — фундамент, расширяется не строится с нуля). Phase 14.5 (spatial hash для neighbour query в ORCA). Phase 7 (TransitionRegistry — расширяется для multi-section).

### Phase 17.7 — Command Surface: Squad Bar + Behavior split + Inspector slim ✅ (закрыта 2026-08-02 на M0-M4; план — `old/PHASE-17.7.md`)

**Итог.** Три поверхности, у каждой одна работа. **Squad bar** (`ui/squad_bar.go`) — карточки бойцов а-ля Men of War оверлеем по низу 3D: роль/стойка, оружие с боезапасом, HP+стамина, клик = выбор бойца (Shift — тоггл), клик по чипу группы = весь отряд; корпус техники получает широкую карточку с построчным лоадаутом и янтарной меткой активного рефлекса. Раскладка считается ДО `handleInput` (иначе клик проваливается в 3D), порядок карточек — память самого бара с 5-секундными KIA-tombstone'ами (ростер чистится в тик смерти). **Behavior widget** (`PanelBehavior`, хоткей `Q`) забрал все standing rules; stateless — всё состояние в ECS. **Inspector** похудел до статуса + приказов + override + навигационного ростера, получил read-only quick-badges (`Pace | Stance | RoE | Autonomy`, клик открывает Behavior) и переехал с легаси `drawText` на `Column`/`TextClipped`. Мелочи механики: скролл стал пер-поверхностным (лист и флоатер одного виджета имеют разные размеры → разные офсеты), у инспектора появились честные sim-часы вместо `rl.GetTime()`.

**M5 (per-weapon aim mode) НЕ сделан — уехал в Phase 21 целиком.** При разборе вскрылось, что канонический вид (COMMAND-MODEL §5: weapon-pref на `AttackTarget` Order'е **owner-юнита**) не имеет фундамента: (1) приказы адресуются только отряду — солист (любая одиночная машина) приказ не держит, `dispatchOrder` шлёт ему `pushSoloMove`; (2) `AttackTarget` не наводит огонь — weapon-код нигде не читает `OrderTarget`, приказ лишь снимает HoldFire (ISSUES #28). Отрядный weapon-pref дал бы семантику против канона (клик по пушке танка А = приказ всему отряду), поэтому M5 идёт после per-unit order layer + focus fire.

**Побочные находки.** ISSUES #27 — краш при КАЖДОМ выходе (двойной `RL_FREE(shader.locs)`: материал заимствует шейдер у `worldShader`, а `UnloadMaterial` считает его своим) — починен. Харнесс `-shot` снимал кадр ДО флеша батча raylib, поэтому последний нарисованный слой не попадал ни в один скриншот (терялись заголовки панелей и HUD) — починен, плюс добавлены `-shot-panel` / `-shot-select-veh`.

**Хвосты.** M5 + per-unit orders + focus fire → Phase 21; collapse карточек при overflow (сейчас честный `+N`) → по жалобе; drag-and-drop бойцов между отрядами → UI expansion; иконки оружия вместо текста → 17.5; bar над картой в Command-preset → по плейтесту.

### Phase 17.5 — Visual fidelity 1 📋 (на паузе — после WS-B → Карты → 19 → 22-lite)

**Цель.** Игра выглядит как тактический симулятор, не как программистский прототип. Юниты - модели, не кубики. Дороги - плавные кривые, не рваные сегменты. Биомы - текстуры на map, не только procgen.

Phase 17.5, не 17, потому что визуальный pass осмыслен только когда юниты ведут себя сносно (Phase 17 closure) **и UI разделение завершено** (Phase 17.6/17.7) — модели проектируются на финальной семантике приказов, не промежуточной. До Phase 17 моделирование .glb для cap-cubes которые бьются в стену = переставлять стулья на тонущем Титанике. До Phase 17.7 — оформлять иконки для popup'а который ещё рефакторится.

- **Unit models.** Per-role .glb (rifleman / leader / sniper / mg / ...). Replaces cap-cube. Vertex animation - placeholder static pose, real anim - Phase 25.
- **Prop models.** Oak / Pine / Birch / Bush / Rock - real .glb meshes. Procedural placement остаётся, swap'ается только render.
- **Texture pipeline.** PNG → rl.Texture2D loader. Atlas system для batched draw calls.
- **Basic lighting.** Directional sun + ambient. Phase 25 polish может добавить shadow maps.
- ~~**Road / river splines.**~~ Закрыто срезом 17.5-ROADS 2026-07-28 иначе, чем планировалось: граф не тесселируется в сплайн (это сломало бы `RoadRoute[32]` и цену Dijkstra) — осевая скругляется филе на углах в производной геометрии, граф остаётся источником правды. Исходный план ниже — историческая справка. `RoadEdge.Spline []ControlPoint` (Catmull-Rom default; Bezier optional). `PreprocessRoadGraph` tesselates spline → polyline + 1m step → existing RoadFlatten / NavGrid / RoadProp pipelines читают polyline без изменений. Smooth turns при пересечениях. Rivers symmetric.
- **Biome map overlays.** PNG masks (forest_mask.png / urban_mask.png / water_mask.png) → composited `BiomeAtlas` texture. PropSpawn samples atlas для density. 2D map renderer overlay'ит над hill-shade. Procedural fallback остаётся когда mask absent.
- **Map object icons.** Building → letter glyph (B/T/F) + rectangle outline. Forest → green polygon region (aggregated tree props). Units → existing short label. Terrain texture - generated (hill-shade + biome tint), не static PNG.

**Зависимости.** Phase 16 (AssetRegistry, .glb pipeline). Phase 16.5 (generator templates как тестовый контент). Phase 17 (юниты ведут себя нормально, моделирование осмыслено). Phase 14.7 (clean codebase для refactor легче).

### Phase 18 — UI L4 (Blender-style) + Plan timeline + Undo/Redo ✅ (core) · 📋 (хвост)

**Итог (2026-05-21/22, core).** Shipped: floating panels (title-drag, resize со всех сторон/углов, chevron-swap контента, input-shield), workspace merge/split/switch через chevron menu, Formation editor (floating + leaf, presets), Timeline panel. НЕ строились: полный tree-of-splits drag-dock, tabs, layout presets (floating + chevron закрыли основную потребность — остаток в Phase 24). Undo/Redo не строился — уезжает в PlanOp поверх UICommand (REFACTOR-PLAN WS-C, «хвост 18»). Исходная спецификация ниже — референс.

**Цель.** Полный Blender-стиль интерфейс. Splittable panels, drag-dock, tabs, floating windows, layout presets. Animated git-tree timeline для плана. Undo/Redo plan edits.

- **PanelManager refactor на tree-of-splits.** Recursive node = either leaf (panel with tabs) or split (orientation + children + ratio). User drag splitter line - resize. User drag panel header to dock zone - relocate. Drag to "void" - undock as floating window.
- **Tabs inside panel slot.** Multiple content panes share slot, tab bar для switch. Drag tab between slots.
- **Floating windows** - in-game floater (not OS window initially). Phase 25 may add OS undock.
- **Layout presets** - save/load named layouts. Auto-save on quit, restore on launch.
- **Plan timeline panel.** Horizontal scroll, left-to-right time. Squad rows. Order blocks с прогресс-баром. Active highlight, completed fade, failed red border. Time gaps между orders preserved (10:00 → 10:30 = visible gap). Click block - camera fly-to + Inspector highlight. Drag block - reorder. Right-click - context menu (Insert before/after / Delete / Convert to joint). Animation: slide-in новых блоков, glow на active, fade на completed. **Открывается снизу** (как Blender timeline) - wide-not-tall layout. Joint orders как vertical links между squad rows.
- **Undo/Redo plan edits.** `PlanEditHistory` resource - stack of `PlanEdit` ops (IssueOrder / CancelOrder / InsertOrder / DragWaypoint / DoctrineChange / etc.). Each op carries `Inverse()`. Ctrl+Z pop & apply inverse. Ctrl+Y reapply. Non-tracked: AI-driven completion / failure / TacticalOverride (sim, не player edit).

**Зависимости.** Phase 15.C (event log + map pings + reason feedback живые), Phase 17 (visual passes need clean codebase).

### Phase 18.5 — FoW + Symbology + Sensors ✅

Закрыта 2026-05-29. `ContactSystem` поглотил VisionSystem: детерминированная sensor-модель (`Sensors` — 4 канала DOD; effective-range × конфигурируемый Falloff × FacingProfile × concealmentMul; НЕ вероятностный detect), `Contact` entities (никогда не auto-delete — fade к floor-alpha по `ContactAgeAlpha`, ручное удаление через RMB ctx-menu), `ContactRegistry`, source-promotion (Sensor → CombatEvidence → CloseRangeID → PlayerClassified, без даунгрейда), audio-bubble carry-over, APP-6 симвология на 2D-карте (DrawSymbol + presets + per-contact override), `LevelVisibility` (пофлорный FoW), EventLog. Deferred polish — список в `old/PHASE-18.5.md`; симвология persistence → 18.9 (WS-F), per-faction belief → WS-G (к скриптовому ИИ).

### Phase 18.9 — Save/Load мира (мини-фаза, REFACTOR-PLAN WS-F) ✅ (закрыта 2026-07-07)

Full-pool snapshot (свой бинарный кодек, ориентир ark-serde), remap-таблицы по ссылочным типам НЕ строим (P3: remap-путь = скрытая XL, запрещён). Классификационная spec-таблица «тип → save/skip/custom» (~105 компонентов + 18 ресурсов, compile-time exhaustiveness); memcpy для ~80% fixed-size POD; custom-кодеки 5-6 ресурсов (StreamingMap, BuildingPlanIndex, симвология → `save/symbols.json` — закрывает deferred 18.5); null-on-save для derived-ссылок (AssignedCover / AssignedSlot / TransitionRegistry — SurvivalInstinct перевыберет за тик); Level-сущности в снапшот (FoW едет бесплатно). Post-load rebuild: chunks → building children → spatial bake → registries (порядок!). Гейт: save в середине headless-боя → load в чистый процесс → ≥1000 тиков; replay-хэш непрерывен через границу save/load. Оценка 8-10 дней. Timing: после мини-трека «Карты», желательно до Phase 19 (DP-5 — каждая фаза добавляет типы в таблицу). Кодеки писать per-component — delta-слой ляжет сверху, если MP-дверь откроется; NetID / delta / interest — НЕ строить.

### Phase 19 — Vehicles (ground) + Jolt decision ✅ (M0-M7 закрыты 2026-07-31; Jolt НЕ берём — кинематика хватает)

**Что вышло:** `VehicleSpecs` spec-таблица (5 классов), кинематический драйвер с
дуговым рулением и реверсом, `RoadRouter` (time-cost Dijkstra по `RoadGraph`) с
проекционными рампами, башни + пер-ствольная гуннери с классовыми множителями и
секторной бронёй, рефлексы (`VehicleOverride`: FaceThreat / SmokeAndReverse /
Flee), объезд зданий и корпусов + `resolveOverlaps`, конвои (stateless pace-cap
по ростеру). Физика — своя кинематика на фиксированном тике; jolt-go не взят.


**Цель.** Тактический контекст и базовый combat есть. Vehicles в текущем roadmap'е сдвинуты с Phase 16 на Phase 19, чтобы сначала закрыть visual fidelity + UI L4 (Phase 16-18). Jolt physics decision point - в начале фазы делается простой kinematic prototype, затем по объёму планируемой динамики решаем jolt-go integration или custom rigid body. Fixed timestep (WS-B) — предусловие: физика интегрируется на фиксированном тике, не на переменном dt.

- **Prep (REFACTOR-PLAN WS-E ш.1):** `OnGround` marker + `LocomotionClass` в NavOpts; второй `SpatialHash` для техники (один hash на locomotion class — см. memory/SpatialHash invariants).

- `Vehicle` компонент: `MaxSpeedRoad`, `MaxSpeedOffroad`, `TurnRadius`, `SeatCount`.
- `RoadFollower`: текущий edge ID + прогресс + цель. Pathfinding по `RoadGraph` (узловой A*) + локальный sticky-to-edge.
- Off-road штраф: при `BlocksMovement` под колёсами / отклонении от ребра > X м, скорость падает до `MaxSpeedOffroad`.
- Vehicle-aware NavService (отдельный locomotion type в `NavOpts`).
- Тестовая сцена: пара грузовиков, патрулирующих между точками карты.

**Зависимости.** Phase 6 (NavGrid), Phase 11 (Order: VehicleMoveTo / Patrol).

### Phase 19.5 — Поведение (Tactical AI Wave 4) ✅ (M0 + треки A/B/C закрыты 2026-08-04; остался MZ — owner-плейтест)

**Что вышло:** гигиена движения пехоты (wake-якоря, string-pulling микропути,
честный ORCA со стоящими), техника (пропы/рельеф в объезде, гистерезис детура,
watchdog прогресса с честным Failed, рефлексы владеют корпусом, конвои),
Threat 2.0 (кластеры угроз + зоны обстрела как сущности), position scoring 2.0
(места, а не слоты: траншеи, дефилада через `CoverMap.DirMask`, тень корпуса),
SquadBrain v0 (`SquadPlan`: Bounding / Relocate / ClearSeq) и focus fire по цели
AttackTarget. Сцен в гейтах: 54 replay / 31 saveload. Детали и хвосты —
`PHASE-19.5.md`.


**Цель.** Юниты и техника перестают быть болванчиками: чинится механика движения (скрежет у стен, дёргание строя, ORCA со стоящими, пропы/рельеф/watchdog для техники), достраивается сенсорика угроз (взрывы/урон/зоны — сейчас единственный источник опасности это пуля в 5 м), pickCover перерастает в position scoring (траншеи, терренная дефилада через неиспользуемый `CoverMap.DirMask`, интерьеры, тень корпуса техники, фолбэк «залечь и выползти»), и строится недостающий оперативный ярус — `SquadBrain` (utility-арбитраж, 1-2.5 с): bounding-перебежки под контактом, relocate из зоны обстрела при живом приказе, секвенсор зачистки здания. Ключевой принцип арбитража: **автоматика меняет «как», не «что»** — приказ игрока не отменяется, мёртвые гейты `BehaviorRules` (HoldUntilOrdered / AllowAutoReposition / AllowReturnFire) обретают читателей, каждый перехват управления виден в EventLog. Плюс минимальный срез ISSUES #28 — focus fire по цели AttackTarget.

Мотив и полный диагноз — аудит 2026-08-03 (4 направления). Порядок внутри: быстрые победы → пехота → техника → мозг; каждый милстоун под полными replay/saveload-гейтами, ~15 новых ai_* сцен. Трек «мозг» — фундамент 22-lite: бот получает тот же SquadBrain сверху.

Подробности — `PHASE-19.5.md`.

**Зависимости.** Phase 17.8 (Utility/ORCA/MicroPath), Phase 19 M0-M7 (формальное закрытие 19 не блокирует — параллельно), Phase 15 (SurvivalInstinct — перерабатывается), PHASE-VISION (terrain-LOS в огне), WS-B (детерминизм).

### Phase 19.7 — Attention & Map Tools (UX-выводы из Sea Power) 🚧 (M1-M5 по коду 2026-08-09; остался owner-плейтест)

**Цель.** Менеджмент внимания на длинной дистанции + дешёвые инструменты карты. Источник — сравнительный анализ `SEA-POWER-INTERFACE.md` (2026-08-09): для концепции длинных операций нужны компрессия времени выше 8× и авто-реакция на события, порознь они не работают.

- M1 ✅: матрица авто-реакции `EventKind → Ignore/Notify/Slow/Pause` поверх существующего `EventLog` + панель Events (чипы + click-to-fly) + баннер AUTO. Авто только понижает скорость, возврат — игрок.
- M2 ✅: компрессия 16×/32× с адаптивным тик-бюджетом `core.AdvanceBudget` (best-effort, SimDt неизменен, первый тик не срезается). Не мержилась без M1.
- M3 ✅: синтезированные звуковые сигналы per-EventKind (`audio_cues.go`, WAV в памяти, без ассетов; голосовое радио — Phase 24).
- M4 ✅: линейка на карте — hold-`L` + drag, дистанция + пеленг, подкраска по дальности оружия выделения.
- M5 ✅: карточка ТТХ (read-only проекция `WeaponSpec`/`VehicleSpec`, вход из строк Inspector'а).

**Одна правка в симе** (план обещал чистый frame-side): `EventEnemyContact` не писала ни одна система, без него авто-замедление на первом контакте не срабатывает в принципе — теперь его пушит `ContactSystem` при создании контакта наблюдателя-игрока. Лог для сима write-only; бит-идентичность доказана (`replay_baseline` 55/55 IDENTICAL). Контактный `MapPing` осознанно НЕ добавлен — спавн сущности сдвинул бы entity ID и все хеши.

Отвергнуто по итогам того же анализа: уровни сложности с подглядыванием (FoW всегда сенсорный), полная энциклопедия. Отложено с адресами: маркеры карты и рандеву по времени → Phase 21, EMCON → Phase 20+, голос → Phase 24. Подробности и отклонения от плана — `PHASE-19.7.md`.

**Зависимости.** Phase 15.C (EventLog + пинги), Phase 18 (флоатеры/виджеты), WS-B (TimeScale/Advance).

### Phase 20 — Aviation + Infantry-Vehicle interop 📋

**Цель.** Воздушная техника (helicopters / jets / транспорт) - отдельный locomotion class. Параллельно - режимы совместного движения пехоты и техники (Mounted / Riding / Following / Escorting). Раньше шли отдельными Phase 16.5 + 17; объединены в 20 после reorganization roadmap'а.

- `Aircraft` компонент с `Altitude`, `Heading`, `Speed`, `AltitudeBand` (Helo / TransportLow / JetHigh).
- 3D-pathfinding (свободная Y-координата, обход высоких препятствий).
- Adaptive movement step для high-speed entity (jet 100-300 m/s — обычный 16ms step = 1.6-4.8м, может проскочить препятствие).
- Vision / Audio sig сильно отличаются от ground.
- Movement system должна быть `OnGround`-marker-aware (GroundStick применяется только если есть маркер; aircraft без маркера летает по своей Y).
- `EmbarkedIn{Vehicle, Mode}` компонент на Squad'е (Mounted / Riding).
- `EscortPair{With, Mode}` компонент (Following / Escorting).
- Order types: `Mount`, `Disembark`, `Follow`, `Escort`.
- AI: SquadMacroPath модифицируется в зависимости от Mode (Mounted = идём с vehicle, Following = центр Squad'а позади vehicle с противоположной стороны от ThreatDir).
- `DynamicCoverEmitter` на Vehicle — Cover Shadows, динамически переоцениваются по ThreatMap.
- AI-veto на mount-into-burning.
- **Joint orders как multi-owner concept**: `OrderJoint{Participants, BarrierKind}` компонент на Order entity — несколько queue'ов могут ссылаться на один Order. Synchronization barriers: `BarrierEnterVehicle` / `BarrierExitVehicle` / `BarrierArriveAll`. Embark / Dismount как первые joint Order kinds. См. `COMMAND-MODEL.md` §3.
- Vehicle physics finalization (Jolt integration если принято в Phase 19 decision).

**Зависимости.** Phase 19 (Vehicles infrastructure + Jolt decision), Phase 15 (TacticalAI читает ThreatDir), Phase 11 (Order linked-list infrastructure).

### Phase 21 — Engineering + Multi-squad coordination + Comms 📋

**Цель.** Три связанных subsystems объединены в одну фазу - они shareить infrastructure (joint orders, build orders, radio gates). Раньше были Phase 18/19/20.

**Engineering.**
- Order types: `Build{Kind}` (Trench / Sandbag / LogBarrier / MineField / ATObstacle).
- `BuildPlan` сущность с `BuildProgress`, видимая на карте как полупрозрачный outline.
- `EngineerSystem`: юнит с ролью Engineer приближается к плану, поднимает прогресс по таймеру. Multi-engineer ускорение через sqrt(N).
- Mine entity'и: invisible до подрыва, blocks-movement on top.
- **`OrderKindDestroyBuilding`** и **`OrderKindBurnBuilding`** (Phase 16 deferred kinds) - real implementation. Burn engages molotov / demoman; Destroy через directed fire.

**Multi-squad coordination.**
- `OrderGroup{GroupID}` компонент на Order — общий ID для joined orders.
- Resolver при multi-squad selection + ПКМ: спавнит N Orders с одинаковым GroupID, auto-distributes targets.
- **`OrderKindDisperseOnLine`** - polyline placement. RMB-hold-drag рисует arc, юниты распределяются равномерно. См. `COMMAND-MODEL.md` §6.
- Joint orders расширяются на N squad'ов (Phase 20 заложил для 2; здесь до 8).
- Pie menu для multi-squad context.

**Comms / Radio.**
- `RadioNetworkSystem`: проверяет RadioOperator в ростере, обновляет `RadioNetwork.HasRadioman` / `HQReachable`.
- Action masking: приказы игрока к Squad'у с `HQReachable == false` ignored или delayed. RadioOperator смерть → pickup механика.
- Радио-мост через технику с тяжёлой рацией.
- UI: squad без связи отображается серым / с `?`.

**Зависимости.** Phase 11 (Orders), Phase 12 (Engineer + RadioOperator roles), Phase 15 (Tactical AI для multi-squad coordination), Phase 20 (Joint orders + vehicle radio bridge).

### Phase 22 — Strategic AI 📋

**Цель.** Макро-уровень поведения противника.

- Utility AI как старт (что атаковать, когда отступать, куда направлять резервы).
- Influence Maps (плотность сил, угрозы, контроль территории).
- Логирование решений в формат, удобный для будущего ML.
- 🔮 Imitation Learning по своим реплеям, потом RL self-play. ONNX Runtime для инференса (только верхний уровень). Подробности — `GAMEDESIGN.md` (TBD: вынести в отдельную секцию там).

**Зависимости.** Phase 15 (тактика работает), Phase 21 (multi-squad coordination как primitive).

### Phase 23 — Content pipeline 📋

**Цель.** Реальные данные местности + замена placeholder моделей. Bezier road editor с per-CP tangent control (Phase 17 ship'нул Catmull-Rom; здесь полный manual control).

- Парсер OSM / OpenHistoricalMap → дельты чанков и `RoadGraph` (как Bezier splines с control points).
- DEM (SRTM / ASTER) → heightmap чанки.
- Landsat / Sentinel-2 → биомные маски (forests / urban / fields).
- Bezier road editor (in-game) - per-CP tangent handles, insert / delete CP, road kind switching.
- Mission scripting infrastructure (.mission.bin format, scenario editor optional).
- Large-world streaming (10×10 km+).
- Map underlay из DEM-tile'ов вместо процедурного hill-shade.

**Зависимости.** Phase 16-17 (asset pipeline + spline foundation), все геймплейные фазы (есть что показать на реальной местности).

### Phase 24 — Polish 🔮

- Анимации поз / движения / стрельбы.
- Звуковая система (шаги, выстрелы, голоса).
- ~~Save/Load всего мира~~ — вынесен в мини-фазу 18.9 (WS-F).
- **Data-residency / universal-sim (WS-E ш.6, DP-7):** `SampleHeight(wx, wz)` = heightmap чанка если загружен, иначе `GroundHeight` + derivable-срезы (river/road/building/trench); выбор «N окон вокруг активных сил» vs упрощённая дальняя модель боя — решать по факту использования окон ИИ Phase 22. Заодно чинит GroundStick-на-Modified (TAILS: юнит в траншее).
- Полный tree-of-splits drag-dock + tabs + layout presets (хвост Phase 18).
- UI скины, customizable layouts, hotkey rebinding.
- Direct Control mode (взять одного бойца «за плечо»).
- Balance pass на основе playtesting.
- Tutorials / onboarding.

---

## Замороженные / архивные направления

### 🗄️ SDF / воксельная деформация
Глубокие изменения поверхности (произвольное копание, тоннели). Отброшено: heightmap-стампы покрывают требуемые кейсы (окопы, воронки, фундаменты). Полноценный воксельный мир — на порядок CPU/память без пропорциональной отдачи.

### 🗄️ Слои и порталы
Старый Phase 3 (порталы как cornerstone). Отвергнуто: здания живут в Layer 0, окна — LOS-прозрачные стены, бункер — заглублённое здание. Архив: `.claude/old/PHASE-3-portals.md`.

### 🗄️ FoW через порталы
Ушло вместе со слоями. FoW теперь — обычный механизм в WorldPos-координатах (Phase 15 / 23).

### 🗄️ Phase 8 нумерация
В предыдущем ROADMAP'е Phase 8 была «Дорожная техника». После reorganization 2026-05-18 она переехала на Phase 19. Старый номер 8 не используется.

### 🗄️ Modding / scripting API
Single-dev project, никогда.

### 🗄️ Multiplayer как первичная цель
Single-player + AI opponent — primary. Multiplayer как далёкая опция, не приоритет.

Позиция 2026-06 (REFACTOR-PLAN P2 / DP-1): **lockstep закрыт навсегда** (camera-driven мир = XL, float-математика, fork-join параллелизм). Открытая опция — **co-op одной фракцией поверх host-authoritative listen-server** (primary) и **WEGO result-stream** (secondary, единственный жизнеспособный PvP-путь). Детерминизм поддерживается в объёме «один бинарь / одна машина» (replay, отладка, WS-B) — НЕ кросс-машинный. Оплата вперёд — только конверт UICommand (POD payload + tick-stamp + PlayerID, WS-C). Стоп-лист: NetID, delta-кодеки, interest management, транспорт — до пост-24 продуктового сигнала.

---

## Заметки на полях

- Этот ROADMAP — детализация плана из `GAMEDESIGN.md`. При изменении видения сначала правится GAMEDESIGN, потом перерисовывается ROADMAP, потом активная PHASE-N.md.
- Зависимости между фазами не строго линейные. Phase 15.A / 15.B / 15.C - три параллельных track'а одной фазы, могут идти в произвольном порядке.
- Reorganization 2026-05-18: visual fidelity + UI L4 + Buildings 2.0 подтянуты с Phase 21-24 в Phase 16-18. Vehicles сдвинуты с Phase 16 в Phase 19. Причина: после Phase 14 combat core стало очевидно что игра не выглядит как тактический симулятор. Buildings/visuals перед deeper AI и vehicles.
- Контент-пайплайн (Phase 23) можно начать раньше, как только формат `PropTypeRegistry` стабилизируется — параллельный track. Первый шаг уже вынесен в мини-трек «Карты» (world manifest).
- Стратегический ИИ (Phase 22) можно начинать с простого Utility AI после Phase 15; нейросетевой апгрейд — годы и отдельный проект. Порядок 2026-07: сначала скриптовый utility-оппонент (22-lite — baseline, curriculum, валидатор дизайна), NN только на верхнем ярусе и только после стабилизации правил.
- Расширение игрового мира до 20×20 км или 40×40 км повлияет на streaming radius'ы, content pipeline, размер map underlay — это становится темой Phase 24, не Phase 10. В Phase 10 underlay генерируется для текущего «маленького» мира (2×2 км placeholder).
