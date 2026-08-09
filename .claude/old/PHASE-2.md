# Phase 2 — рабочий план ✅ ЗАВЕРШЕНА

> **Статус:** закрыта. Файл оставлен как архив принятых решений и пройденного цикла. Действующее состояние терраина и persistence — в `CLAUDE.md` (раздел *Terrain pipeline / Persisted world data*) и в коде (`systems/persistence.go`, `systems/terrain_load.go`, `systems/stamp.go`, маркер `components.Modified`).
>
> **Что подтверждено пользователем:** кратер от X отображается, переживает eviction (выход за Relevant + возврат) и рестарт приложения. Pristine-чанки на диск не пишутся.

Persistence чанков: модифицированный ландшафт переживает eviction и рестарт; pristine остаётся чисто процедурным (нулевой оверхед на диске). Под конец появляется `Stamp(...)` API и дебаг-клавиша, которой можно ставить кратеры — это и тест-инструмент для пайплайна, и заготовка под Phase 3 (деформация от попаданий).

ROADMAP — высокоуровневый трекер. Этот файл — рабочий, обновляется по ходу выполнения.

---

## Решения, которые лочим до начала кода

**P1. Гранулярность хранения: один файл на чанк.**
~17 КБ на файл, atomic-write через `.tmp + rename`. До ~5–10 тыс. модифицированных чанков на одну директорию — без проблем на ext4/btrfs. Когда упрёмся — мигрируем на region-файлы (Minecraft-style, 32×32 чанков в одном пакете). Поэтому **persistence API минимален и абстрагирует подложку**: `WriteChunk(saveDir, cc, heights)` / `ReadChunk(saveDir, cc) → (heights, found, err)`. Остальной код не знает, как чанки лежат на диске.

**P2. Путь хранения.** `./save/world-default/chunks/{x}_{z}.bin`. Имя мира захардкожено `world-default` — задел на multi-world через подкаталоги. `./save/` добавляется в `.gitignore`.

**P3. Бинарный формат v1.**
| Offset | Size | Field |
|---|---|---|
| 0 | 4 | Magic `RTSC` |
| 4 | 2 | Version uint16 LE (=1) |
| 6 | 2 | Flags uint16 LE (reserved, =0 для v1) |
| 8 | 4 | ChunkCoord.X int32 LE |
| 12 | 4 | ChunkCoord.Z int32 LE |
| 16 | 16900 | Heights — 4225 × float32 LE, row-major по +Z |

Итого 16 916 байт. `Flags` зарезервирован под флаги вида «есть SDF-патч / cover-map / NavGrid». При смене формата — `Version` бьётся, на чтении при mismatch → `(found=false, err=ErrUnsupportedVersion)`, fallback на procgen (не падаем).

ChunkCoord в файле дублирует имя файла — sanity-check на чтении: если в файле координата не совпадает с ожидаемой, считаем файл битым.

**P4. Atomic-write.** `os.WriteFile(path+".tmp", buf, 0o644)` → `os.Rename(path+".tmp", path)`. Без fsync — на eviction чанков нам не критична durability «после crash через миллисекунду», а fsync дорогой.

**P5. Семантика маркеров (уточнение из Phase 1).**
- `HeightmapDirty` = **«нет heights, нужна начальная заливка»**. Ставится только при создании чанка. Не используется для деформации.
- `MeshDirty` = меш надо пересобрать.
- `Modified` (новый) = чанк отличается от чисто процедурного. Ставится `Stamp`. На eviction решает, писать ли на диск.

`Stamp` ставит **только** `MeshDirty + Modified`. Не трогает `HeightmapDirty`, иначе load/gen-системы затрут модификацию.

**P6. Триггеры записи.**
- `TerrainStreamingSystem.evict`: если у entity есть `Modified` И `Heightmap` — `WriteChunk` ДО `RemoveEntity`.
- При выходе из приложения: `defer` в `main.go` обходит все живые чанки с `Modified + Heightmap`, дописывает оставшиеся.

**P7. Триггеры чтения.** Новая `TerrainLoadSystem` встаёт в пайплайн **перед** `TerrainGenSystem`. Для каждого чанка с `HeightmapDirty` пробует `ReadChunk`; если найден — добавляет `Heightmap + Modified`, снимает `HeightmapDirty`. Если файла нет — оставляет как есть, дальше отрабатывает gen.

Порядок пайплайна:
1. terrain_streaming → 2. **terrain_load (новая)** → 3. terrain_gen → 4. terrain_mesh → 5. ground_stick → ...

**P8. Stamp API.**
```go
type HeightKernel func(dx, dz float32) float32

type Stamper struct { /* кэш Maps + Resource handle, по InitUI-паттерну */ }

func NewStamper(w *ecs.World) *Stamper

func (s *Stamper) StampHeightmap(center WorldPos, kernel HeightKernel, radius float32)
```
`Stamper` — service-объект (не System), создаётся один раз в `main.go`, используется по требованию. Внутренние Maps пре-билдятся как у систем — экономит на повторном построении handle при каждом стампе.

Алгоритм `StampHeightmap`:
1. Через `TerrainChunkIndex` найти живые чанки в bounding-квадрате стампа (max 9 при `radius < ChunkSize`).
2. Для каждого затронутого чанка с `Heightmap`: пройти вершины, у которых XZ-расстояние до центра ≤ `radius`, применить `kernel(dx, dz)` к высоте.
3. Поставить `MeshDirty + Modified`.

**P9. Готовый kernel: Crater.**
```go
func Crater(depth, radius float32) HeightKernel
```
Профиль — косинусная половинка: `delta(d) = -depth * 0.5 * (1 + cos(pi * d/radius))` для `d ≤ radius`, 0 за пределами. Гладкий, без острых краёв, подходит для будущих воронок от снарядов.

**P10. Дебаг-клавиша.** В `main.go`: `rl.KeyX` → `stamper.StampHeightmap(*anchorPos, systems.Crater(2.0, 4.0), 4.0)`. Кратер 4м радиусом, 2м глубиной, у позиции якоря. Ничего не стоит, даёт визуальный тест полного цикла save → eviction → load.

**P11. Что осознанно НЕ делаем в Phase 2.**
- Сжатие (gzip / RLE / delta-encoding) — Phase 12, контент-пайплайн.
- Region-файлы / packed format — мигрируем когда упрёмся.
- Multi-world UI / переключение миров — пока один захардкоженный.
- Версионирование данных за пределами bump-on-format-change — нет миграций v1→v2 в этой фазе.
- Page-alignment / mmap — overkill на этом масштабе.
- Persistence cover-maps / SDF-патчей / NavGrid — этих структур пока нет.
- Async-write через горутины — eviction редкий, fsync пропущен; синхронной записи в треде стриминга достаточно.
- Fsync — durability «после crash через 100 мс» нам не критична, экономим латенцию eviction.

---

## Три мильстоуна

### M2.1 — File format + IO primitives

**Цель.** Standalone-функции, которые умеют сериализовать/десериализовать чанки. Без ECS, без интеграции — чисто IO.

- Создать `systems/persistence.go`:
  - `const persistMagic = "RTSC"`, `const persistVersion uint16 = 1`
  - `const SaveDir = "./save/world-default"` (или принимать как параметр у `WriteChunk`/`ReadChunk` — последнее лучше для тестируемости)
  - `chunkFilePath(saveDir string, cc components.ChunkCoord) string` — возвращает `saveDir + "/chunks/" + fmt.Sprintf("%d_%d.bin", cc.X, cc.Z)`
  - `WriteChunk(saveDir string, cc components.ChunkCoord, heights *[components.ChunkResolution * components.ChunkResolution]float32) error`
    - Создаёт `saveDir/chunks/` через `os.MkdirAll` если нет
    - Сериализует header + heights в `bytes.Buffer` через `binary.LittleEndian`
    - Атомарная запись: `WriteFile(path+".tmp", buf, 0o644)` → `Rename(path+".tmp", path)`
  - `ReadChunk(saveDir string, cc components.ChunkCoord, out *[...]float32) (found bool, err error)`
    - `os.ReadFile`. Если `os.IsNotExist(err)` → `(false, nil)` (не ошибка, просто pristine).
    - Парсит header, проверяет magic, version, sanity-check на ChunkCoord совпадает
    - Заполняет `*out` из bytes
    - При битом файле → `(false, fmt.Errorf("...")) ` — caller решает, паниковать или fallback на procgen
- `.gitignore`: добавить `/save/`

**Проверяем.**
- Round-trip: записал случайные heights → прочитал → побайтно совпадает.
- Несуществующий файл → `(false, nil)`, без ошибки.
- Битый magic → ошибка, `found=false`.
- (Тестирование неформально — пробным main или временным debug-кодом; формальные тесты не пишем по политике CLAUDE.md.)

### M2.2 — Load on creation, Save on eviction

**Цель.** Чанки автоматически читаются с диска при создании и пишутся при выгрузке/выходе. Pristine — не пишутся.

- Добавить `Modified struct{}` в `components/terrain.go`.
- Создать `systems/terrain_load.go`, `TerrainLoadSystem`:
  - LODPolicy: `ActiveEvery: 0`, остальные disabled.
  - Filter: `ChunkCoord + HeightmapDirty` (без `Heightmap` — пытаемся заполнить).
  - В `Update`: для каждого dirty-чанка вызвать `ReadChunk(SaveDir, cc, &heights)`. При `found` — добавить `Heightmap{Heights: heights} + Modified`, снять `HeightmapDirty`. При `!found` — пропустить (gen разберётся следующей).
  - Применять изменения через deferred-archetype-changes pattern.
  - Регистрация в `main.go` **после** `terrain_streaming`, **до** `terrain_gen`.
- Дополнить `TerrainStreamingSystem`:
  - В InitUI добавить `modifiedMap` и `heightmapMap`.
  - В eviction-цикле: ДО `RemoveEntity` — если `modifiedMap.Has(id)` И `heightmapMap.Get(id) != nil` → `persistence.WriteChunk(SaveDir, cc, &heightmap.Heights)`. Логировать ошибку, не падать.
  - Затем уже текущая логика `UnloadMesh` + `RemoveEntity`.
- Shutdown flush в `main.go`:
  - Перед `defer rl.CloseWindow()` (или сразу после, чтобы выполнялось раньше — `defer` LIFO) добавить `defer flushModifiedChunks(app.World, persistence.SaveDir)`.
  - `flushModifiedChunks` строит filter на `WorldPos + Heightmap + Modified`, проходит, пишет каждый.

**Проверяем.**
- Запустил → на хм-холме нажал X (привет, M2.3 — для теста сейчас можно временно прицепить запись в main без полного Stamper) → прошёл за пределы Relevant → вернулся → меш не "проскакал" (т.е. heights загружены, не сгенерированы заново).
- В `./save/world-default/chunks/` появились файлы.
- Удалил папку, перезапустил → ландшафт чисто процедурный.
- Перезапуск без удаления → модифицированный регион сохранился.

### M2.3 — Stamp API + дебаг-клавиша

**Цель.** Полный цикл «изменил → сохранилось → перезагрузилось» доступен с клавиатуры и виден на экране.

- Создать `systems/stamp.go`:
  - `type HeightKernel func(dx, dz float32) float32`
  - `func Crater(depth, radius float32) HeightKernel` — косинусный профиль. Нормализация: `t = d / radius` ∈ [0,1], `factor = 0.5 * (1 + cos(pi*t))`, `delta = -depth * factor`.
  - `type Stamper struct { ... }` с полями для `TerrainChunkIndex` resource, `Heightmap`/`MeshDirty`/`Modified` Maps.
  - `func NewStamper(w *ecs.World) *Stamper` — пре-билдит handles.
  - `func (s *Stamper) StampHeightmap(center components.WorldPos, kernel HeightKernel, radius float32)`:
    1. Найти chunk-bounds: `minCC = center.Chunk + floor((center.Local.X - radius) / ChunkSize)`, аналогично для max и Z. Обычно 1×1 или 2×2.
    2. Для каждого `cc` в bounds: проверить `idx.Loaded[cc]`; пропустить если нет.
    3. Получить `heightmap := heightmapMap.Get(ent)`; пропустить если nil.
    4. Для каждой вершины (i, j) в `[0, ChunkResolution)`: вычислить world-coord, посчитать `dx, dz` от центра стампа, если `sqrt(dx² + dz²) ≤ radius` — `heights[j*ChunkResolution + i] += kernel(dx, dz)`.
    5. Поставить `MeshDirty` (если ещё не стоит) и `Modified` (если ещё не стоит).
- В `main.go`:
  - После `cameraSys.InitUI` (или рядом с другими "service-объектами"): `stamper := systems.NewStamper(app.World)`.
  - В render-loop, после WASD-блока, до `app.Tick`:
    ```go
    if rl.IsKeyPressed(rl.KeyX) {
        stamper.StampHeightmap(*anchorPos, systems.Crater(2.0, 4.0), 4.0)
    }
    ```
- Обновить HUD-текст: «X to drop a crater».

**Проверяем.**
- Запустил, X — в полу появилась вмятина, мгновенно (mesh пересобрался в том же тике).
- Прошёл за пределы Relevant → вернулся → вмятина на месте.
- Quit → restart → вмятина на месте.
- Стамп на границе двух чанков — вмятина непрерывна (кратер пересекает шов корректно), оба чанка на диске.

---

## Что считаем «закрытием Phase 2»

- `go run main.go` → ландшафт виден, X создаёт кратер, кратер переживает eviction и рестарт.
- `./save/world-default/chunks/` содержит файлы только для модифицированных чанков; pristine чанки не создают файлов.
- Persistence API скрывает форму хранения за двумя функциями (`WriteChunk` / `ReadChunk`) — будущий свап на region-файлы не заденет вызывающий код.
- Семантика маркеров (`HeightmapDirty` = needs initial fill, `MeshDirty` = needs rebuild, `Modified` = differs from procgen) согласована во всех системах терраина.

После этого — обновление ROADMAP, Фаза 2 → ✅, и переходим к выбору следующего шага. По плану — Фаза 3 (SDF-патчи + порталы), но возможно стоит вытянуть кусочек юнитной системы (Фаза 5) пораньше, чтобы было на чём проверять последующие смарт-объекты. Обсудим в момент перехода.

---

## Заметка про IO и масштаб

При Arma-масштабе (~16 км² = ~16K чанков 64×64) если **каждый** чанк будет модифицирован — это 16K файлов в одной директории. ext4 с htree выдержит, но `ls`/бэкапы начнут страдать. Реалистичная оценка для тестового геймплея: < 1K модификаций, никаких проблем.

**Триггер миграции на region-файлы:** когда увидим заметную задержку при запуске (>500мс на load всех чанков из active-зоны) или директория `chunks/` содержит >5K файлов. До этого — `WriteChunk`/`ReadChunk` API остаётся, мигрируется только реализация под капотом.
