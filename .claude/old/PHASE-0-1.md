# Phase 0 + Phase 1 — рабочий план

Координатный рефакторинг + Terrain MVP. Сделаны вместе, потому что одно проверяет другое: переход на `WorldPos` не имеет смысла без реального открытого мира, а терраин нельзя адресовать без `ChunkCoord`.

Этот документ — рабочий, обновляется по ходу. ROADMAP остаётся высокоуровневым трекером.

---

## Решения, которые лочим до начала кода

Эти выборы определяют форму всего остального. Хочу зафиксировать их явно, прежде чем писать код.

**Р1. Форма `WorldPos`.**
```go
type ChunkCoord struct{ X, Z int32 }    // Y не чанкуется — только high/low

type WorldPos struct {
    Chunk ChunkCoord
    Local rl.Vector3   // X,Z ∈ [0, ChunkSize); Y — высота, не ограничена
}
```
Инвариант: `Local.X, Local.Z ∈ [0, ChunkSize)`. Любая операция, которая может его нарушить, обязана сразу нормализовать (перенести в соседний чанк).

**Р2. Константы.** `ChunkSize = 64.0` метра, `ChunkResolution = 65` (= 64 квада + 1 общая вершина с соседом). Зашиваются в `components` пакет как `const`, не как поля компонентов.

**Р3. Render origin = чанк камеры, пересчитывается каждый кадр.** В `BeginMode3D` всё рисуется в render-space относительно `camera.Chunk`. Камера в render-space = `camera.Local`. Объект на `WorldPos p` рисуется в `(p.Chunk - camera.Chunk)*ChunkSize + p.Local`. Float32-точность не страдает: дальние объекты вне фрустума всё равно не рисуются.

В коде заводим `systems.CurrentOriginChunk ChunkCoord` рядом с уже имеющейся `systems.CurrentCamera`. Обновляются обе одной системой.

**Р4. Радиусы стриминга терраина.** Стартуем с консервативных:
- Active: 2 чанка (≈ 5×5 = 25 чанков, полный меш)
- Relevant: 4 чанка (≈ 9×9 − 5×5 = 56 чанков, упрощённый меш)
- Гистерезис: 1 чанк
- Дальше — Dormant (сущности уничтожаются)

Память на 81 чанк ≈ 1.4 МБ heights + 3 МБ GPU-данных. Запас на расширение есть.

**Р5. Procgen — детерминированный шум по мировым координатам.** Шум сэмплируется в координатах `(chunk.X*ChunkSize + local.X, chunk.Z*ChunkSize + local.Z)` с глобальным seed. Совпадение краёв соседних чанков — by construction, специальной логики не нужно. Реализация: **fBm поверх Perlin, 4 октавы**, `lacunarity = 2.0`, `persistence = 0.5`, общая амплитуда ≈ 8м. Без внешних библиотек, своя реализация в `systems/noise.go` (~80–100 строк). Параметры легко тюнить, общий вид оценим визуально после M1.3.

**Р6. Два штамма стриминга.** `TerrainStreamingSystem` (новый, дистанционный) и `StreamingSystem` (старый, графовый, для будущих smart-spaces) — *два разных файла, два разных назначения*. Не сливать.

**Р7. Маркеры LOD на чанках = те же `LODActive`/`LODRelevant`.** Не плодим отдельные `ChunkActive`/etc. Но: `LODSystem` (тот, что для юнитов) должен исключать терраин-чанки из своей итерации, потому что их LOD управляется другой системой. Это сделаем через маркер `TerrainChunk` + `Without` в фильтре.

**Р8. Пайплайн чанков из трёх систем.** Разделение ответственности через пары маркеров:
- `TerrainStreamingSystem` — создаёт/уничтожает сущности чанков, управляет LOD-маркерами, ставит `HeightmapDirty` + `MeshDirty` при создании, ставит только `MeshDirty` при смене тиера.
- `TerrainGenSystem` — читает `HeightmapDirty`, генерит heights, снимает `HeightmapDirty`.
- `TerrainMeshSystem` — читает `MeshDirty` (и наличие `Heightmap`), строит/перестраивает меш, снимает `MeshDirty`.

Это даёт явные граф зависимостей между этапами и упрощает повторные перестройки (например, после деформации в будущем — поставил `HeightmapDirty`, и пайплайн отработает дальше сам).

**Р9. Якорь приклеен к поверхности.** Введём `groundHeight(world_x, world_z) float32` — единая точка вычисления высоты по мировым координатам, работает на том же fBm, что и procgen. Используется и при генерации высот чанков, и при позиционировании якоря: `anchor.WorldPos.Local.Y = groundHeight(world_x, world_z) + AnchorEyeHeight` (например `1.5` пока). Любой будущий юнит-пехотинец будет «прибиваться к земле» этой же функцией. Реализация — в `systems/noise.go`, чтобы и procgen, и контроллер якоря импортировали один и тот же источник истины.

**Р10. Что осознанно НЕ делаем в этой паре фаз.**
- Persistence чанков на диск (Фаза 2).
- SDF-патчи (Фаза 3).
- Cover-map / NavGrid (Фаза 4).
- Шейдеры террейна (текстурирование, biomes) — пока flat-color меш.
- Удаление `Position3D` API «в стиле raylib`rl.Vector3`» — `WorldPos.Local` остаётся `rl.Vector3`, чтобы не тащить ещё одну математическую обёртку.

---

## Шесть мильстоунов

### M0.1 — Типы и хелперы

**Цель.** `components/worldpos.go` с типами и операциями. Никаких изменений в системах ещё.

**Делаем:**
- `components/worldpos.go`:
  - `ChunkCoord{ X, Z int32 }`
  - `const ChunkSize float32 = 64.0`, `const ChunkResolution int = 65`
  - `WorldPos{ Chunk ChunkCoord; Local rl.Vector3 }`
  - `func (p WorldPos) Add(v rl.Vector3) WorldPos` — двигает Local, нормализует пересечение чанка
  - `func (p WorldPos) Sub(other WorldPos) rl.Vector3` — возвращает мировой вектор от other к p
  - `func DistanceSquared(a, b WorldPos) float32`
  - `func Distance(a, b WorldPos) float32`
  - `func (p WorldPos) ToRenderSpace(origin ChunkCoord) rl.Vector3`
  - `func Normalize(p WorldPos) WorldPos` (на случай, если Local вылетел за границы по любой причине)
- Удаляем `components/components.go::Position3D` (ломает компиляцию — это намеренно, чинимся в M0.2).

**Проверяем.** Пакет `components` собирается; `go build ./components` ок. Остальное проекта временно не собирается.

### M0.2 — Миграция всех систем на WorldPos

**Цель.** Проект снова собирается и запускается с тем же визуальным поведением, что и до рефакторинга — но всё уже на новых координатах.

**Порядок переписывания:**
1. `MovementSystem` — `pos.X += vel.X*dt` → `*pos = pos.Add(vel * dt)`. `wrapBounds` пересмотреть: с открытым миром wrap не нужен; убираем (mobile-кубики просто разлетятся, это пока ок).
2. `LODSystem` — anchor lookup и distance pass переходят на `WorldPos` + `DistanceSquared`. Здесь же добавляем исключение терраин-чанков (через `Without[components.TerrainChunk]` в фильтре, даже если самого маркера ещё нет — поле есть, тип есть).
3. `SpatialAudioSystem` — distance + chunk-bucket для voice limiting. Бакет считаем не по `(int(pos.X/50), int(pos.Y/50), int(pos.Z/50))` а по `(chunk.X, chunk.Z)` напрямую (или по более грубой группе чанков).
4. `OrbitSystem` — target lookup и сферические→декартовы координаты. Camera position обновляется через `Add` относительно target.
5. `CameraSystem` — главное изменение: вычисление `rl.Camera3D` в render-space. Сюда же добавляем `systems.CurrentOriginChunk = camPos.Chunk` и `Camera.Position = camPos.Local`, `Camera.Target = targetPos.ToRenderSpace(camPos.Chunk)`.
6. `main.go`:
   - WASD-движение якоря: `anchorPos = anchorPos.Add(rl.Vector3{X: moveSpeed, ...})`.
   - Создание сущностей: все `posMap.Add(e, &WorldPos{...})`. Убираем дублированный `posMap2`.
   - Render loop: для каждой нарисованной точки — `renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)`.
7. `StreamingSystem` — анкор-фильтр запрашивает `WorldPos`, но прямо позицию не использует (он по `NodeEntity`). Минимальные правки.

**Проверяем.** `go run main.go` запускается; якорь двигается; кубики рисуются; камера крутится; рендер не дёрнулся. Поведение идентично прежнему.

### M0.3 — Починка StreamingMap + чистка

**Цель.** Убрать накопившиеся мелочи, чтобы они не путались в Фазе 1.

**Делаем:**
- В `main.go` после `app.Tick`-инициализации создаём `StreamingMap` ресурс: `streamingMapRes.Add(&components.NewStreamingMap())`.
- В `StreamingSystem.Update` — переписываем на in-place: вместо `streamingMapRes.Add(&components.StreamingMap{...})` используем `clear(sMap.States); sMap.States[anchorNode] = ...`. Никаких аллокаций map за тик.
- Удаляем мёртвый `_ = time.Now()` в `OrbitSystem`.

**Проверяем.** Поведение прежнее; нет аллокаций map в `StreamingSystem` (визуально через профилировщик/bench не проверяем — просто корректность).

### M1.1 — Terrain streaming (скелет без рендера)

**Цель.** Сущности чанков создаются и уничтожаются по мере движения якоря. Рендера ещё нет — проверяем только lifecycle.

**Делаем:**
- `components/terrain.go`:
  - `Heightmap{ Heights [ChunkResolution*ChunkResolution]float32 }` — fixed-size массив, кэш-френдли
  - `ChunkMesh{ Model rl.Model; Uploaded bool }`
  - `HeightmapDirty struct{}`, `MeshDirty struct{}`, `TerrainChunk struct{}`
- `systems/terrain_streaming.go`:
  - Resource `TerrainChunkIndex{ Loaded map[ChunkCoord]ecs.Entity }` — для O(1) проверки «жив ли чанк»
  - Filter: `LODAnchor` + `WorldPos` (находим якорь)
  - Update логика:
    1. Найти `anchor.Chunk`
    2. Построить target set: все `(x,z)` в Chebyshev-дистанции ≤ RelevantRadius
    3. Для отсутствующих создать сущности с `WorldPos{anchor.Chunk_neighbor, (0,0,0)}`, `ChunkCoord`, `TerrainChunk`, `HeightmapDirty`, `MeshDirty`. LOD-маркер по дистанции с гистерезисом.
    4. Для существующих, выпавших из target set — уничтожить (включая выгрузку GPU-меша через `rl.UnloadModel` если был загружен).
    5. Для существующих в target set — обновить LOD-маркер при необходимости + добавить `MeshDirty` при смене тиера.
- `main.go`: создаём якорь с `WorldPos`, регистрируем `TerrainStreamingSystem`. Регистрация после существующих систем; LOD-policy на терраин-стриминге: `ActiveEvery: 250ms` (нам не нужно проверять каждый кадр).

**Проверяем.** Логирование: при движении якоря через границу чанка в консоль печатаются «создан чанк X,Z» / «уничтожен чанк X,Z». Количество живых сущностей-чанков примерно совпадает с теоретическим (`(2*Relevant+1)^2`).

### M1.2 — Procgen

**Цель.** Чанки получают heights через детерминированный шум.

**Делаем:**
- `systems/terrain_gen.go`:
  - Свой Perlin или fBm-функция в `systems/noise.go` (~80–100 строк, без зависимостей)
  - Filter: `ChunkCoord` + `HeightmapDirty` (без `Heightmap` или с — всё равно перегенерируем)
  - Update: для каждой сущности — генерим 65×65 высот по мировым координатам, добавляем/обновляем `Heightmap`, снимаем `HeightmapDirty`
- LOD-policy: `ActiveEvery: 0` (хочется генерить как только маркер появился, но не чаще чем раз в кадр). При создании 25+ чанков сразу — все они попадут в одну итерацию; это может дать спайк. Если ощутимо — позже добавим time-slice (N чанков за тик), но в MVP не делаем.

**Проверяем.** Дамп heights любого чанка в консоль. Соседние чанки имеют совпадающие крайние ряды (визуально или принтом).

### M1.3 — Mesh + render

**Цель.** Терраин виден на экране. Активные чанки — высокая детализация, релевантные — упрощённая. Швы бесшовны.

**Делаем:**
- `systems/terrain_mesh.go`:
  - Filter: `Heightmap` + `MeshDirty` + tier-маркер (запускается раздельно для Active и Relevant — две версии меша)
  - Active: 65×65 вершин, 64*64*2 = 8192 треугольника
  - Relevant: 33×33 вершин (каждый второй сэмпл), 32*32*2 = 2048 треугольников
  - Нормали считаем из соседних высот (cross-product)
  - Вершины, индексы, нормали → `rl.Mesh` → `rl.UploadMesh` → `rl.LoadModelFromMesh` → сохраняем в `ChunkMesh`. Если был старый `Model` — `rl.UnloadModel` его сначала.
  - Снимаем `MeshDirty`
- `main.go` render loop:
  - Filter: `WorldPos` + `ChunkMesh` + (`LODActive` ИЛИ `LODRelevant`)
  - Для каждого: `renderPos := pos.ToRenderSpace(systems.CurrentOriginChunk)`, `rl.DrawModel(mesh.Model, renderPos, 1.0, цвет_для_отладки)`
  - Цвета по тиеру для отладки (Active = травяной зелёный, Relevant = серо-зелёный)
- Убрать `rl.DrawGrid` (он перестаёт быть нужен и будет конфликтовать с террейном по Y=0)

**Проверяем.** Виден непрерывный ландшафт; при движении якоря дальние чанки появляются «в Relevant» (серо-зелёные), при подходе становятся «в Active» (зелёные). На стыках Active↔Relevant видна разница тесселяции, но *швов* (зазоров, перепрыгов высоты) не должно быть. Проверяем на кадре с активной границей.

---

## После M1.3 — что считаем «закрытием Фазы 1»

- `go run main.go` показывает бесшовный процедурный ландшафт
- Якорь двигается, чанки потоково появляются/исчезают
- LOD-тиеры визуально различимы, но без графических артефактов на швах
- Положения всех существующих сущностей выражены в `WorldPos`
- `StreamingMap` ресурс инициализирован и работает (готов к Фазе 3 — порталам)

После этого — обновляем `ROADMAP.md`: Фазы 0 и 1 → ✅, и переходим к выбору следующего этапа (Persistence или сразу Smart Objects + минимальный юнит для боёв на ландшафте).
