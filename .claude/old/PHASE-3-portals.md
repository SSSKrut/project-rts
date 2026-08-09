# Phase 3 — рабочий план

Здания и подземные пространства как **отдельные логические слои** ECS-мира. Поверхность остаётся слоем 0 (heightmap-чанки). Здания/бункеры — графовые сущности на слоях 1+. Связь между слоями — `Portal`. Smart-Object компоненты на узлах здания заводятся как «данные для будущих потребителей» (Phase 4 NavGrid, Phase 5–7 ИИ); сейчас никто их не читает.

ROADMAP — высокоуровневый трекер. Этот файл — рабочий, обновляется по ходу выполнения.

**SDF / воксельная деформация перенесена дальше** (Phase 3b в roadmap, после первых снарядов / инженерных машин — без них от воксельных кратеров нулевая отдача). FoW через порталы — Phase 3c, зацепится за тактический ИИ. В этой фазе их не делаем.

---

## Решения, которые лочим до начала кода

**Q1. Слой как поле `WorldPos`, не отдельный компонент.**
Добавляем `Layer LayerID` (`type LayerID int16`) в `WorldPos`. Слой 0 = surface, по умолчанию — то, что есть сегодня. Альтернатива «отдельный компонент `LayerOf{ID}`» отвергнута: каждое сравнение позиций становится двойным lookup'ом, а это внутренний цикл и LOD, и аудио, и рендера. Поле в `WorldPos` стоит 2 байта на сущность — копейки.

Размер `WorldPos`: было 20 байт (`ChunkCoord 8 + Vec3 12`), стало 22 → выровняется до 24. В кеш-линию по-прежнему попадают ~2 позиции, ничего не ломается.

`WorldPos.Add(v)` и `Sub` **не трогают** `Layer`. Перемещения остаются внутри слоя; смена слоя — только через `Portal` (см. Q5).

**Q2. Семантика слоёв.**
- `0` — surface. Только сюда стримятся terrain-чанки. Все существующие сущности живут здесь.
- `1..N` — building / underground. Только графовое представление: `Room`, `Doorway`, `Stairs`, `Window`. Никаких heightmap-чанков, никаких операций `GroundStick` / `Stamp` / стриминга чанков.
- Отрицательные значения — резерв (например, `-1` под «scratch / debug»).

**Q3. Реестр слоёв — ECS-ресурс.**
Singleton `LayerRegistry{ Layers map[LayerID]*LayerInfo }`. `LayerInfo` хранит:
- `Kind` (`LayerKindSurface` / `LayerKindBuilding` / `LayerKindUnderground`).
- `Root ecs.Entity` — корневая сущность здания/пространства (для итерации его узлов и метаданных).
- `Bounds *AABB` — опциональная bounding-box на surface, для рендера footprint'а здания и для будущего LOD-стриминга графа.
- Reserved-поля: `OcclusionMode` (полностью заслонять surface на этом слое или нет), и т.п. — не реализуются сейчас.

Регистр инициализируется в `main.go` до `InitUI`, как `StreamingMap` и `TerrainChunkIndex`.

**Q4. Все системы, чувствительные к слою — однообразный паттерн.**
Системы, оперирующие парами «якорь vs прочие» (`LODSystem`, `SpatialAudioSystem`), берут `anchor.Layer` и сравнивают **строго равенство** при оценке дистанции до сущностей. Сущности на других слоях для них как будто не существуют — пропускаются без затрат.

`TerrainStreamingSystem` / `GroundStickSystem` / `TerrainGenSystem` / `TerrainMeshSystem` / `TerrainLoadSystem` / `Stamper` не касаются ничего кроме слоя 0. Они получают свои Filter'ы с `Without[LayerNonZero]`?  Нет, проще: фильтры остаются как есть (`TerrainChunk` маркер уже эксклюзивен слою 0 по построению — никто не сделает `TerrainChunk` на слое 1). Дополнительная защита — assert в Stamper'е при `center.Layer != 0`.

**Q5. Portal — простая телепортирующая сущность.**
Сущность с компонентом `Portal{ ToLayer LayerID, ToLocal Vec3, ToChunk ChunkCoord, Radius float32 }`. Лежит на каком угодно слое. `PortalSystem` каждый тик ищет якорь в радиусе порталов; при попадании переписывает `anchor.WorldPos` на `(ToChunk, ToLocal, ToLayer)`. Cooldown — 0.5 сек на якорь, чтобы не дёргался между двумя порталами в одной точке. Cooldown хранится как поле в системе (`map[ecs.Entity]time.Duration`), не как компонент.

**Симметрия порталов** — ответственность того, кто создаёт здание: на каждый «вход с поверхности → дверь комнаты» обычно ставится парный «дверь комнаты → выход на поверхность». Автоматическая парность — работа Phase 12 (контент-пайплайн), не наша.

**Q6. Здание = корневая сущность + детишки на отдельном слое.**
- `Building{ Layer LayerID, Footprint AABB, Name string }` — корневая сущность; сама лежит на слое 0 (footprint показывается на surface).
- `Room`, `Doorway`, `Stairs`, `Window` — обычные сущности с `WorldPos` (Layer = здание-слой) и пустыми маркерами одноимённых типов.
- Связи: `RoomOf{ Building ecs.Entity }`, `Connects{ A, B ecs.Entity }` — двунаправленная связь между двумя `Room` через `Doorway` или `Stairs`. На сущности-двери / лестницы.

Pathfinding между комнатами в Phase 4 (NavGrid). Сейчас граф просто статически декларирован — ECS-сущности с компонентами связи.

**Q7. Smart-Object компоненты — данные без потребителей.**
- `Occupancy{ Max, Current uint8 }` — на `Window`, `Doorway`, и в будущем на любых cover-slots. `Current` пока всегда 0; никто его не инкрементирует.
- `CoverDirection{ Dir Vec3 }` — единичный вектор, **откуда** направление защищено стеной/препятствием.
- `ShootingArc{ Forward Vec3, HalfAngleRad float32 }` — куда позволено стрелять (например, из окна — наружу в полусферу).

В этой фазе они только записываются на тестовом бункере. ИИ заберёт их в Phase 4–7. Никаких систем, читающих эти компоненты, в Phase 3 нет.

**Q8. Камера всегда на том же слое, что и её таргет.**
`OrbitSystem` после вычисления offset'а копирует `pos.Layer = target.Layer`. Это значит, что когда якорь телепортируется через портал, камера через тик окажется в том же слое, и render-фильтр (Q9) автоматически переключится.

`CameraSystem` дополнительно публикует `CurrentCameraLayer` (по аналогии с `CurrentOriginChunk`), чтобы render-цикл в `main.go` мог отфильтровать чужие слои без захода в World.

**Q9. Рендер фильтрует по `CurrentCameraLayer`.**
Все query-фильтры в render-loop'е `main.go` берут только сущности `pos.Layer == CurrentCameraLayer`. Реализация: внутри цикла сравнить `pos.Layer` и пропустить чужие. Делать это через ECS `Filter` с динамическим параметром нельзя — Layer не маркер; через runtime-сравнение — самый простой путь. Профилировать смысла нет: рендер уже идёт по square-ring чанкам (десятки штук) и по unit-фильтрам (сотни). Лишний int16-compare на сущность не виден.

**Q10. Тестовый бункер захардкожен в `main.go`.**
Чтобы у Phase 3 был визуальный output, в конце инициализации `main.go` создаётся демо-здание: 2 комнаты на слое 1 (`Room A`, `Room B`), `Doorway` между ними, окно с `ShootingArc`, лестница на слой 2 (`Room C`), портал на surface рядом с якорем (Layer 0 → Layer 1, дверь `Room A`), и обратный портал в `Room A` (Layer 1 → Layer 0). Координаты подобраны так, что footprint виден неподалёку от спавна.

Build / placement / разметка — Phase 12 (контент-пайплайн с OSM, raycast-сканер). Сейчас просто чтобы было что тестировать.

**Q11. Что осознанно НЕ делаем в Phase 3.**
- SDF / воксельная деформация (Phase 3b позже).
- Fog of war через порталы (Phase 3c).
- ИИ-потребители Smart-Object данных (Phase 4–7).
- Авторазметка зданий из готовых моделей (Phase 12 — vertex-color tagging / raycast scanner).
- Мульти-этажный pathfinding (Phase 4 NavGrid).
- Коллизия со стенами (Phase 5/6 — пока якорь и порталы только телепорт, никакой непроходимости стен).
- Срез этажей / полупрозрачные верхние слои (Phase 11 UI/UX).
- Сериализация графа здания на диск (нужно сначала решить, как делать procgen vs ручную разметку — Phase 12).
- Procedural placement зданий (Phase 12).
- Анимация перехода через портал (Phase 13 полировка).

---

## Четыре мильстоуна

### M3.1 — `LayerID` в `WorldPos` + рендер-фильтр

**Цель.** Инфраструктура слоёв, не ломая сегодняшнего поведения. Без зданий — просто всё что есть переезжает на слой 0, и debug-клавиша подтверждает, что фильтр рендера действительно изолирует слои.

- `components/worldpos.go`:
  - `type LayerID int16`. `const LayerSurface LayerID = 0`.
  - Расширить `WorldPos` полем `Layer LayerID`.
  - `Normalize`, `Add`, `Sub`, `ToRenderSpace` — `Layer` неизменяется и в render-space не участвует.
  - `Distance` / `DistanceSquared` — **не меняются**. Cross-layer дистанция формально считается; ответственность за «не считать» — на вызывающей стороне.
- `components.NewLayerRegistry()` (новый файл `components/layers.go`): `LayerRegistry{ Layers map[LayerID]*LayerInfo }`, `LayerInfo{ Kind, Root, Bounds }`. Регистрируется в `main.go` через `ecs.AddResource` рядом со `StreamingMap` и `TerrainChunkIndex`. На M3.1 в реестре только запись `0 → LayerInfo{Kind: Surface}`.
- Системы, добавляющие layer-фильтр в pair-loop'ах:
  - `LODSystem`: пропуск, если `pos.Layer != anchorPos.Layer`.
  - `SpatialAudioSystem`: то же самое (источник на чужом слое не слышим).
  - `TerrainStreamingSystem` / `Stamper`: assert / no-op, если anchor/center.Layer ≠ 0. Логировать предупреждение в Stamp'ере, не падать.
- `OrbitSystem`: после `target.Add(offset)` — `pos.Layer = target.Layer`.
- `CameraSystem`: публикует `var CurrentCameraLayer components.LayerID`.
- `main.go`:
  - render-цикл фильтрует по `CurrentCameraLayer` (in-loop check, не Filter).
  - Debug-клавиша `L` — переключает `anchorPos.Layer` между 0 и 1 (циклически: 0 → 1 → 0). Пока зданий нет, на слое 1 видно пустоту с камерой; ничто не рисуется. Это и есть тест: рендер действительно изолирует.
  - HUD выводит `Layer: N`.

**Проверяем.**
- Запуск без правок поведения → как до Phase 3.
- Нажал L → видно пустоту, кубики и terrain исчезли. Нажал L → всё вернулось.
- Якорь в layer 1 — `Stamper` (X) пишет warning в stdout, ничего не делает.
- Sanity: WASD продолжает работать на любом слое (двигает якорь в plane его текущего слоя).

### M3.2 — Building / Room / Doorway / Stairs / Window + тестовый бункер

**Цель.** Графовое здание в ECS, видно как набор debug-точек/линий в собственном слое.

- `components/building.go`:
  - `type Building struct { Layer LayerID; Footprint AABB; Name string }` (AABB как уже знакомая структура — если её нет, добавить тут же: `type AABB struct { MinChunk, MaxChunk components.ChunkCoord; MinLocal, MaxLocal rl.Vector3 }` упрощённую — на surface footprint обычно умещается в один чанк).
  - Маркеры: `Room`, `Doorway`, `Stairs`, `Window` — пустые struct'ы.
  - Связи: `RoomOf{ Building ecs.Entity }` (на `Room`), `Connects{ A, B ecs.Entity }` (на `Doorway` / `Stairs`). Во избежание зацикливания вершины сами связи **не хранят** — связь хранится в edge-сущности.
- `systems/building_debug.go`:
  - `BuildingDebugSystem` — `LODPolicy{Active: 0}`. Не мутирует ECS — только публикует render-данные в плоский буфер `var BuildingDebugDraws []DebugDraw` (структура `{Pos rl.Vector3; Color rl.Color; Kind: Sphere/Line/Text}`). Render-цикл в `main.go` после основной геометрии проходится по этому буферу и рисует.
  - Простейшая версия: для каждой сущности с `Room|Doorway|Stairs|Window` маркером и совпадающим слоем — точка соответствующего цвета (Room — серый, Doorway — жёлтый, Stairs — оранжевый, Window — голубой). Для каждого `Connects{A,B}` — линия между ними.
- `main.go` — функция `spawnTestBunker(world, registry, anchor)` (или встроено): создаёт `Building` на слое 1 у footprint около якоря; 2 `Room` на слое 1 (одна возле двери, одна вглубь), `Doorway` между ними с `Connects{A, B}`, `Window` с `ShootingArc` (см. M3.4) на одной из стен `Room A`, `Stairs` ведёт на слой 2 в `Room C`. На surface (слой 0) рисуем footprint квадратом (debug, чтобы было видно где здание).
- Регистрация слоёв 1 и 2 в `LayerRegistry` при создании бункера.

**Проверяем.**
- Запуск → на surface видно квадрат-footprint бункера. Нажал L → попал на слой 1, видны точки `Room A`, `Room B`, `Doorway`, `Window`, линии связей. Нажал L дважды (0→1→2 после расширения L под M3.2) — на слое 2 видна `Room C`.
- Streaming не сходит с ума, FPS не проседает.

(После M3.2 кнопка `L` циклится через все зарегистрированные слои в `LayerRegistry`, не только 0/1.)

### M3.3 — `Portal` и переход между слоями

**Цель.** Якорь физически переходит между слоями, проходя сквозь портал. Камера автоматически следует.

- `components/portal.go`: `Portal{ ToLayer LayerID; ToChunk ChunkCoord; ToLocal rl.Vector3; Radius float32 }`.
- `systems/portal_system.go`:
  - `PortalSystem` — `LODPolicy{Active: 0}`. Filter: `Portal + WorldPos`.
  - Каждый тик: найти якорь, для каждого портала с `pos.Layer == anchor.Layer` посчитать дистанцию якорь-портал (внутри слоя); если ≤ `Radius` И якорь не в кулдауне → переписать `*anchor.WorldPos = WorldPos{Chunk: portal.ToChunk, Local: portal.ToLocal, Layer: portal.ToLayer}`, занести якорь в `cooldown[anchor] = elapsed + 500ms`.
  - Cooldown держится в self-state системы (`map[ecs.Entity]time.Duration`). Чистится лениво — при следующем переходе.
- В `spawnTestBunker`: создать пару порталов:
  - На surface (слой 0) у footprint — `Portal{ToLayer: 1, ToChunk: ..., ToLocal: <coord of Room A center>}`.
  - В Room A — `Portal{ToLayer: 0, ToChunk: ..., ToLocal: <coord 2m снаружи от первого портала>}`.
- HUD строка дополняется текущим Layer'ом (если ещё не был дополнен в M3.1).

**Проверяем.**
- Подойти WASD к точке портала на surface → автоматический переход на слой 1, видны debug-точки `Room A/B`. Камера на месте, орбитит вокруг якоря.
- Подойти к обратному порталу в Room A → переход обратно. Без щёлкания (cooldown работает).
- WASD на слое 1 двигает якорь, координаты в HUD меняются.
- При переходе через слой `Stamper.X` всё ещё пишет warning (срабатывает только на слой 0); тест из Phase 2 не сломался.

### M3.4 — Smart-Object компоненты на узлах

**Цель.** Окно, дверь и узлы укрытий несут метаданные, нужные ИИ Phase 4–7. Никаких потребителей сейчас — только запись и debug-визуализация.

- `components/smart_objects.go`:
  - `Occupancy{ Max, Current uint8 }`
  - `CoverDirection{ Dir rl.Vector3 }` — единичный вектор, **откуда** защита (т.е. с этого направления стена за спиной).
  - `ShootingArc{ Forward rl.Vector3; HalfAngleRad float32 }` — `Forward` — единичный вектор «куда смотрит» окно/амбразура; `HalfAngleRad` — половина угла раскрытия.
- В `spawnTestBunker`:
  - На `Window` — `Occupancy{Max: 2}`, `CoverDirection{Dir: внешний_нормаль_стены}`, `ShootingArc{Forward: -внешний_нормаль_стены, HalfAngleRad: 60° в радианах}`.
  - На `Doorway` — `Occupancy{Max: 1}`, `CoverDirection{Dir: вдоль стены}`.
- `BuildingDebugSystem` расширяется:
  - `CoverDirection` — короткая стрелочка от узла в направлении `Dir` (видно «где спина»).
  - `ShootingArc` — две линии в плоскости XZ от узла длиной 2 м, развёрнутые на ±`HalfAngleRad` от `Forward` (выглядит как «галочка» сектора обстрела).

**Проверяем.**
- На слое 1 у `Window` рисуется галочка сектора и стрелка cover-direction.
- Никаких регрессий: M3.1–M3.3 продолжают работать.
- Сборка чистая (`go build && go vet`).

---

## Что считаем «закрытием Phase 3»

- `LayerID` есть в `WorldPos`; render и LOD/audio изолируют слои; всё, что было до Phase 3, продолжает работать с `LayerSurface = 0`.
- Тестовый бункер с 2 этажами и комнатами реализован сущностями ECS на слоях 1/2; debug-рендер показывает граф.
- `Portal` работает: якорь свободно входит и выходит; камера следует; cooldown отсутствует визуально.
- На узлах здания `Occupancy`, `CoverDirection`, `ShootingArc` записаны и видны в debug-рендере; никаких потребителей не появилось — это задел.
- В `ROADMAP.md` Phase 3 → ✅; SDF/voxel и FoW через порталы стоят как Phase 3b и Phase 3c в правильном месте очереди.

---

## Открытые вопросы (решим по ходу или в Phase 4)

- **Геометрия комнаты.** Сейчас `Room` — точка. Чтобы будущий NavGrid строил пути внутри, понадобятся стены / footprint комнаты (полигон или прямоугольник). Решим, когда возьмёмся за NavGrid (Phase 4) — добавлять можно incrementally в существующие сущности.
- **Видимый interior**. Стены / пол интерьера сейчас не рисуются вообще. Стоит ли в M3.2 добавить минимальные boxy-стены чтобы было видно, что ты в комнате — или хватит точек графа? Голос за «оставляем точки до Phase 11». Если визуальный голод сильный — на 5 минут можно набросать `rl.DrawCubeWires` для каждой комнаты.
- **Layer compaction.** Если в будущем будет много слоёв (микро-бункеров на карте, по сотне на регион), `LayerID int16` упирается в 32K. Хватит надолго; смена на int32 — копеечная миграция, когда понадобится.
- **Симметрия порталов.** Сейчас руками. После Phase 12 (контент-пайплайн) автоматизируется при импорте моделей.
