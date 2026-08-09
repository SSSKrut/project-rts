# REFACTOR-PLAN — структурное оздоровление + MP-опция

Дата: 2026-06-12. Источник: два multi-agent аудита (структура кода + доки; детерминизм / single-anchor / командный слой / сериализуемость).
Статус: план согласован концептуально, DP-решения (ниже) — за владельцем.

## Диагноз (кратко)

«Движок сросся с игрой» — ложный диагноз. Слоистость чистая (core ← ничего, ui НЕ импортирует
systems, системы общаются только через компоненты/ресурсы и 5 сервисов). Реальные проблемы:

1. `func main()` ~2630 строк: closure-мегаскоуп (~50 хэндлов, ~40 локалов, 9 замыканий).
2. Три несогласованных канала UI→ECS (draw-записи, 3 mailbox-глобала, поллинг результатов).
3. Симуляция структурно привязана к камере: стены/NavGrid/полы существуют только в стрим-окне
   вокруг единственного LODAnchor — бой вне окна идёт сквозь несуществующие стены.
4. Найденные SP-баги (см. WS-A/WS-B): гонка ORCA-соседей (настоящий data race), двойной тик
   ContactSystem (часы FoW идут 2×), LevelVisibility расфоговывается вражескими юнитами,
   мёртвый Garrison-пин (order_resolver вешает LODAnchor, streaming его игнорирует),
   GroundStick клампит к procgen-высоте, игнорируя Modified-рельеф (юнит в траншее стоит
   на до-срезной высоте).
5. Гигиена: незакоммиченные +2160 строк Phase 18.5; мёртвые vision.go / pie_menu.go;
   `.claude/group/` (51 stale-копия); ROADMAP отстал на ~5 фаз; в CLAUDE.md упомянут
   несуществующий VisualEvents.

## P-решения

- **P1. Движок НЕ выделяем.** Ни пакета `engine/`, ни обёрток над raylib, ни обобщения
  core.App. Структуризация — внутренняя: декомпозиция main(), командная воронка, при
  реальной боли — подпакеты systems/worldgen + systems/terrain (не раньше).
- **P2. Lockstep закрыт навсегда** (camera-driven мир = XL, float-математика, fork-join;
  соответствует GAMEDESIGN принципу 8). Открытая MP-опция: **co-op одной фракцией поверх
  host-authoritative listen-server** (primary) и **WEGO result-stream** (secondary, единственный
  жизнеспособный PvP-путь). Детерминизм поддерживаем в объёме «один бинарь / одна машина»
  (replay, отладка) — НЕ кросс-машинный. Подтвердить: DP-1.
- **P3. Save/Load — мини-фаза 18.9** (вынос из Phase 24): full-pool snapshot, БЕЗ
  remap-таблицы по 22 ссылочным типам (remap-путь = скрытая XL, запрещён). M-фаза 8-10 дней.
- **P4. Конверт UICommand сразу в MP-shape**: POD payload + tick-stamp + PlayerID; команды
  несут намерение (FindPath на сим-стороне), entity-ссылки в выделенном поле. Это
  единственная «оплата вперёд» за MP (S), всё остальное — стоп-лист.
- **P5. Data-residency** (SampleHeight + N-окон или дальняя модель боя) — явный пункт
  universal-sim в Phase 24. Вперёд ради MP не тянуть.

## Workstreams

### WS-A. Гигиена + коммит 18.5 + синхронизация доков — СЕЙЧАС, 1-2 дня
1. Закоммитить Phase 18.5 (владелец, 2-4 логических коммита: components / systems / ui / main).
2. Удалить: systems/vision.go (сначала вынести audioPaceMul/audioPostureQuiet/audioBaseRadius/
   visionMaxRange в systems/audio_detect.go — их импортирует contact_detect.go),
   ui/pie_menu.go, `.claude/group/`. Дедуплицировать ContactAgeAlpha (ui/map_contacts.go:10).
3. Однострочник: ctx.Tier-гейт в ContactSystem.Update (systems/contact.go:133) — чинит
   двойной detect-pass и 2× ход часов FoW-fade.
4. Faction-гейт в LevelVisibilitySystem (systems/level_visibility.go:61) — вражеский патруль
   перестаёт расфоговывать здания игроку.
5. Синхронизировать ROADMAP (16/16.5/17.8/17.9/18-core/18.5 закрыты; TAILS-раздел с адресатами
   хвостов; мини-фаза 18.9; residency-пункт в 24; MP-позиция из DP-1) и CLAUDE.md (VisualEvents).
6. Clamp аномального dt в core/app.go::Tick (закрывает ISSUES.md #2 — Tab-лаг).

Gates: `go build ./...` после каждого коммита; headless ai_*-сцены; FoW-fade визуально (возраст
контакта неотрицателен). MP-доплата: ноль.

### WS-B. Детерминизм-слой (одна машина) — СЕЙЧАС → до 17.7, 5-8 дней
1. Гонка ORCA: SpatialEntry += Vel/VelocityYaw/Radius (снапшот в serial rebuild),
   unit_movement_step.go:125-153 читает соседей из hash.entries, не из живых posMap/motionMap.
2. contact_detect.go: mutex-мерж → workerContacts [][]contactRec по индексу воркера
   (паттерн WeaponSystem); порядок NewEntity стабилен run-to-run.
3. nav_service.go:439: BFS-сиды из range-по-map → слайс с сортировкой по ключу NavNode.
4. Единый SimNow (float64) + TickIndex (uint64) в core.UpdateContext; удалить ~15 приватных
   elapsed-аккумуляторов; SquadService.Clock() → канонический.
5. Fixed timestep: accumulator, sim-tick 60 Hz, TimeScale = N целых тиков/кадр;
   LOD-интервалы → счётчики тиков; унифицировать с headless-веткой (main.go:877 уже 1/60).
6. Replay-harness: headless ai_*-прогон, FNV-хэш состояния каждые 100 тиков, два прогона →
   побайтовое равенство. Постоянный гейт всех последующих рефакторингов.

НЕ делать: SimID-замену entity-ID-хэшей (speedJitter, rngSeed) — бессмысленно, пока история
спавнов камеро-зависима; отложено до residency (Phase 24).
Gates: `go test -race ./core/`; `go run -race . -workers=4` 30 с движения — ноль репортов;
replay-хэши равны; Ctrl+P медианы систем не выросли.
MP-доплата: ноль — всё оплачено SP-причинами (воспроизводимые баги, честные тесты).

### WS-C. Командная воронка: UICommand → PlanOp → EventBus — окна 17.7 / хвост-18 / 21, 10-14 дней
Перед 17.7 (UICommand, ~5-6 дней, 9 коммитов):
1. Тип UICommand (tagged union) + очередь + dispatcher в начале sim-тика; первый клиент —
   RMB-приказы (command.go:393/491 — уже воронка, нужен конверт).
2. Soloist-ветка → SquadService.OrderSolo (намерение; FindPath на сим-стороне) — убирает
   client-baked waypoints из command.go:402-417/504-519 и H-stop.
3. applyPreservedSlots → внутрь SquadService.CreateFromUnits (snapshot позиций в момент
   применения); T/U → команды.
4. SetFormationKind (F1-F4 + formation_editor.applyKind), MovementProfile/Posture ([/]/'),
   PlaceIndividual/ClearIndividual (input.go:161 + inspector_unit.go:92).
5-6. inspector_standing_rules: 15 чипов → одна команда SetRule{field, value};
   formation editor: commit-on-release; FormationPresets → клиентский профиль (player-данные).
7. ContactService: схлопнуть mailbox-путь (main.go:2368-2416) и дубль ctx-menu
   (main.go:2545-2601) в ClassifyContact/DeleteContact; глобалы-ящики удалить
   (CameraFocusRequest остаётся client-state — камера не сим).
8. StampTerrain{pos, kernel, params} (X-кратер; Phase 21 размножит класс) + SetTimeScale
   (хоткеи и TopBar зовут одну команду).
9. test_scene_* переводятся на очередь — бесплатный smoke-тест.

Перед хвостом 18: PlanOp{Apply, Inverse} поверх SquadService (тривиален, когда мутации уже
перечислимы); DP-3 — OrderChain linked list → массив на Squad.
К 21: EventBus ring buffer — sim-факты (shot/impact/KIA/order-state) отдельно от спавна
визуалов; первый консьюмер — event log.
Gates: replay-хэши не изменились; grep-гейт «в ui/ нет записей sim-компонентов из draw»;
ручной прогон всех input-паттернов.
MP-доплата: tick-stamp + PlayerID в конверте (S) + дисциплина entity-ссылок. Сериализацию и
транспорт НЕ строить.

### WS-D. Декомпозиция main() + render/ + View — до 17.5, 6-10 дней
Порядок (каждый шаг = компилируемый вечерний коммит; ВСЁ в package main до ш.6):
1. boot_resources.go + boot_services.go (registerResources → *worldRes; defer
   FlushModifiedChunks ОСТАЁТСЯ в main(), LIFO не трогаем).
2. boot_systems.go — табличный registerSystems (порядок пайплайна фиксируется кодом).
3. boot_world.go — spawnStartupEntities + структура gameMaps (~30 Map-хэндлов).
4. boot_render_handles.go — фильтры + HitTester + ghostCtx + orderMarkerCtx; починить
   per-frame аллокации (ecs.NewMap[LevelNavGrid] main.go:2198, hiddenLevels, isSelected → set).
5. **uiSession** — разлом closure-мегаскоупа (Selected/Hovered/RMBState/Marquee/MapCam/...).
6. buildMapCtx()/buildInspectorCtx() вместо 5 дублей литералов; consumeUIRequests() → одна
   точка (сюда встанет UICommand из WS-C).
7. Game-структура + frame-методы: frame_chrome / frame_input / frame_render3d / frame_ui2d;
   main() ≈ 150 строк. Input-гейты (chromeBusy → … → rmbState) не дробить дальше.
8. Пакет render/ (render_world/buildings/units/overlays); View{Camera, OriginChunk,
   **ViewerFaction**} параметром; глобалы systems.CurrentCamera/CurrentOriginChunk удаляются.
9. components/ без raylib: rl.Vector3/Color → components.Vec3/RGBA в ~10 компонентах
   (прецедент Vec3 — nav.go:108); открывает headless-сборку.
10. BuildingViewMode → клиентский view-store (per-player презентация уходит из shared-мира).

Анти-ходы: не выносить оркестрацию в пакет game//cmd/rts (defer-лестница), не отдавать сборку
ctx в ui/ (цикл ui↔systems), не двигать файлы до шага 5.
Gates: replay-хэши те же (рендер не трогает сим); `go list -deps ./components | grep -c raylib == 0`;
скриншот-сравнение; полный ручной прогон UI.
MP-доплата: ViewerFaction в View — подготовка ко «второму взгляду» за ~0.

### WS-E. Sim-структура — перед 19 (ш.1-2), до 22 (ш.3-5), Phase 24 (ш.6)
1. (перед 19) OnGround marker + LocomotionClass в NavOpts; второй SpatialHash для техники.
2. (перед 19) Contact cap + LRU-выселение Unknown; ключ реестра сразу (faction, tracked).
3. (до 22) N-якорный стриминг: все LODAnchor (сейчас first-match+break,
   terrain_streaming.go:163-173), evict по min-Chebyshev вне ВСЕХ окон, spawn = union колец.
4. (до 22) Оживить Garrison-пин + снятие LODAnchor по завершении приказа (сейчас течёт).
5. (до 22) LODSystem: min-dist по всем якорям (S-патч).
6. (Phase 24, сейчас только текст в ROADMAP) data-residency: SampleHeight(wx,wz) =
   chunk-heightmap если загружен, иначе GroundHeight + derivable-срезы; выбор
   «N-окон вокруг активных сил» vs «упрощённая дальняя модель боя». Заодно SampleHeight чинит
   GroundStick-на-Modified-рельефе (юнит в траншее).
Gates: сценарий «Garrison за 500 м» исполняется без подвоза камеры; профайлер в бюджете;
race-прогон при двух окнах.
MP-доплата: N-якорей = будущий union-of-interest, но оплачен Phase 22 / Garrison-багом.

### WS-F. Save/Load — мини-фаза 18.9, после хвоста 18, перед 19; 8-10 дней
P-решение: full-pool snapshot (ориентир ark-serde, но свой бинарный кодек), remap НЕ строить;
чанки остаются в WriteChunk/ReadChunk v1.
1. Классификационная таблица «тип → save/skip/custom» (~105 компонентов + 18 ресурсов,
   spec-table c compile-time exhaustiveness). Skip: Particle/MapPing/ThreatSource/маркеры/
   ChunkMesh/scratch. Derivable: 9 индексов с существующими rebuild-путями.
2-3. Кодеки: memcpy для ~80% fixed-size POD; custom для 5-6 ресурсов (StreamingMap,
   BuildingPlanIndex, симвология → save/symbols.json — закрывает deferred 18.5).
4. Null-on-save для derived-ссылок (AssignedCover/AssignedSlot/TransitionRegistry —
   SurvivalInstinct перевыберет за тик); Level-сущности в снапшот (FoW едет бесплатно).
5. Post-load rebuild: chunks → building children → spatial bake → registries (порядок!).
6. Full-cycle тест: save в середине headless-боя → load в чистый процесс → ≥1000 тиков.
Gates: full-cycle скрипт становится постоянным гейтом; replay-хэш непрерывен через границу
save/load. MP-доплата: кодеки писать per-component (delta-слой ляжет сверху, если дверь
откроется); NetID/delta/interest — НЕ строить.

### WS-G. MP-дверь: документ + belief-store — сейчас (текст) / M0-19 (DP-4) / начало 22
1. (WS-A) Зафиксировать в GAMEDESIGN: lockstep закрыт; опция = co-op/host-auth listen-server;
   dedicated server не цель; стоп-лист (NetID, delta-кодеки, interest management,
   VisualEvents-фильтрация по зрителю, кросс-арч математика).
2. (M0 Phase 19, если DP-4 = да) Controller{OwnerID} ≠ Faction.ID; явный Faction на каждом
   комбатанте (zero-value-трюк «нет Faction = игрок» умирает); ~10 литералов FactionPlayer
   (contact_detect.go:198, main.go:1907, command.go:50) + спавн-сайты.
3. (начало 22) Per-faction belief: ключ ContactRegistry (faction, tracked); убрать drop
   не-игроковых наблюдателей в appendContactIfHostile (detect уже per-seer, несёт obsFactID —
   менять только storage/upsert); relative-affiliation таблица вместо абсолютного Hostile.
   Без этого Phase 22 AI либо всевидящий, либо слепой.
4. (начало 22, DP-6) LevelVisibility: AI-only или per-faction каналы.
Gates: ревью документа; headless-сцена, где AI-фракция копит СВОИ контакты; FoW игрока без
регрессий по replay-хэшам.

## Decision points

- **DP-1 (сейчас):** подтвердить «кросс-машинный детерминизм не цель» → lockstep закрыт,
  MP-опция = co-op/host-auth. WS-B делается в любом случае (SP-ценность).
- **DP-2 (перед 17.7):** StampTerrain и SetTimeScale входят в первый UICommand-enum?
  (рекомендация: да, оба S); FormationPresets → клиентский профиль? (да).
- **DP-3 (перед хвостом 18):** OrderChain linked list → массив [N]ecs.Entity на Squad?
  (рекомендация: да, в одном проходе с PlanOp; упрощает undo/сериализацию).
- **DP-4 (до Phase 19, главная развилка):** co-op на горизонте 1-2 лет нужен?
  Да → Controller≠Faction в M0 Phase 19 (S-M сейчас, M-L после 19-21). Нет → не платить.
  **Решено 2026-07-19: да** → Controller≠Faction входит в M0 Phase 19 (PHASE-19.md P9).
- **DP-5 (после хвоста 18):** WS-F до Phase 19 или после 19/20? (рекомендация: до — каждая
  фаза добавляет типы в таблицу, а сейв-гейт охраняет все последующие фазы).
- **DP-6 (до Phase 22):** belief-store AI-only или PvP-ready? (рекомендация: AI-only,
  но ключ (faction, tracked) сразу).
- **DP-7 (планирование Phase 24):** residency-дизайн (N-окон vs дальняя модель) — решать
  по фактическому использованию окон Phase 22 AI.

## Критерии закрытия

1. Дерево чисто: 18.5 закоммичен; vision.go / pie_menu.go / .claude/group/ удалены;
   ROADMAP синхронизирован (18.9, residency, MP-позиция).
2. `go test -race ./core/` зелёный; race-прогон толпы — ноль репортов.
3. Два headless-прогона → побайтово равные хэши каждые 100 тиков (replay-harness — гейт).
4. Sim-тик 60 Hz; grep не находит приватных elapsed-аккумуляторов; одни часы SimNow/TickIndex.
5. 100% sim-мутаций из ввода — через UICommand; в ui/ нет записей sim-компонентов из draw;
   mailbox-глобалы удалены; soloist-приказы несут намерение.
6. main() < 300 строк, main.go < 700; пакет render/ существует; CurrentCamera/OriginChunk
   заменены View; components/ без raylib.
7. Save/load full-cycle ≥1000 тиков; remap-таблицы не существует в кодовой базе.
8. Garrison за пределами окна исполняется (N-якоря живы, пин снимается).
9. Contact-население ограничено cap'ом; PlanOp даёт undo/redo (хвост 18 закрыт).
10. Phase 22 стартует с per-faction ContactRegistry.

## Стоп-лист (не делать, даже если чешется)

Пакет engine/ и обёртки над raylib; обобщение core.App (DI/reflection/plugins); распил
SquadService до Phase 20; big-bang нарезка systems/ (только worldgen+terrain, и только при
реальной боли навигации); multi-owner Order до design-spike Phase 20; universal-sim миграция
«заодно»; NetID / delta-кодеки / interest management / транспорт до пост-24 продуктового
сигнала; кросс-арч детерминированная математика; Scene-интерфейс до 4+ сцен; конверсия
CurrentCamera в Resource как отдельный проект (только внутри WS-D ш.8, если потребуется).
