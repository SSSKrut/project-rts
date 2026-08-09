# Phase 3 — рабочий план

Props-фаза. Все статические объекты мира — деревья, кусты, камни, реки, мосты — через единую абстракцию `Prop{TypeID, Transform}`. Геймплейные атрибуты (cover, blocks-LOS, blocks-movement, hp) приходят из `PropTypeRegistry` по `TypeID`, не из меша. Это позволит в Phase 15 заменить placeholder-меш на загружаемую модель без правок геймплея.

ROADMAP — высокоуровневый трекер. Этот файл — рабочий, обновляется по ходу выполнения.

---

## Решения, которые лочим до начала кода

**P1. Prop = отдельная ECS-сущность, не запись в массиве чанка.**
Альтернатива «slice пропов внутри Heightmap-сущности» отвергнута: в Phase 11 (combat) пропы станут разрушаемыми (свалить дерево, разрушить забор), а в Phase 6 — источниками cover-slot'ов для Smart Objects. Промоушен «структура → сущность» при первом изменении состояния — лишняя работа. Сразу делаем сущности.

Размер компонента маленький: `WorldPos (24) + Prop (8) + LODRelevant (0)` ≈ 32 байта на проп. На лесном чанке ~200 пропов = ~6 КБ — нормально.

**P2. `PropType uint16` — индекс в singleton-реестр.**
```go
type PropType uint16

const (
    PropNone PropType = iota
    PropOak
    PropPine
    PropBirch
    PropBush
    PropRock
    PropWater
    PropBridge
    PropTypeMax  // sentinel
)

type Prop struct {
    Type  PropType  // 2
    Yaw   float32   // 4 — поворот вокруг Y, [0, 2π)
    Scale float32   // 4 — uniform — для растительности достаточно
}
// 16 байт с padding до 8-aligned внутри Prop
```

Фиксированный набор типов в enum — заменим на динамическую регистрацию когда появятся загружаемые модели (Phase 15). До этого `Metas [256]PropMeta` массив достаточен.

**P3. `PropTypeRegistry` — singleton resource.**
```go
type PrimitiveKind uint8
const (
    PrimitiveCone PrimitiveKind = iota
    PrimitiveSphere
    PrimitiveCube
    PrimitiveCylinder
    PrimitivePlane
)

type PropMeta struct {
    Primitive  PrimitiveKind
    Size       rl.Vector3   // полуразмеры для куба, (radius, height, _) для cone/cylinder
    Color      rl.Color
    Cover      float32      // 0..1, читает Phase 6 CoverEvaluation; в Phase 3 не используется
    HP         float32      // в Phase 3 не используется
    BBoxRadius float32      // плоский радиус для будущих spatial queries
    BlocksLOS  bool         // в Phase 3 не используется
    BlocksMove bool         // в Phase 3 не используется
}

type PropTypeRegistry struct {
    Metas [256]PropMeta
}
```

Регистрация в `main.go` через `ecs.AddResource` *до* `InitUI` всех систем, использующих регистр (по аналогии с `TerrainChunkIndex`).

В Phase 3 placeholder-рендер — через `rl.DrawCube*` / `rl.DrawCylinderEx` / `rl.DrawSphere` от `Primitive + Size + Color`. Никаких меш-загрузок. В Phase 15 структура `PropMeta` поменяется — добавится `Mesh rl.Mesh` для загружаемых моделей; контракт `PropType → PropMeta` сохранится.

**P4. Owner-чанк проп определяется через `prop.WorldPos.Chunk`.**
Никакого отдельного `OwnerChunk` компонента. Пропсы статичны (yaw фиксирован при спавне, scale тоже), их позиция определяет принадлежность чанку.

**P5. `PropChunkIndex` — singleton resource для O(1) despawn.**
```go
type PropChunkIndex struct {
    Loaded map[components.ChunkCoord][]ecs.Entity
}
```

Заполняется в `PropSpawnSystem` (append после спавна), читается в `TerrainStreamingSystem.evict` (пройти список, удалить сущности). Не читать «найди все пропы в чанке X» через filter — на стотысячах сущностей будет линейный пас.

**P6. Спавн пропов триггерится маркером `PropsDirty struct{}` на чанке.**
По аналогии с `HeightmapDirty + MeshDirty`. `TerrainStreamingSystem` ставит при создании чанка одновременно с `HeightmapDirty + MeshDirty`. `PropSpawnSystem` снимает после прохода.

**P7. Размещение растительности — детерминированный grid + jitter.**
Каждый чанк делится на `8×8 = 64` grid-клетки (по 8м каждая). В каждой клетке:
- `densityF = BiomeDensity(seed, cellCenterX, cellCenterZ, biomeKind)` — float в [0..1].
- `roll = hashFloat(seed, cc.X, cc.Z, gridX, gridZ, salt)` — детерминированный random в [0..1].
- Если `roll < densityF * baseRate` — спавним. `baseRate` = вероятностный множитель для типа (например, 0.7 для деревьев в плотном лесу, 0.3 для кустов).
- Позиция внутри клетки: jitter из того же hash'а с другим salt'ом, в пределах `[0, GridCellSize)` по X и Z.
- Тип конкретного prop'а внутри биома (Oak/Pine/Birch для леса) — отдельный hash → mod на варианты.

`hashFloat` — splitmix-подобный mix без `math/rand` (он скрывает глобальный state, не годится для детерминизма по координатам).

Никакой ручной расстановки. Все 64 проверки на чанк отрабатывают за десятки наносекунд.

**P8. Биом-карта = fBm Perlin со своим offset'ом, не пересекается с heightmap.**
```go
func BiomeDensity(seed int64, wx, wz float32, kind BiomeKind) float32
```

Реализация — тот же fBm-Perlin что в `GroundHeight`, но с другим базовым seed-сдвигом и другими частотами (более низкая частота для крупных лесных массивов: lacunarity ниже, octaves меньше — 2 хватает). Биомы: `Forest`, `Bushland`, `Rocky`, `Plains`. Несколько биом-карт с разными смещениями — кусты могут быть и в лесу, и в polях; не суммируются, каждая независима.

`BiomeDensity` лежит в `systems/biome.go` — отдельный файл, не пихаем в `noise.go` (там procgen heightmap'а, читаемость).

**P9. `Y` пропа = `GroundHeight(wx, wz)`.**
Та же функция что для якоря. Дерево «прибито к земле» по тому же источнику истины. Если в Phase 6 появится `BlocksMove` от уклона — это уже на уровне `BiomeDensity` (склоны > 30° → density = 0).

**P10. River = захардкоженный polyline в `main.go`.**
В Phase 3 — 1-2 ломаные `[]WorldPos`. Ни ресурс, ни сущность; просто пакетный `var Rivers = []RiverPolyline{ ... }`. В Phase 4 (Roads) рассмотрим вместе с RoadGraph: возможно поднимем до `RiverNetwork` ресурса. Авто-генерация рек — Phase 15 (контент-пайплайн с OSM).

**P11. River-cut — heightmap-stamp через `Stamper.RiverCut`, без `Modified`.**
Расширяем существующий `Stamper`:
```go
func (s *Stamper) RiverCut(cc components.ChunkCoord, polyline []components.WorldPos, width, depth float32)
```

Работает на одном чанке: проходит heightmap-вершины, для каждой считает дистанцию до ближайшей точки polyline, применяет cosine-falloff `delta(d) = -depth * 0.5 * (1 + cos(pi * d/width))` при `d ≤ width`, иначе 0. Ставит `MeshDirty`. **Не ставит `Modified`** — это процедурная модификация, чанк остаётся pristine с точки зрения persistence.

Профиль такой же как у `Crater`-kernel'а, только центр — линия, а не точка. Реализация — переиспользуем cosine-half kernel.

**P12. `RiverSystem` — отдельная система, гейтит cut'ы маркером `RiverProcessed`.**
```go
type RiverProcessed struct{}
```

Маркер локальный (на сущности чанка), не сериализуется. Filter: `Heightmap + Without[Modified] + Without[RiverProcessed]`. Логика:
1. Для каждой `RiverPolyline` rough-check пересечения с bbox чанка.
2. При пересечении: `stamper.RiverCut(cc, polyline, width, depth)` + `spawnWaterProps(cc, polyline)`.
3. Поставить `RiverProcessed`.

Гарантирует, что cut применяется ровно один раз на pristine-чанк за его жизненный цикл. После `Stamper.StampHeightmap` (пользовательская правка) на этом же чанке встаёт `Modified` → `RiverSystem` пропустит при повторном проходе. После evict'а чанк уничтожается, `RiverProcessed` теряется; повторный спавн pristine применит cut снова — это ок, детерминированно.

**P13. Modified-маркер — НЕ ставится `RiverSystem` и `PropSpawnSystem`.**
Только пользовательские правки через `Stamper.StampHeightmap` (debug X) ставят Modified. Persistence-формат v1 не меняется. Пропы не сохраняются (детерминированы).

**P14. Пропы НЕ персистятся в Phase 3.**
Все детерминированы от seed'а. Если в Phase 11 (combat) появятся разрушаемые пропы — добавим маркер `Modified` на проп и flush at evict. Формат файла v1 не меняется в этой фазе; зарезервированный `Flags` чанкового формата зайдёт в дело позже.

**P15. LOD пропа = LODRelevant всегда.**
Простой инвариант: пропы спавнятся только в Active+Relevant чанках, despawn'ятся вместе с чанком при переходе в Dormant (см. P5). Поэтому у живого пропа всегда `LODRelevant` — нет смысла в дополнительной логике.

`LODSystem` (тот, что для юнитов) **исключает** Prop через `Without[Prop]` в фильтре — по аналогии с тем как сейчас исключаются `TerrainChunk`. Это явно фиксируем при правке `LODSystem` в M3.4.

**P16. Рендер — наивный цикл.**
Filter в render-loop'е `WorldPos + Prop + (LODActive | LODRelevant)` (но фактически у пропа всегда `LODRelevant`). Один `rl.DrawCube/Sphere/Cylinder` на проп. Никакого `rl.DrawMeshInstanced` — переедем на instancing когда увидим спайки на 5+ тыс пропов в фрустуме (Phase 16).

Транформация — `WorldPos.ToRenderSpace(CurrentOriginChunk)` + поворот по Yaw + scale по Scale.

**P17. Пайплайн систем меняется.**
Текущий: `terrain_streaming → terrain_load → terrain_gen → terrain_mesh → ...`

Новый:
```
terrain_streaming
terrain_load
terrain_gen
river                  (новая)
prop_spawn             (новая)
terrain_mesh
ground_stick
...
```

`river` ставит `MeshDirty` (cut меняет heights → меш надо пересобрать). `prop_spawn` НЕ трогает heights — только спавнит сущности. Регистрация — строго в этом порядке в `main.go`.

**P18. Что осознанно НЕ делаем в Phase 3.**
- Persistence пропов (Phase 11/16).
- Авто-генерация дорог + мостов на пересечении дорог и рек (Phase 4).
- Авто-генерация рек (пока polylines в коде; авто — Phase 15).
- Разрушаемость пропов (Phase 11 Combat).
- Cover slots вокруг пропов / Smart Object компоненты `Occupancy`/`CoverDirection`/`ShootingArc` на пропах (Phase 6).
- Instancing рендера / GPU foliage (Phase 16).
- Trench-stamp kernel (Phase 5 Buildings — там же, где fortifications). RiverCut в этой фазе закрывает потребность в polyline-stamp'е, при этом trench по форме идентичен — реюзнем kernel.
- BlocksMove от пропов (NavGrid читает в Phase 6).
- Авто-разметка пропов как cover-slot (Phase 6).
- LOD-decimation для пропов (например, дальние деревья как imposter'ы) — Phase 16.

---

## Семь мильстоунов

### M3.1 — Типы, реестр, placeholder-рендер одного пропа

**Цель.** Каркас собран: типы заведены, реестр загружен, в `main.go` руками заспавнена одна тестовая сущность с `Prop` — она рендерится на сцене.

**Делаем:**
- `components/prop.go`:
  - `PropType uint16` + константы из P2.
  - `Prop{Type, Yaw, Scale}`.
  - `PropsDirty struct{}` — маркер.
  - `RiverProcessed struct{}` — маркер.
- `components/prop_registry.go`:
  - `PrimitiveKind uint8` + константы из P3.
  - `PropMeta` структура.
  - `PropTypeRegistry{Metas [256]PropMeta}` — поле в виде ресурса.
- `systems/prop_registry.go`:
  - `func NewPropTypeRegistry() *PropTypeRegistry` — создаёт и заполняет meta'ы для всех `Prop*` констант (placeholder параметры: Oak — коричневый цилиндр + зелёный конус сверху; Pine — узкий тёмно-зелёный конус; Bush — маленькая сфера; Rock — серый куб; Water — синий plane; Bridge — деревянный куб). На уровне Phase 3 простота важнее красоты.
- `main.go`:
  - До InitUI: `ecs.AddResource(app.World, systems.NewPropTypeRegistry())`.
  - В качестве sanity-теста: руками заспавнить одну сущность `Oak` рядом с anchor'ом, проверить что она рисуется (см. M3.5 для render-фильтра).

**Проверяем.** `go run main.go` запускается, в консоли нет ошибок про отсутствующий ресурс, тестовая сущность виднеется (после M3.5 — без него ещё не видно). Если M3.5 ещё впереди — просто проверяем компиляцию и `ecs.GetResource[PropTypeRegistry]` возвращает ненил.

### M3.2 — Биом-карта + детерминированный hash

**Цель.** Утилитарный слой — функция для density-сэмплинга и детерминированный hash для размещения. Без интеграции в системы.

**Делаем:**
- `systems/biome.go`:
  - `BiomeKind uint8` + константы `Forest / Bushland / Rocky / Plains`.
  - `func BiomeDensity(seed int64, wx, wz float32, kind BiomeKind) float32` — fBm-Perlin со seed-сдвигом по `kind` (например, `seed + int64(kind)*1000003`). 2 октавы, lacunarity 2.0, persistence 0.5, базовая частота `1/256` (более крупная масштаб, чем у heightmap'а — лесные массивы крупнее, чем неровности рельефа).
  - `func hashFloat(seed int64, args ...int32) float32` — splitmix-подобный mix args + seed → uint32 → нормализованный float32 в [0, 1).

**Проверяем.** Tiny debug print в `main.go` (или временный F2-overlay): `BiomeDensity(seed, anchorX, anchorZ, Forest)` — печатает float, видим, что при движении anchor'а значение меняется плавно, есть локальные клейстеры > 0.5 и < 0.5.

### M3.3 — `PropSpawnSystem` + `PropChunkIndex`

**Цель.** Чанки автоматически населяются процедурными пропами. Лесные кластеры видны в логах.

**Делаем:**
- `components/prop_chunk_index.go`:
  - `PropChunkIndex{Loaded map[ChunkCoord][]ecs.Entity}` — структура ресурса.
- `systems/prop_spawn.go`:
  - `PropSpawnSystem` с pre-built handles (`InitUI` pattern).
  - LOD-policy: `ActiveEvery: 0`, остальные disabled — пропы спавнятся в кадр создания чанка.
  - Filter: чанки с `Heightmap + PropsDirty`.
  - Update: для каждой сущности чанка:
    1. Получить `cc := chunkCoord`.
    2. Для `gridX, gridZ in [0..8)`:
       - `cellOriginWorld := (cc.X * ChunkSize + gridX*8, cc.Z * ChunkSize + gridZ*8)`.
       - Сэмплить `BiomeDensity(seed, cellCenter, Forest)` → решение по hashFloat.
       - При решении «спавнить»: тип = hashMod на {Oak, Pine, Birch}, jitter позиции, `Y = GroundHeight(...)`, yaw = hashFloat * 2π, scale = `0.8 + 0.4 * hashFloat`.
       - Создать сущность через `ecs.NewEntity` с `WorldPos + Prop + LODRelevant`.
       - `propIndex.Loaded[cc] = append(..., entity)`.
    3. Аналогично для `Bushland` (более низкий threshold) и `Rocky` (камни).
    4. После прохода — снять `PropsDirty` (deferred batch — собирать сущности чанков и снимать одной волной после Filter-цикла, по паттерну CLAUDE.md).
  - Регистрировать ресурс `PropChunkIndex` в `main.go` до `InitUI`.
- `systems/terrain_streaming.go`:
  - При создании чанка добавлять `PropsDirty` рядом с `HeightmapDirty + MeshDirty`.
- `main.go`:
  - Регистрация `PropSpawnSystem` после `terrain_gen`, до `terrain_mesh` (см. P17).

**Проверяем.**
- Логирование в spawn-цикле: «spawn N props in chunk X,Z; trees=A bushes=B rocks=C» — N > 0 в лесных биомах, N == 0 в чистом поле.
- `len(world.entities)` (или filter `Prop`) растёт линейно с количеством Active+Relevant чанков, потом стабилизируется.

### M3.4 — Despawn пропов при эвикте чанка

**Цель.** Пропы удаляются вместе с чанком; нет утечек сущностей.

**Делаем:**
- `systems/terrain_streaming.go`:
  - В eviction-цикле, *до* `RemoveEntity` чанка:
    ```go
    if props, ok := propIndex.Loaded[cc]; ok {
        for _, p := range props {
            world.RemoveEntity(p)
        }
        delete(propIndex.Loaded, cc)
    }
    ```
  - Затем — текущая логика `WriteChunk` для `Modified` + `UnloadMesh` + `RemoveEntity` чанка.
- `systems/lod.go` (`LODSystem`):
  - В фильтр добавить `Without[Prop]` — `LODSystem` не пересчитывает LOD пропов.

**Проверяем.**
- HUD: «Live entities: N | Live props: M». Идёшь к лесу — M растёт, отходишь за Relevant радиус — M падает обратно. Стабильно.
- `propIndex.Loaded` не растёт неограниченно (можно временно print'ом подтвердить через `len(propIndex.Loaded)`).

### M3.5 — Рендер пропов

**Цель.** Леса и кусты видны на ландшафте.

**Делаем:**
- В `main.go` render-loop, внутри `BeginMode3D`:
  ```go
  filter.Each(func(e ecs.Entity) {
      pos := posMap.Get(e)
      prop := propMap.Get(e)
      meta := registry.Metas[prop.Type]
      renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)
      drawPropPrimitive(meta, renderPos, prop.Yaw, prop.Scale)
  })
  ```
- `drawPropPrimitive` — switch по `meta.Primitive`:
  - `Cube` → `rl.DrawCubeV(pos, meta.Size*scale, meta.Color)`.
  - `Cylinder` → `rl.DrawCylinderEx(...)` (для стволов деревьев).
  - `Cone` → нет нативного `DrawCone` в raylib — собираем из `DrawCylinderEx(top=0, bottom=R)`.
  - `Sphere` → `rl.DrawSphere(pos, R*scale, meta.Color)`.
  - `Plane` → `rl.DrawPlane(pos, size, color)`.
- Yaw применяем через `rlPushMatrix / rlRotatef / rlPopMatrix` в обёртке (raylib-go экспонирует эти примитивы).
- HUD-обновление: добавить «Props live: N» рядом с уже отображаемыми статистиками.

**Проверяем.**
- Запустил → видно лес, кусты, камни, разбросанные по биомам.
- На границе биома (где density переходит через threshold) есть видимый кластеризованный градиент.
- Якорь идёт в чистое поле → пропов нет.
- На стыке Active/Relevant чанков пропы есть с обеих сторон одинаково — нет «пустой полосы».
- Кратер от X (debug stamp) рисуется *поверх* пропов (пропы не утопают в землю — но дерево, попавшее в центр кратера, может теперь висеть в воздухе. Это норм, в Phase 11 будет реакция «дерево падает»).

### M3.6 — Реки

**Цель.** В мире видна одна-две реки: heightmap имеет понижение русла, поверх русла нарисована полоса воды, рядом не растут деревья.

**Делаем:**
- `components/river.go`:
  - `RiverPolyline struct { Points []WorldPos; Width float32; Depth float32 }`.
- `main.go`:
  - `var Rivers = []components.RiverPolyline{ ... }` — захардкодить 1-2 ломаные через несколько чанков рядом со стартом якоря (для визуальной проверки).
- `systems/stamp.go`:
  - `func (s *Stamper) RiverCut(cc components.ChunkCoord, polyline []WorldPos, width, depth float32)`:
    1. Получить heightmap чанка через `propIndex` ... wait — нужен `terrainChunkIndex` resource (он уже есть). Через него `entity := terrainIndex.Loaded[cc]`, `heightmap := s.heightmapMap.Get(entity)`.
    2. Для каждой вершины (i, j): world XZ = `(cc.X*ChunkSize + float32(i), cc.Z*ChunkSize + float32(j))`.
    3. `dist = nearestDistanceToPolyline(world XZ, polyline)`.
    4. Если `dist <= width`: `delta = -depth * 0.5 * (1 + cos(pi * dist/width))`, `heights[j*Resolution + i] += delta`.
    5. Поставить `MeshDirty` на сущности чанка. **Не ставить `Modified`.**
- `systems/river.go`:
  - `RiverSystem` с pre-built handles, LOD-policy `ActiveEvery: 0`.
  - Filter: `WorldPos + Heightmap + ChunkCoord + Without[Modified] + Without[RiverProcessed]`.
  - В `Update` для каждого чанка:
    1. Для каждой `RiverPolyline` — rough-check пересечения с bbox чанка (расширенный bbox: `(cc world bounds) ± width`).
    2. При пересечении: `stamper.RiverCut(cc, polyline.Points, polyline.Width, polyline.Depth)`.
    3. Спавнить water-пропы: пройти polyline сегментами, в пределах bbox чанка ставить `WaterProp` каждые 4м (placeholder — синий plane 4×4 на уровне русла; визуально достаточно).
    4. После прохода — поставить `RiverProcessed` на чанке (deferred batch, см. M3.3).
  - Регистрация в `main.go` — после `terrain_gen`, до `prop_spawn` (P17).
- `systems/prop_spawn.go`:
  - В spawn-цикле: для каждой кандидатной точки — проверить `nearestDistanceToRivers(wx, wz) > minDistanceFromRiver`. Если ближе — пропустить (не сажаем дерево в реку). Это можно как фильтр — не идеально, но дёшево (1-2 polyline, считаем дистанцию к ним).

**Проверяем.**
- Спавн якоря рядом с рекой → видна полоса понижения, синяя вода поверх.
- Деревья не растут в воде (видно по визуальному отступу от русла).
- Якорь ушёл далеко за Relevant → вернулся → река та же.
- Стампанул кратер X на чанке с рекой → чанк стал Modified → quit/restart → кратер на месте, русло на месте, повторного "удвоения" cut'а нет.

### M3.7 — Тестовый мост + завершающая полировка

**Цель.** Финальный визуальный output. Один тестовый мост поверх реки, чтобы проверить пользовательский путь от берега до берега.

**Делаем:**
- В `main.go` после регистрации систем: ручной спавн `PropBridge` сущности в точке пересечения реки и пути якоря (захардкоженные координаты).
- Bridge-prop в реестре — деревянный Cube размера ~ width реки × 2м толщиной × 1м высотой над русла.
- Включить yaw для bridge'а так, чтобы перекрыть реку поперёк.

**Проверяем.**
- Якорь идёт на мост — он стоит над рекой, не утоплен.
- Якорь стоит под мостом — мост нависает (это норм, в Phase 6 будет blocks-move от воды; пока anchor проходит куда угодно).
- HUD: «Props live: N | Rivers: 2 | Bridges: 1».
- Quit/restart — мост и река на месте.

---

## Что считаем «закрытием Phase 3»

- `go run main.go` показывает населённый ландшафт: лес, кусты, камни в нужных биомах, одна-две реки с водой, тестовый мост поверх.
- Пропы пропадают и появляются вместе с чанками; нет утечек сущностей при долгом блуждании по миру.
- `Stamper.StampHeightmap` (debug X) продолжает работать; модифицированный чанк с рекой переживает quit/restart без артефактов (cut не удваивается, мост и пропы регенерируются процедурно).
- Persistence формат v1 не изменился; пропы не пишутся на диск; `./save/world-default/chunks/` содержит только пользовательские модификации.
- Контракт `PropType + PropTypeRegistry` готов к Phase 15 (загружаемые модели) — внутренние правки рендера не затрагивают вызывающий код.

После этого — обновление ROADMAP, Фаза 3 → ✅, и выбор следующего шага. По плану — Фаза 4 (Roads). Возможно вытянуть кусочек NavGrid (Phase 6) пораньше, если потребуется для дорог; обсудим в момент перехода.

---

## Заметки на полях

- В Phase 3 дороги ещё не существуют, поэтому мост спавнится «руками» поверх реки. В Phase 4 при создании дороги, пересекающей реку, мост будет создан автоматически — но контракт `PropBridge` тот же.
- River-cut и prop-spawn — pristine-only. Это значит: если игрок построит здание (Phase 5) поверх реки, надо будет либо запретить через UI, либо обработать через `Modified`-цикл. В Phase 3 этого нет — реки и пропы только на чистом ландшафте.
- `BiomeDensity` сэмплируется в центре каждой grid-клетки, не в каждой потенциальной точке prop'а. Это даёт небольшой aliasing на границах биомов (резкие переходы шириной 8м). Если станет видно — увеличиваем сэмплирование (4×4 пере-сэмпла на клетку), но в Phase 3 не оптимизируем.
- Размер чанка 64×64м и grid 8×8 = 64 кандидата на чанк. При плотности 0.5 это ~32 prop'а на лесной чанк. Active+Relevant = ~80 чанков → ~2.5К пропов на экране в худшем случае. Naive `rl.DrawCube`-цикл выдержит. Когда увидим спайки — Phase 16.
- `RiverSystem` сейчас single-pass (в InitUI читает массив `Rivers` один раз). Если потом понадобится динамические реки (например, разрушение дамбы поднимает уровень) — переделаем в систему с ресурсом `RiverNetwork`, но в Phase 3 polyline'ы статичны.
