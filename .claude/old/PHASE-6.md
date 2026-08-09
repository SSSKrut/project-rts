# Phase 6 — рабочий план

Spatial intelligence. Слои данных, которые в дальнейших фазах будут читать unit-контроллер (Phase 7), pathfinder техники (Phase 8), тактический ИИ (Phase 10): **NavGrid** (проходимость + cost), **CoverMap** (откуда защищён каждый квадрат), **Cover slots** (точечные позиции укрытия у пропов и стен). Все три бэйкаются на чанке оффлайн при его первом появлении в стриме, не персистятся (детерминированы от world resources), и пересчитываются при возвращении чанка из dormant. Никаких юнитов / Tactical AI / Threat Map в этой фазе — это Phase 7+.

Чтобы было чем глазами тестировать бэйки, добавляем минимальный pathfinder как **сервис-объект** + правый-клик-навигация для якоря. Никаких юнит-контроллеров; якорь движется по найденному пути напрямую (как текущий WASD, только цель ставится мышью). Unit selection / direct-control / Boids — фаза 7.

ROADMAP — высокоуровневый трекер. Этот файл — рабочий, обновляется по ходу выполнения.

---

## Решения, которые лочим до начала кода

**P1. NavGrid — fixed-size массив на сущности чанка, 1×1 м.**

```go
const NavGridSide = 64                  // ChunkSize / 1 m
const NavGridCells = NavGridSide * NavGridSide

type NavCell struct {
    Cost  uint8     // 0 = непроходимо; >0 = относительная стоимость для A*
    Flags NavFlags  // битовая маска: OnRoad | InBuilding | InTrench | NearWater | ...
}

type NavGrid struct {
    Cells [NavGridCells]NavCell
}
```

Хранится как компонент на сущности чанка (как `Heightmap`). Размер: 8 KB/чанк × ~170 живых чанков = ~1.4 MB — тривиально.

Альтернативы отвергнуты:
- **Сущность на клетку** (4096 × ~170 чанков = 700K сущностей) — бессмысленно, все клетки read-only после бэйка.
- **2×2 м сетка** — теряем гранулярность для дверей (1.2 м) и узкой техники (Phase 8). 1 м совпадает с шагом heightmap'а, slope считается по соседним вершинам без интерполяции.
- **Глобальный contiguous grid** — несовместим со streaming (чанки приходят и уходят); per-chunk + cross-chunk seam handling в A* проще.

`Cost` calibrated as relative steps:
```
Дорога:        2
Открытое поле, slope < 0.3:   4
Пересечённая, 0.3 ≤ slope < 0.6:   8
Брод / мокро / в траншее:   16
Непроходимо (slope ≥ 0.6, стена, вода без моста, закрытая дверь, пропа `BlocksMove`): 0
```

A* пересчитывает в реальные единицы при FindPath: cost cell умножается на длину перехода (1 м для NSEW, √2 м для диагоналей).

**P2. CoverMap — fixed-size массив на той же сущности чанка, 1×1 м.**

```go
type CoverCell struct {
    BaseCover uint8   // [0..255] — суммарное «насколько здесь укрытно» (агрегат раскастов)
    DirMask   uint8   // 8 битов — какие из 8 направлений компаса заблокированы (1 = блок)
}

type CoverMap struct {
    Cells [NavGridCells]CoverCell
}
```

**1 м**, не 2 м, как NavGrid — пользователь явно зафиксировал, что для техники малого габарита (Phase 8 — мотоциклы / лёгкие машины) агрегаты на 2 м слишком грубо. Стоимость: 8 KB/чанк, симметрично NavGrid.

Direction mask: 8 компасных направлений, бит N = «луч на N м длиной упёрся во что-то выше пояса». «Что-то» — соседние heightmap-вершины поднявшиеся выше некоторого порога (для пехоты ~1.2 м), плюс пересечение с `WallSegment` (через `BuildingChildIndex`), плюс пропы с `BlocksLOS=true` в радиусе.

`BaseCover` = popcount(DirMask) × 32. Phase 6 не интерпретирует это число — лишь для UI debug-overlay'я. Phase 10 (Cover Evaluation) построит utility-функцию поверх.

**P3. Cover slots — отдельные сущности.**

```go
type CoverSlot struct {
    Host       ecs.Entity   // prop / WallSegment / Floor — у кого «отрос» этот слот
    HostKind   CoverHostKind  // Prop / WallCorner / Window — для разной интерпретации в UI
    OriginDir  rl.Vector3   // единичный вектор, ОТКУДА слот защищает (ровно по полусфере)
    Quality    uint8        // [0..255], pre-computed at bake; Phase 10 переоценит динамически
    Stance     StanceMask   // битовая маска: можно ли тут лечь / сесть / стоять
}

type StanceMask uint8
const (
    StanceProne StanceMask = 1 << iota
    StanceCrouch
    StanceStand
)
```

Сущность слота: `WorldPos + CoverSlot + LODRelevant`. Без `BuildingMember` — слоты не дети здания в смысле жизненного цикла, у них **свой** индекс (см. P4) — но привязаны через `Host`.

Адресация. Для Phase 11 (разрушение) важно уметь O(1) спросить «дай все слоты, у которых Host == X». Это даёт `CoverSlotIndex`:

```go
type CoverSlotIndex struct {
    ByHost map[ecs.Entity][]ecs.Entity   // host root → его слоты
}
```

Резолвинг для здания через два прыжка: `BuildingChildIndex[buildingRoot]` даёт стены/окна/полы → для каждого `CoverSlotIndex.ByHost[wall]` → список слотов. На уровне здания — единый запрос через helper `coverSlotsForBuilding(root) []ecs.Entity`, который и обходит детей и собирает их слоты. Это покрывает destruction-кейс: при удалении здания вызывается helper, чистит все слоты вокруг всех его стен/окон, потом удаляются дети, потом сам root.

Для пропа симметрично: `CoverSlotIndex.ByHost[propEntity]` = слоты этого пропа. При эвикте чанка `TerrainStreamingSystem` дополняется проходом «снять слоты у каждого props/buildingChild в этом чанке» через индекс.

Альтернатива «массив слотов внутри Prop / WallSegment» отвергнута: ломает паттерн «всё, что адресуемо AI-запросом, — сущность», вводит магическую константу «не больше N слотов на пропе», и не даёт filter-запросу «все слоты в радиусе R с углом против threatDir» работать без join'ов через массив.

**P4. Pathfinder — сервис-объект `NavService`, не System.**

По аналогии со `Stamper`. Pre-built handles в конструкторе, public API:

```go
func NewNavService(w *ecs.World) *NavService

func (s *NavService) FindPath(from, to components.WorldPos, opts NavOpts) []components.WorldPos
```

`NavOpts` зашит для Phase 6 одним locomotion-классом «foot». `Locomotion` enum мы введём, но в Phase 6 поддерживаем только `LocomotionFoot`. Phase 8 расширит. API не изменится — добавится поле в `NavOpts`.

Возвращает waypoint'ы в WorldPos (центры клеток вдоль пути), [] если путь не найден (или target в dormant chunk'е), nil если оба конца совпадают. Не делает smoothing'а — это работа steering'а в Phase 7.

A* по multi-chunk grid'у. Глобальный индекс клетки = `(cc.X * NavGridSide + i, cc.Z * NavGridSide + j)`. Соседние клетки через границу чанка → fetch соседнего чанка через `TerrainChunkIndex`; если соседний не загружен → направление считается **проходимым через границу к точке выхода**, но дальнейшее A* зафейлится (мы туда не дойдём). Реалистично: pathfinder работает в загруженной зоне; запрос путя в dormant-зону возвращает [].

Heuristic — Chebyshev distance (диагонали разрешены). Cap на iteration: 50_000 cells (защита от race-condition'ов). Open-set — slice-based binary heap, keyed by f-score.

Альтернатива «System, который раз в N кадров пересчитывает пути всех юнитов» — нет потребителей в Phase 6, и в Phase 7 unit-контроллер сам решит когда вызывать. Service-объект чище.

**P5. Slope-to-cost mapping для NavGrid.**

Для клетки (i, j): четыре corner heightmap-вершины (i, j), (i+1, j), (i, j+1), (i+1, j+1). slope = max попарной разницы / 1 м.

```
slope < 0.30  (≈17°)  →  Cost = 4   (открытое поле)
0.30 ≤ slope < 0.60   →  Cost = 8   (пересечённая)
slope ≥ 0.60  (≈31°)  →  Cost = 0   (непроходимо для пехоты в Phase 6)
```

Phase 8 для техники переопределит пороги (танк 25°, мотоцикл 35°). Phase 6 один класс.

Cost модификаторы поверх slope'а — порядок применения важен, ниже = выше приоритет:
- `OnRoad` flag → `Cost = 2` (перекрывает slope, road-bias всегда привлекает).
- `InTrench` flag → `Cost = 16` (можно пройти, но дорого; пехота снизит, рассматривая как cover, в Phase 10).
- `NearWater` (внутри river-cut'а ниже -0.5 м) → `Cost = 0` (вода блокирует, мост — отдельная история, см. P9).

**P6. Стены / двери / окна / пропы — растеризация в NavGrid.**

После того как `BuildingSystem` и `PropSpawnSystem` отработали в чанке, бэйкер обходит чанк-сущности `WallSegment / Prop` и проставляет `Cost = 0` в задетые клетки.

Алгоритм для `WallSegment`: выбираем bbox стены в world XZ (длина × thickness, повёрнутый на yaw). Для каждой клетки внутри bbox — точная проверка точка-в-прямоугольнике. Помечаем `Cost = 0`. Если у стены `OpeningKind == OpeningDoor / OpeningWindow`, **не помечаем** клетки внутри проёма (позиция: `[OpeningCenterT*Length - OpeningWidth/2, +Width/2]` вдоль стены, по толщине — вся).

Closed door (`Door.State == DoorClosed`) — отдельно: после стен, помечаем клетки проёма обратно в `Cost = 0`. Window — оставляем непроходимым **для движения** (стекло, рама), но в CoverMap он, наоборот, *прозрачен* для LOS-раскаста (P10). Door (open) — проходим.

Для `Prop` с `BlocksMove == true` (`registry.Metas[Type].BlocksMove`): bounding box по `BBoxRadius` — пометка клеток внутри. Тонкая аппроксимация, но `BBoxRadius` ровно для этого и придумывался в Phase 3.

**P7. Road-bias.**

После slope-cost'а проходим по `RoadGraph`-рёбрам, пересекающим bbox чанка. Для каждой клетки: если её центр в пределах `edge.Width / 2` от центральной линии ребра — выставляем флаг `OnRoad`, `Cost = 2`. Bridge-рёбра учитываются: их клетки тоже `OnRoad` (это даёт тот самый road-bias для пехоты на мосту, без чего по мосту никто бы не пошёл — `Cost = 0` под мостом из-за river-cut'а).

Junction-узлы на NavGrid не выделяются — клетки вокруг узла попадают в радиусы инцидентных рёбер и так получают `OnRoad`.

**P8. Trench cells.**

`TrenchSystem` обработал чанк → клетки с центром в полосе `±Width/2` от центральной линии получают флаг `InTrench`, `Cost = 16`. **Не блокируем**: окоп — это ослабленная проходимость, не стена.

Идея: пехота в Phase 10 получит мотивацию «рассматривать `InTrench` клетки как cover, садиться в них и стрелять». Но это уже потребитель, не Phase 6.

**P9. Bake-маркеры и pipeline.**

Один System, два прохода через два маркера (паттерн RoadSystem / BuildingSystem):

- `NavBaked` — NavGrid собран. Filter: `Heightmap + ChunkCoord + Without[NavBaked]`. После bake'а ставит маркер.
- `CoverBaked` — CoverMap собран и cover slots для props/buildings в этом чанке проспавнены. Filter: `Heightmap + ChunkCoord + Without[CoverBaked]`. После — маркер.

Оба прохода в одном `SpatialBakeSystem`. Modified не гейтится — бэйк должен учитывать cur heightmap (потенциально с stamp'ом), а props/buildings уже спавнены отдельными системами.

Pipeline order:

```
streaming → load → gen → river → road → building → trench → prop_spawn → spatial_bake → terrain_mesh → ...
```

`spatial_bake` — после `prop_spawn` (видим пропы), до `terrain_mesh` (никакой связи на самом деле, но симметрично). Он не пишет в heightmap, не ставит `MeshDirty`.

**P10. CoverMap раскасты — что и как считаем.**

Для каждой клетки:

1. Восемь компасных направлений (N, NE, E, SE, S, SW, W, NW).
2. Длина луча: 8 м (фиксированное окно). Шаг — 1 м (8 точек проверки).
3. Высота наблюдателя над heightmap'ом — 1.2 м (приседающий пехотинец; consistent с `Suppression` в DESIGN).
4. На каждом шаге: world (X, Z) → heightmap interpolated → если `terrain_height + 0.5 ≥ observer_height`, направление **заблокировано**.
5. Дополнительно: проверяем пересечение луча с любым `WallSegment` без проёма-окна. Через `BuildingChildIndex` и спатиальный фильтр (только стены чанков в радиусе 1 чанка от текущего). Окна (`OpeningKind == OpeningWindow`) пропускают луч. Закрытые двери (Doors `State==Closed`) блокируют. Props с `BlocksLOS==true` — блокируют.
6. Если хотя бы одна точка вдоль луча даёт «блок» — direction-bit set.

`BaseCover = popcount(DirMask) × 32` (диапазон 0..256, обрезаем до 255).

8 раскастов × 4096 клеток × ~170 чанков = ~5.6M раскастов на старте мира. Каждый раскаст — 8 шагов. Это ~45M heightmap-точечных лукапов. Не realtime, но это **разовый бэйк per chunk**: считаем по ~70K раскастов на чанк × 8 шагов = ~560K шагов на чанк. На современном CPU — миллисекунды. Если станет узким местом — поднимем на пол-секунды spawn-латентности или вынесем в горутину (бэйк per-chunk идемпотентен, пишет только в свою сущность).

**P11. Cover slots — генераторы.**

Три источника, каждый — pure helper-функция возвращающая `[]CoverSlotSpec`:

1. **Prop** с `Cover > 0`. Раскладываем 8 точек по периметру bbox (радиус `BBoxRadius`, 8 равномерных углов). Для каждой: raycast от точки в центр пропа на высоте 1 м; если пересечение — точка валидна, slot создаётся с `OriginDir = (точка - центр).Normalize()`, `Quality = registry.Metas[Type].Cover * 255`, `Stance = StanceCrouch | StanceStand` (или только `StanceCrouch` если высота пропа < 1.5 м).
2. **Window** (WallSegment с `OpeningKind == OpeningWindow`). Один slot ровно на позиции окна, `OriginDir = outwardNormal` (уже есть в Phase 5 в виде `CoverDirection`), `Quality = 200`, `Stance = StanceCrouch | StanceStand`. На этой же сущности уже висит `ShootingArc` — его читает Phase 10, не Phase 6.
3. **Wall corner**. Для каждой пары соседних `WallSegment` одного здания, делящих общую конечную точку (детектится по совпадению world XZ конечных позиций), один slot в углу с `OriginDir = (n1 + n2).Normalize()` где n1, n2 — outward normals двух стен, `Quality = 180`, `Stance = StanceProne | StanceCrouch | StanceStand`.

Slots создаются в одном проходе с CoverMap (тот же `SpatialBakeSystem`, marker `CoverBaked`). Регистрируются в `CoverSlotIndex.ByHost[host]` и в `PropChunkIndex` / `BuildingChildIndex` для ownership-tracking'а — чтобы при эвикте чанка слот ушёл с host'ом единым кодом, без дополнительной логики.

Точное место регистрации:
- Slot для пропа → `PropChunkIndex.Loaded[cc] = append(..., slotEntity)`. То есть слоты пропа попадают в тот же chunk-bucket, что и сам проп; при эвиките чанка они оба удаляются. Дополнительно `CoverSlotIndex.ByHost[propEntity] = append(..., slotEntity)` — для destruction в Phase 11.
- Slot для wall / window / corner → `BuildingChildIndex.Loaded[buildingRoot] = append(..., slotEntity)`. То же самое: эвикт чанка → child уйдёт. Плюс `CoverSlotIndex.ByHost[wallEntity]` (или `cornerHost`).

При destruction отдельного host'а (не эвикт чанка, а Phase 11 разрушение пропа / стены) — helper `removeHostAndSlots(host)`: проходит по `CoverSlotIndex.ByHost[host]`, удаляет каждый slot, потом удаляет host. Никакой дрейф индексов.

**P12. Cross-chunk соседи в A*.**

Cell index = `(globalI, globalJ) = (cc.X * 64 + i, cc.Z * 64 + j)`, оба int32. Для cell ↔ chunk:
```
cc = ChunkCoord{X: globalI >> 6, Z: globalJ >> 6}     // shift 6 = log2(64)
local = (globalI & 63, globalJ & 63)
```

Соседи: 8 направлений (NSEW + диагонали). Получая globalI±1, globalJ±1, считаем cc + local. Через `TerrainChunkIndex` достаём `NavGrid` соседнего чанка.

Если соседний чанк не загружен — direction блокирован (как если бы клетка была за пределами карты). Это означает, что A* не может найти путь, который выходит за пределы стрима. Phase 6 — приемлемо. Phase 8 (длинные пути для техники) добавит «macro-path по `RoadGraph`», который не зависит от стрима.

**P13. Modified-стейл.**

После того как клетка `Cost` посчитана, любой `Stamper.StampHeightmap` (например, `KeyX` крафтер) делает NavGrid stale — fakethe бэйкер не пересчитывает на edit. Принимаем: пользователь видит дыру в земле, NavGrid думает «это всё ещё проходимо». Фикс — Phase 11 (combat/damage) добавит `NavDirty`-маркер, аналог `MeshDirty`, и часть `SpatialBakeSystem` будет умной перебэйкавкой задетых клеток.

В Phase 6 этого осознанно нет: единственный mutate-источник — `KeyX` debug-стэмп, и он stand-alone test для persistence-цикла, а не tactical scenario.

**P14. Persistence.**

Никаких NavGrid / CoverMap / cover slots на диск. Бэйкер детерминированно пересчитывает при каждом возвращении чанка из dormant — ровно как `RiverCut` / `RoadFlatten` / `RectCut` в предыдущих фазах. Формат файла v1 не меняется.

**P15. Locomotion classes — отложено в Phase 8.**

Сейчас один класс «foot». `NavOpts.Locomotion` enum заведём (`LocomotionFoot`), API готов к расширению. Phase 8 либо сделает per-cell мультиклассовый cost, либо ленивую оценку по `Flags + Locomotion`. Контракт `NavService.FindPath` не сломается.

**P16. Что осознанно НЕ делаем в Phase 6.**

- **Multi-floor NavGrid внутри зданий** → Phase 7. Surface-only здесь. Внутри здания первого этажа NavGrid видит стены как блокировки и открытые двери как проёмы — один этаж проходим без правок.
- **Unit selection / direct control** → Phase 7. Якорь по правому клику ходит сам, как player marker; никакого выделения отрядов мышью.
- **Юнит-контроллер, steering, Boids, локальное обхождение** → Phase 7.
- **Threat Map** (плотность угроз по карте) → Phase 13 (Strategic AI).
- **Influence Map** → Phase 13.
- **Cover Shadows от техники** (`DynamicCoverEmitter`) → Phase 10.
- **Cover utility scoring** (выбор лучшего слота под threat-vector) → Phase 10. Quality в Phase 6 — статический pre-bake.
- **Action Masking, doctrines, suppression-aware path planning** → Phase 10/12.
- **NavDirty / live re-bake при `Modified` и при разрушении стен** → Phase 11.
- **Smoothing path'а, splines, anti-zig-zag** → Phase 7 steering layer.
- **CrowdManager**, обход пути юнита другими юнитами → Phase 7.
- **NavGrid для air-units** (вертолёты Phase 8+) — игнор Y, 2D enough; Phase 8 решит.

---

## Восемь мильстоунов

### M6.1 — Типы + сущность чанка + skeleton bake

**Цель.** Все компоненты заведены, пустой бэйкер регистрируется в pipeline, чанк после bake'а имеет привязанный `NavGrid` и `CoverMap` (даже если все клетки cost=0 и все маски нули).

**Делаем:**
- `components/nav.go`:
  - `NavGridSide`, `NavGridCells`, `NavCell`, `NavFlags` enum, `NavGrid`.
  - `CoverCell`, `CoverMap`.
  - `Locomotion` enum (`LocomotionFoot` единственный для Phase 6).
  - Маркеры `NavBaked`, `CoverBaked`.
- `components/cover.go`:
  - `CoverHostKind`, `StanceMask`, `CoverSlot`.
- `systems/spatial_bake.go`:
  - `SpatialBakeSystem` с двумя проходами через два маркера. В M6.1 — заглушки, проставляющие маркеры без работы.
- `systems/cover_index.go`:
  - `CoverSlotIndex` resource + конструктор.
- `main.go`:
  - Резерв `CoverSlotIndex` ресурс до InitUI.
  - Регистрация `SpatialBakeSystem` после `prop_spawn`, до `terrain_mesh` (P9).

**Проверяем.** `go run main.go` запускается без падений, `len(NavGrid)` накапливается = `len(loaded chunks)`, `CoverSlotIndex.ByHost` пустой.

### M6.2 — NavGrid baseline: slope из heightmap

**Цель.** На сцене виден debug-overlay по клавише `N` — каждая клетка раскрашена по проходимости (зелёный → жёлтый → красный → чёрный = непроходимо). Здания, дороги, реки на overlay'е пока не учтены — только slope.

**Делаем:**
- `systems/spatial_bake.go`:
  - В первом проходе для каждой клетки: четыре corner heights из `Heightmap`, slope, cost (P5), запись в `NavGrid.Cells[idx].Cost`.
- `main.go` (render):
  - При зажатой `N` — пройтись filter'ом `WorldPos + ChunkCoord + NavGrid` и нарисовать каждую клетку как маленький прозрачный квад (`rl.DrawCubeV` с alpha) на `surfaceY + 0.05`. Цвет от cost через простой LUT.

**Проверяем.**
- Отсутствие чёрных клеток на ровном поле.
- Холмы (slope > 0.3) — желтоватые.
- Совсем крутые склоны — красные/чёрные.
- Никаких визуальных артефактов на стыках чанков (slope считается одинаково с обеих сторон, потому что соседний чанк делит heightmap-вершины).

### M6.3 — Растеризация стен / дверей / окон / пропов

**Цель.** Здания и пропы появляются на NavGrid'е как блокировки. Двери (открытые) — проходы. Окна — блокировки.

**Делаем:**
- `systems/spatial_bake.go`:
  - Для каждой `WallSegment` чанка (через `BuildingChildIndex` reverse-lookup или filter `WorldPos + WallSegment` ограниченный bbox чанка): определить bbox стены (oriented по yaw, длина × толщина), пометить клетки внутри как `Cost = 0`. Затем — клетки проёма обратно в исходный slope-cost (если `OpeningKind == OpeningDoor && Door.State == DoorOpen` или `OpeningKind == OpeningWindow` для движения — НЕТ, окно блокирует движение, оставляем 0).
  - Для каждого `Prop` чанка: если `registry.Metas[Type].BlocksMove`, пометить клетки в радиусе `BBoxRadius` (просто всё внутри окружности).
- В overlay (M6.2) — те же цвета, теперь видим контуры зданий чёрным.

**Проверяем.**
- Дом виден чёрной рамкой 4 сегмента + светлым внутри.
- Проёмы дверей — светлые «дырки» в чёрной рамке (1.2 м = 1-2 клетки).
- Деревья/булыжники — чёрные точки.
- Якорь (правый-клик в M6.5) обходит здание, не пробует через стену.

### M6.4 — Road-bias + trench-flag

**Цель.** Дорожные клетки имеют пониженную стоимость; A* в M6.5 выбирает путь по дороге даже если он формально длиннее. Окопы помечены как `InTrench`, проходимы.

**Делаем:**
- `systems/spatial_bake.go`:
  - После slope-cost'а и стен, проход по `RoadGraph.Edges` пересекающим bbox чанка (тот же bbox-clip что в `RoadSystem`): для клеток в полосе `±Width/2` — `Flags |= OnRoad`, `Cost = 2`.
  - Аналогично по `TrenchNetwork.Lines` — клетки в полосе `±Width/2` получают `Flags |= InTrench`, `Cost = 16`.
- Overlay расширяется: бледно-синий = `OnRoad`, оранжевый = `InTrench`.

**Проверяем.**
- Дороги — бледно-синие линии по NavGrid'у, проходящие сквозь холмы (потому что road-flatten уже выровнял).
- Bridge-edge — тоже бледно-синий поверх русла (под мостом — обычно непроходимая вода, но на bridge-клетках мы перезаписываем).
- Траншея — оранжевая полоса.

### M6.5 — `NavService` A* + правый-клик debug

**Цель.** Якорь по правому клику начинает идти к указанной точке; путь видно как линию. Это main test-инструмент остальных мильстоунов.

**Делаем:**
- `systems/nav_service.go`:
  - `NavService` с pre-built handles (`TerrainChunkIndex`, `*ecs.Map[NavGrid]`).
  - `FindPath(from, to WorldPos, opts NavOpts) []WorldPos` — A* с Chebyshev-эвристикой, slice-binary-heap open-set, max 50K iterations.
  - Cross-chunk соседи через `TerrainChunkIndex` (P12).
- `main.go`:
  - `navService := systems.NewNavService(app.World)` после остальных систем.
  - В render loop: если `rl.IsMouseButtonPressed(rl.MouseRightButton)` — raycast мыши через камеру в плоскость `Y = surfaceY` (или `Y = anchor.Local.Y`), получить целевую WorldPos. `path := navService.FindPath(anchorPos, targetPos, ...)`. Если непустой — store path в local var, и якорь начинает по нему идти (вместо WASD-input'а; WASD прерывает).
  - Anchor walking: каждый кадр — двигать якорь к `path[0]`, при достижении — `path = path[1:]`.
  - Visualize: `rl.DrawLine3D` между waypoint'ами + `DrawCircle3D` на текущем target'е.

**Проверяем.**
- Правый клик на полу → якорь идёт прямой линией к точке.
- Правый клик за стеной здания → путь обходит здание, якорь обходит за угол.
- Правый клик за рекой → путь идёт через мост (чёрно-жёлтая линия по road-graph debug, синяя по nav-overlay).
- Правый клик на крыше / в стене → `path == nil`, ничего не происходит (якорь стоит).
- Правый клик в dormant зону (за relevant радиусом) → `path == nil`, корректный fail.

### M6.6 — CoverMap bake

**Цель.** CoverMap собран; debug-overlay по `C` показывает какие клетки укрыты с какой стороны.

**Делаем:**
- `systems/spatial_bake.go`:
  - Второй проход (gated `Without[CoverBaked]`).
  - Для каждой клетки: 8 раскастов по 8 м, шаг 1 м (P10).
  - Heightmap-сэмплинг: bilinear interpolation по 4 corner вершинам клетки (точно, без alias'а на стыках).
  - Wall-сэмплинг: filter `WorldPos + WallSegment` ограниченный bbox-чанка ± 8 м, для каждого луча — 2D segment-segment intersection (XZ).
  - Записать `DirMask` + `BaseCover = popcount * 32`.
- Overlay (`C`): на каждой клетке — маленькая 8-сегментная диаграмма (или просто закрасить клетку прозрачным синим с интенсивностью `BaseCover / 255`).

**Проверяем.**
- За стеной здания — clear cover paths (один-два направления `BaseCover` от стены, остальные открытые).
- Возле скал/больших пропов — повышенный `BaseCover`.
- Окна не отбрасывают cover (LOS-прозрачны).
- Двери (закрытые) — отбрасывают.

### M6.7 — Cover slots для пропов / окон / углов стен

**Цель.** Видны slot-маркеры рядом с пропами и зданиями (по клавише `V`).

**Делаем:**
- `systems/cover_slots.go`:
  - Три pure-функции generator'а (P11): `propCoverSlots`, `windowCoverSlots`, `wallCornerCoverSlots`. Каждая принимает host-сущность + её данные, возвращает `[]CoverSlotSpec`.
- `systems/spatial_bake.go`:
  - В CoverBaked-проходе после CoverMap'а: для каждого пропа/окна/угла в чанке — вызов соответствующего генератора, спавн `CoverSlot`-сущностей.
  - Регистрация в `CoverSlotIndex.ByHost[host]` и в `PropChunkIndex` / `BuildingChildIndex` (P11).
- Helper `coverSlotsForBuilding(idx *CoverSlotIndex, bIdx *BuildingChildIndex, root ecs.Entity) []ecs.Entity` для будущего Phase 11 destruction-теста.
- `TerrainStreamingSystem.evict`: при удалении пропа/wall — также чистить `CoverSlotIndex.ByHost[entity]`. (Запросто удалится через iteration, но индекс надо обновить.)
- `main.go` (render):
  - `wallCornerHost`-сущности: по факту они привязаны к одной из двух образующих стен (выбираем меньший entity-id для детерминизма). Создаются в bake.
  - При зажатой `V` — filter `WorldPos + CoverSlot`, на каждом — маленький жёлтый куб + стрелочка `DrawLine3D` в сторону `OriginDir`.

**Проверяем.**
- Каждое дерево / валун / куст с `Cover > 0` имеет 6-8 жёлтых slot'ов вокруг себя.
- Каждое окно — один slot с стрелочкой наружу здания.
- Каждый угол здания — один slot со стрелочкой по биссектрисе.
- Подойти к пропу с любой стороны — рядом по крайней мере один slot, направленный в эту сторону.
- Эвикт чанка → slot'ы исчезают вместе с пропами.
- Возврат → slot'ы те же самые (детерминированный seed).

### M6.8 — Финальный test-pass

**Цель.** Полный сценарий проходим. Pipeline / persistence / clearance — без регрессий.

**Делаем:**
- В `main.go` HUD: `Nav: chunks=N | Slots: live=M | Path: waypoints=W` и т. п.
- Тест-сценарий вручную:
  1. Запуск → правый клик через дом → путь обходит дом.
  2. Правый клик через мост на другой берег → путь идёт по дороге, по мосту, к точке.
  3. Правый клик через траншею → путь старается обойти траншею (`Cost=16` дороже чем поле `Cost=4`); если обхода нет — идёт через окоп.
  4. Стэмп кратера X на дороге → mesh пересобран, NavGrid stale (P13). Принимаем — это не блокер для Phase 6.
  5. Quit → restart → cratere persists, дома persist, но NavGrid пересчитан с нуля: dirt-track-через-кратер по-прежнему имеет cost 2 потому что road-bias накладывается *поверх* slope'а; провал визуально — но проходимый.
  6. Дальний правый клик (за relevant радиусом) → `path = nil`, ничего не происходит.
  7. Большой обход (~60 чанков от спавна и обратно) → `CoverSlotIndex.ByHost` стабилен (число slot'ов на known host'ов не дрейфует), нет утечек сущностей.

**Проверяем.** См. сценарий — без визуальных дефектов, без падений, без растущей памяти.

---

## Что считаем «закрытием Phase 6»

- `go run main.go` показывает мир с дорогами / зданиями / траншеями / реками / props и: правый-клик-навигация ведёт якоря по корректным путям, обходя препятствия и предпочитая дорогу.
- `NavGrid` и `CoverMap` доступны как компоненты на сущности чанка, готовы к чтению из Phase 7 (unit-контроллер) и Phase 10 (TacticalAI / CoverEvaluation).
- `CoverSlot`-сущности с `WorldPos + Host + OriginDir + Quality + Stance`, индексированы через `CoverSlotIndex` (host-keyed) и через `PropChunkIndex` / `BuildingChildIndex` для жизненного цикла.
- `NavService.FindPath(from, to, opts)` — публичное API, готово принимать `Locomotion` enum (расширение в Phase 8).
- Pipeline: `streaming → load → gen → river → road → building → trench → prop_spawn → spatial_bake → terrain_mesh`. Бэйк per-chunk детерминирован, не персистится.
- `helper coverSlotsForBuilding(...)` готов для Phase 11 destruction.
- Debug-overlay'и `N` (NavGrid), `C` (CoverMap), `V` (Cover slots) — для верификации без юнитов.

После этого — обновление ROADMAP, Phase 6 → ✅, переход к Phase 7 (Базовые юниты + unit selection / direct control + multi-floor NavGrid). Что-то из Phase 7 (например, multi-floor) можно начать сразу после M6.5 если возникнет необходимость для отладки внутри здания.

---

## Заметки на полях

- A* через многочанковый grid с разной плотностью (Active full-detail, Relevant — те же 64×64 на чанк, мы не loDим NavGrid). Это значит Relevant-зона навигационно равна Active-зоне. Нагрузка на бэйк в Relevant — те же ~5K раскастов на чанк × ~120 relevant-чанков = ~600K раскастов. Значимо для cold-start, но один раз за чанк.
- Heightmap-вершины разделяются между соседними чанками (вершина (i=64, j) одного чанка = (i=0, j) следующего). Slope-расчёт корректен на стыках без дополнительных операций.
- Wall-rasterization имеет потенциальную проблему «стена строго на границе клетки» — два соседних чанка могут разрастеризовать одну и ту же стену по-разному. В Phase 5 footprint-constraint (здание ⊆ один чанк) делает это невозможным: все стены здания лежат внутри одного чанка, никаких cross-chunk стен. Phase 15 multi-chunk здания будут решать это явным алгоритмом «чья граница owns wall» (предположительно min(cc.X, cc.Z)).
- CoverMap раскасты выходят за границу чанка (8 м). Реализация: при сэмплинге берём heightmap соседних чанков через `TerrainChunkIndex`. Если соседний не загружен — направление считается **открытым** (нет блокирующего рельефа). Это слегка занижает `BaseCover` у клеток на границе loaded-зоны, но это краевая полоса 8 м из 320 м radius'а — глаза не заметят. Phase 13 Influence Map смотрит на агрегированные данные, ему всё равно.
- Cover slot generation для props спавнит 6-8 entity per props × ~60 пропов на лесной чанк = ~400 entity на чанк. На 50 active+relevant таких чанков — ~20K cover-slot entity. Архетип лёгкий (`CoverSlot + WorldPos + LODRelevant`), Ark справится. Если станет узким местом — переход на side-by-side bake (только для пропов с `Quality > threshold`, остальные — без slot'ов до Phase 10 запроса).
- Path waypoint'ы возвращаем как центры клеток. Steering в Phase 7 пройдётся string-pulling'ом и сгладит, но в Phase 6 при ходьбе якоря «по клеткам» это может выглядеть рваным на crowded scenes. Принимаем — debug-якорь, не рендер-критичный.
- `NavCell.Flags` enum в Phase 6 минимален (`OnRoad`, `InTrench`, `InBuilding`, `NearWater`). Phase 8 расширит (`Bridge`, `RoadHighway`/`Local`/`Dirt` для дифференцированного preference у разных vehicle types). Контракт «Flags uint8» оставляет 4 свободных бита, дальше — `uint16`.
- `CoverSlot.Quality` сейчас static. Phase 10 (CoverEvaluation) считает динамическое значение поверх через `dot(OriginDir, threatDir) × Quality × stance_match`. То есть Phase 6 Quality — это «макс достижимый», dynamic — это «фактический сейчас» под конкретный threat. Контракт устойчив к этому.
- Рассматривали третий debug-overlay для слотов — `V`. Не конфликтует с движениями (W, A, S, D в WASD), не перекрывает `G` (road graph), `N` (NavGrid), `C` (CoverMap). Если потребуется ещё — `B` или `M`.

### Что прокидывается дальше как pre-flight

Эти пункты сознательно отложены, и каждый из них имеет конкретного потребителя в следующих фазах. Перечисляю явно, чтобы при заходе на эти фазы не «всплывало» как сюрприз:

- **`coverSlotsForBuilding(idx, bIdx, root)` helper** — заложен здесь (P3, P11), но никто в Phase 6 не вызывает. Потребитель — **Phase 11** (combat / destruction): когда здание разрушается, helper достаёт все cover-slot сущности всех его стен/окон/углов через `BuildingChildIndex` + `CoverSlotIndex.ByHost`, и они зачищаются единой пачкой до того, как удалятся сами child'ы и root. То есть destruction-pipeline в Phase 11 вызывает helper первым.

- **CoverMap edge-band 8 м** — клетки в полосе 8 м от границы loaded-зоны имеют слегка заниженный `BaseCover` (раскасты в dormant-направления возвращают «открыто»). Принимаем как краевую ошибку. Потребитель — **Phase 10** (CoverEvaluation utility AI): если глаза покажут, что юниты неоправданно ныряют в боевые позиции у самого края зоны, перейдём на «считать раскасты в dormant как блокированные» (overcounting вместо undercounting). Решение откладываем до момента, когда Phase 10 покажет, что это реально проблема.

- **Modified-stale NavGrid** — `KeyX` крафтер в Phase 6 не триггерит re-bake. Принимаем. Потребитель — **Phase 11** (повреждения): тогда же добавим `NavDirty` маркер на чанке, аналог `MeshDirty`, и часть `SpatialBakeSystem` будет переписана на инкрементальный re-bake задетых клеток. До этого момента (Phase 7-10) единственный источник стэмпов — debug-крафтер, и юниты не должны попадать в дыру.

- **Multi-floor NavGrid внутри зданий** — Phase 6 даёт surface-only grid; внутрь здания юнит зайдёт через дверь и будет ходить по первому этажу (стены = блок, открытые двери = проход). Потребитель — **Phase 7** (базовые юниты + CQB): тогда же добавим per-floor NavGrid'ы и Stairs как cross-graph edges. Контракт `NavService.FindPath`: добавится опциональный параметр `FloorContext`, дефолтное поведение (без него) останется прежним.

- **Unit selection / direct control** — Phase 6 управляет якорем как player marker'ом (правый клик → move-to-point). Потребитель — **Phase 7**: marquee select / single-click / direct-control toggle / stance keys. Никаких Squad-абстракций (это Phase 9).

- **`Locomotion` enum + per-class cost** — Phase 6 задаёт enum с одним значением `LocomotionFoot`. Потребитель — **Phase 8** (техника): добавит `LocomotionWheeled / Tracked / Tracked-heavy` и per-cell precomputed cost variants (либо ленивую оценку через `Flags + Locomotion` decision-table). `NavService.FindPath` API готов к этому через `NavOpts.Locomotion`.

- **Cover Quality dynamic re-eval** — Phase 6 пишет статический `Quality` на slot'е. Потребитель — **Phase 10**: utility-AI считает реальное значение через `dot(OriginDir, threatDir) × Quality × stance_match`. Контракт устойчив (Phase 6 Quality = верхняя граница).
