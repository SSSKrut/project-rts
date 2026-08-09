# Phase 7.5 — рабочий план

Observability / Tooling. Между Phase 7 и Phase 9 вставляется короткая инструментальная фаза: профайлинг тиков, FPS-HUD, entity-counts. Цель — на каждом следующем тике оптимизаций (Phase 9 формации, Phase 10 utility-AI per-tick, Phase 11 баллистика) иметь живой сигнал «что стало дороже / дешевле», а не гадать. Делаем минимум, который окупится с первого использования и не возмутит архитектуру.

**Чего в этой фазе сознательно НЕТ.** GPU breakdown (draw-call count, gpu ms) — raylib не отдаёт это дёшево, и без него HUD остаётся читаемым; интегрировать после Phase 15, когда подключим реальные модели. Sampling-CPU-profile / pprof / Tracy-timeline — другой инструмент, своя цена, своя ниша; runtime/pprof и так доступен через `net/http/pprof` если понадобится глубокое расследование. Per-tier breakdown системы (Active/Relevant/Dormant отдельно) — P3 ниже. Persistence снапшотов профиля на диск — пока нет, captures идут в stdout. Алармы по budget-overrun — Phase 16 polish.

ROADMAP помечает эту фазу как 🚧, чтобы линейный список не сбивался. Этот файл — рабочий, обновляется по ходу.

---

## Решения, которые лочим до начала кода

**P1. Что измеряем.**

- FPS — `rl.GetFPS()`, всегда виден.
- Frame ms — `rl.GetFrameTime()` × 1000, всегда виден.
- Tick ms (sum) — суммарное время одного `app.Tick`, замер в `core.App`.
- Per-system tick ms (median over rolling window) — замер вокруг `sys.Update` в `App.Tick`.
- Entity counts по архетипам — chunks (loaded/active/relevant), props, walls, floors, stairs, cover-slots, units, weapons, transition-edges.
- Go heap — `runtime.MemStats.HeapAlloc` округлённо в МБ, обновляется раз в 1 сек (ReadMemStats дорог, не каждый кадр).

**Чего нет:** render ms / present ms отдельно (raylib не отдаёт удобно). Считаем как `frame_ms - tick_ms = «всё остальное»`, этого достаточно для разделения «лагает ECS» vs «лагает рендер».

**P2. Storage — `Profiler` struct в `core/profiler.go`.**

Отдельный файл рядом с `app.go`. `Profiler` — value-by-pointer на `App`, не глобал. Не singleton-resource (не нужен в ECS-фильтрах), просто поле в `App` плюс getter для main.go.

```go
type Profiler struct {
    samples [profileWindow]profileFrame
    head    int      // куда писать следующий кадр
    full    bool     // прошли первый круг?
}

type profileFrame struct {
    tickTotalNs  int64
    perSystemNs  [maxSystems]int64   // индекс = индекс системы в App.systems
}

const profileWindow = 60   // 1 секунда при 60 fps
const maxSystems   = 32    // запас, сейчас зарегистрировано ~17
```

Fixed-size массивы — никаких аллокаций per-tick. `maxSystems = 32` — запас на ×2 от текущих 17 систем, при переполнении ronseal-panic в `AddSystem`.

**P3. Гранулярность — per-system aggregated по всем тирам.**

`App.Tick` делает `sys.Update` до трёх раз за тик (Active/Relevant/Dormant). Замеряем суммарно. Аргументы — в обсуждении выше: разные тиры одной системы практически всегда пропорциональны по cost'у, разделение утроит строки HUD без полезного сигнала. Когда станет нужно (Phase 11 boids tier-split?) — добавим, форма Profiler позволяет: `perSystemNs[sys][tier]` через 2D расширение.

**P4. Median, не mean.**

GC pause или один тяжёлый chunk-mesh upload смещают mean заметно. Median по `profileWindow=60` сэмплам гасит хвосты. Стоимость медианы — копия 60-element массива и `sort.Slice` раз в кадр на ~17 строк HUD. Это ≈ 50 µs/кадр в worst case, пренебрежимо. Альтернатива (без сортировки) — quickselect, но 60 элементов сортируются за ~µs, не стоит усложнения.

**P5. UI — toggle на `P`.**

Свёрнутый вид — всегда виден в правом-верхнем углу, две строки:
```
60 FPS  16.6 ms (tick 3.2 / other 13.4)
heap 24 MB  ents 4127
```

Расширенный — пока зажат `P`, отрисовывается ниже свёрнутого, табличкой:
```
─── systems (ms median) ───
  terrain_streaming   0.04
  spatial_bake        1.20
  unit_movement       0.18
  vision              0.05
  terrain_mesh        0.85
  ...
─── entity counts ───
  chunks: 49 active / 121 total
  walls: 64  floors: 6  stairs: 4  cover-slots: 156
  units: 12  weapons: 12
  transitions: 18
```

`Ctrl+P` (нажатие, не hold) — `fmt.Println` снапшота в pretty-формате. Удобно для записи «до/после» при сравнении вариантов оптимизации.

Не используем существующий левый HUD (Phase 6/7 census в нижней части) — Phase 7.5 HUD заменит часть из них (e.g. units / cover-slots / nav chunks), оставшиеся (`Roads: …`, `Buildings: plans=…`) переедут или будут удалены. Чистка — M7.5.3.

**P6. Trace в файл за build-tag'ом `trace`.**

Сценарий: «прогнать смоук-тест PHASE-7 M7.8, получить полный таймшерис в файле, диффать между «до оптимизации» и «после»». Stdout-снапшот (P5, Ctrl+P) — это один срез, не история. Для регрессий и сравнения вариантов нужен непрерывный лог.

Форма:
- Build tag `trace`. Без тэга — кода для file-IO нет вовсе, zero overhead. Сборка с тэгом: `go build -tags trace -o rts main.go`.
- Симметричная пара файлов: `core/trace_on.go` (`//go:build trace`) — реальная имплементация `Tracer`. `core/trace_off.go` (`//go:build !trace`) — стабы с теми же сигнатурами, тело пустое. Никаких `if-tag` внутри `app.go` / `profiler.go` — выбор делает компилятор.
- CLI flag `-trace=path.jsonl` (через `flag` пакет в `main.go`, тоже за `//go:build trace`-обвязкой). Без флага сборка-с-тэгом ведёт себя как обычная.
- Формат — JSONL, одна строка = один кадр. Поля: `frame`, `elapsed_ms`, `fps`, `frame_ms`, `tick_ms`, `systems` (map name→ms median), `entities` (map archetype→count), `heap_mb`. JSON чтобы можно было ad-hoc анализировать через `jq` / pandas; CSV не подходит из-за вложенных map'ов.
- Записываем каждый кадр, без буферизации сверх того что даёт `bufio.Writer` со стандартным 4 KB. На смоук-сценарии 30 сек × 60 fps × ~250 B/кадр = ~450 KB — компактно даже без gzip.
- Дополнительно — event markers. Хоткей `M` (mark) под `trace`-сборкой пишет в текущий JSONL строку вида `{"event":"mark","frame":N,"elapsed_ms":...}`. Удобно вручную помечать «здесь начался стресс-тест» в reproducible сценарии.

Что **не** делаем:
- Headless-режим (без окна). Требовал бы вырывания рендера, и смоук-тест Phase 7.5 всё равно нужно отсматривать глазами. Если когда-то понадобится — `-tags=headless` другой опт.
- Replay (повторить последовательность ввода из trace-файла). Phase 16 polish, отдельная задача.
- Бинарный формат. JSONL разбирается чем угодно, размер не проблема в Phase 7.5-масштабе.
- Стрим в сеть / в shared memory. Файл — единственный sink.

---

## Пять мильстоунов

### M7.5.1 — Profiler в core.App

**Цель.** Каждый тик `App.Tick` записывает время в `Profiler`, доступный из main.go через `app.Prof`.

**Делаем:**
- `core/profiler.go` — `Profiler`, `profileFrame`, константы `profileWindow` / `maxSystems`, методы `Begin()/End()` для границ тика, `RecordSystem(idx int, ns int64)`, `MedianTotal() time.Duration`, `MedianSystem(idx int) time.Duration`, `SystemName(idx int) string`.
- `core/app.go` — поле `Prof Profiler`, инициализация в `NewApp()`. В `Tick`: `t0 := time.Now()`, конец — `app.Prof.RecordTick(time.Since(t0))`. Вокруг `sys.Update`: `ts := time.Now(); sys.Update(...); app.Prof.RecordSystem(i, time.Since(ts))`.
- Перенумерация: чтобы `RecordSystem` использовал стабильный индекс, `App.AddSystem` сохраняет порядок (уже так). Индекс = позиция в `app.systems`.

**Проверяем.** Запуск + `fmt.Printf("%+v\n", app.Prof.Snapshot())` раз в секунду — цифры есть, FPS не упал измеримо (≤ 0.5% относительно «без профайлера», прикинуть по `rl.GetFPS()` на 1000 фреймов до/после).

### M7.5.2 — Свёрнутый HUD

**Цель.** Правый верхний угол показывает FPS / frame ms / tick ms / heap / total entity count постоянно.

**Делаем:**
- В main.go — функция `drawCollapsedProfHUD(app *core.App, screenW int32)`. Считает median, форматирует, рисует двумя строками. Шрифт 18 / 16, чтобы не мешать сцене.
- Total entity count — через `world.Stats().NumEntities` или ad-hoc счётчик; если Ark не отдаёт легко — суммируем по фильтрам, что уже есть в HUD.
- Heap — `runtime.ReadMemStats` раз в 60 кадров (не каждый), кешируем последнее значение в `Profiler.lastHeapMB`.

**Проверяем.** На пустой сцене (12 юнитов, 3 здания) FPS пишет 60, frame ms ~16.6, tick ms < 5, heap < 50 MB. После рывка движения (load новых чанков) видно временный пик tick ms.

### M7.5.3 — Расширенный HUD (hold P) + чистка старого

**Цель.** Зажатый `P` — таблица per-system median ms + entity counts. Старые «Roads / Buildings / Trenches» строки из левого HUD — либо удаляем, либо консолидируем.

**Делаем:**
- `drawExpandedProfHUD` — таблица под свёрнутым блоком. Сортировка по median desc, top-N (8?) систем. Под таблицей — entity counts из P5.
- Левый HUD очистка: убрать дублирующиеся census-строки (units / nav chunks / cover slots переезжают в правую таблицу). Оставить только инструкции по управлению.
- TransitionRegistry size — через `len(reg.Out)` + сумма `len(edges)` по всем ключам, считаем раз в кадр (дёшево).

**Проверяем.** На сцене с активным движением (RMB-приказы на 4 юнитов) `terrain_mesh`/`spatial_bake` в холодном кэше — топ-2, `unit_movement` стабильно мал. При зажатии H (Stop) — `unit_movement` падает к 0.

### M7.5.4 — Snapshot dump на Ctrl+P + smoke test

**Цель.** `Ctrl+P` — `fmt.Println` в stdout текущего снапшота (median по всему окну) в формате пригодном для копипасты в issue / в этот файл.

**Делаем:**
- `Profiler.PrintSnapshot()` — pretty-print, две колонки `name`/`median ms`, единый width.
- Хоткей в main.go: `if rl.IsKeyPressed(rl.KeyP) && (rl.IsKeyDown(rl.KeyLeftControl) || rl.IsKeyDown(rl.KeyRightControl)) { app.Prof.PrintSnapshot() }`.
- Финальный smoke-test: сценарий PHASE-7 M7.8 (marquee 4 юнитов, ПКМ внутрь дома, чек кратера). На каждом этапе нажать Ctrl+P, убедиться, что вывод осмысленный и формат стабильный.

**Проверяем.** Вывод копируется одним блоком, цифры стабильны (не на лету меняются между моментом нажатия и моментом печати).

### M7.5.5 — Trace в файл за build-tag'ом

**Цель.** `go build -tags trace` + `./rts -trace=run.jsonl` пишет per-frame JSONL с median ms / FPS / entity counts. На «обычной» сборке вся кодовая ветка отсутствует, оверхеда нет. Хоткей `M` ставит event-marker в trace.

**Делаем:**
- `core/trace_on.go` (`//go:build trace`) — `Tracer` struct, `Open(path)`/`WriteFrame(*Profiler, ents map)`/`Mark(label)`/`Close()`. Использует `encoding/json` + `bufio.Writer`.
- `core/trace_off.go` (`//go:build !trace`) — те же сигнатуры с пустыми телами. `Tracer` — пустой struct.
- `core/app.go` — поле `Trace Tracer`, вызов `app.Trace.WriteFrame(...)` в конце `Tick`. На обычной сборке это `func() {}`, инлайнится в ноль.
- `main_trace.go` (`//go:build trace`) — `init()` парсит флаг `-trace=path`, открывает Tracer; в game-loop регистрирует хоткей `M`.
- `main_notrace.go` (`//go:build !trace`) — пустой init, чтобы `flag.Parse()` не падал на неизвестном флаге. Альтернатива — `flag.CommandLine.Init` с `ContinueOnError`, но раздельные файлы прозрачнее.
- README-фрагмент / CLAUDE.md обновление: «как собирать с трейсом, где лежит вывод, минимальный jq-рецепт».

**Проверяем.**
- `go build` (без тэга) — бинарь работает как раньше, размер не вырос измеримо.
- `go build -tags trace -o rts main.go && ./rts -trace=/tmp/run.jsonl` — каждые ~16 мс новая строка в файле. Проверка `jq -r '.tick_ms' /tmp/run.jsonl | head` отдаёт цифры.
- Нажатие `M` пишет `{"event":"mark",...}`. `jq 'select(.event=="mark")' /tmp/run.jsonl` находит маркеры.
- Один полный прогон сценария M7.5.4 с трейсом, файл сохранён рядом с этим планом как `/tmp/phase-7.5-baseline.jsonl` (reference baseline для будущих сравнений).

---

## Что считаем «закрытием Phase 7.5»

- `Profiler` в `core/`, инструментированы `App.Tick` границы и каждый `sys.Update`.
- Свёрнутый HUD (FPS / frame ms / tick ms / heap / entity count) виден всегда.
- Расширенный HUD на hold `P` — per-system median table + entity counts по архетипам.
- Snapshot dump на Ctrl+P в stdout.
- Старый левый HUD почищен от дублирующихся census-строк.
- Build-tag `trace`: `go build -tags trace` + `-trace=path.jsonl` пишет per-frame трейс. Обычная сборка кода tracer'а не содержит (overhead = 0).
- Overhead профайлера ≤ 0.5% FPS на тестовой сцене (~200 ns на `time.Since` × ~17 систем × 60 fps = 200 µs/sec).
- Baseline trace смоук-сценария сохранён как `/tmp/phase-7.5-baseline.jsonl` для будущих диффов.
- ROADMAP обновлён, PHASE-7.5.md переезжает в `.claude/old/`.

После — переход к Phase 9 (Squads + Formations).

---

## Заметки на полях

- **Почему 60-кадров окно, а не дольше.** 60 = 1 сек при 60 fps — это естественный «глаз видит сейчас» масштаб. Длиннее окно (e.g. 300 = 5 сек) гасит реакцию HUD'а на спайки от текущей оптимизации, что неудобно при «изменил код, перезапустил, смотрю стало лучше». Если понадобится сравнение «long-term», PrintSnapshot уже даёт фиксированный срез.

- **Median vs p99.** Median покажет «как обычно», p99 покажет «худший кадр». На solo-фазе важнее первое — оптимизировать «нормальный кадр», а тяжёлые спайки (chunk-mesh upload, GC pause) лечатся отдельно по причине. p99 добавим если возникнет реальная нужда (Phase 11 ballistics на 200 юнитов — там p99 будет интереснее).

- **Heap раз в секунду.** `runtime.ReadMemStats` собирает stop-the-world snapshot, стоит ~100-200 µs. Каждый кадр это уже 0.6-1% бюджета — не критично, но без надобности. Раз в секунду через `app.elapsed % time.Second` достаточно: leak'и проявляются на сек/мин шкале, не на per-frame.

- **Profiler как часть core, а не отдельный пакет.** Альтернатива — `profiler/` пакет, чтобы `core` не знал о замерах. Текущий вариант проще: один файл рядом с App, нулевой overhead имплементации. Если в Phase 13+ появится Strategic AI с собственным «бюджетом» — вынесем в отдельный пакет с интерфейсом, пока преждевременно.

- **Капчер в stdout, не в файл.** Файл потребовал бы выбора пути / overwrite политики / rotation. Stdout — один копипаст в Discord/issue/CLAUDE-memory, конец истории.

- **Что НЕ делаем сразу, но держим в голове:**
  - Если Phase 9 формации добавят O(N²) cohesion check между юнитами одного squad'а — Profiler сразу покажет рост `unit_movement` / `squad_formation`. Это первый реальный use case для измерений.
  - Если Phase 10 Utility AI окажется недостаточно throttled (re-evaluate каждый тик вместо «по триггеру»), `tactical_ai` система выскочит топ-1 в таблице — мгновенный сигнал, что time-slicing не работает.
  - Если Phase 11 ballistics naive raycast'ит каждый Bullet entity per-tick — счётчик ballistic-entities + время системы покажет проблему до того как FPS упадёт.
