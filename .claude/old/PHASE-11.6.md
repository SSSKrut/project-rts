# Phase 11.6 — рабочий план

Микро-полировка после Phase 11.5. Точечные правки производительности и race-safety без архитектурных сдвигов. Phase 11.5 ввела worker pool + параллелизацию, но на текущем тест-scene (12 юнитов) это дало перформанс-regression в `unit_movement` (с ~0.00 мс до 0.02-0.05 мс по median'у profiler'а) из-за:

- per-tick allocations (`make([]…)` каждый Update во всех 4 параллельных системах);
- ParallelFor overhead на маленьких N (channel ops + WaitGroup + cache misses при split'е на 8 workers);
- map-аллокаций в `wallsByChunk` у VisionSystem каждый тик.

Плюс закрываем известный «forward-aware concern» — preemptive per-worker leaveBuffer в FormationSystem (сейчас race-safe только пока `cohesionEjectionEnabled = false`, Phase 15 включит флаг и race станет реальным).

**Что в Phase 11.6 сознательно НЕТ.** Никаких новых фич. Никаких изменений симуляционной семантики. Никаких изменений API систем (конструкторы / LODPolicy / порядок Update). Spatial hashing — это Phase 14. Активация time-slicing'а (`BucketCount > 1`) — Phase 14. Hot-tune worker pool size — Phase 21+. Сюда не лезем.

ROADMAP — высокоуровневый трекер. Этот файл — рабочий, обновляется по ходу выполнения.

---

## Решения, которые лочим до начала кода

**P1. Reusable snapshot buffers как поля систем.**

Заменяем per-tick `make([]T, 0, N)` на reuse через slice-of-length-zero pattern:

```go
type UnitMovementSystem struct {
    // ... existing fields
    workBuf       []unitWork        // reused across ticks; reset to len=0 each Update
    neighbourBuf  []unitNeighbour
}

func (sys *UnitMovementSystem) Update(ctx core.UpdateContext) {
    sys.workBuf = sys.workBuf[:0]
    sys.neighbourBuf = sys.neighbourBuf[:0]
    // ... append into them as before
}
```

Это устраняет heap-аллокации для буферов после первого тика (slice grows once в первый Update, после — стабильный capacity). GC pressure снимается полностью.

Применяется к 4 системам:
- `UnitMovementSystem`: `workBuf` + `neighbourBuf`.
- `VisionSystem`: `unitsBuf` + `seersBuf`. Map `wallsByChunk` — отдельная история, см. P2.
- `FormationSystem`: `workBuf`. `leaveBuffer` — отдельная история, P4.
- `SquadMacroPathSystem`: `workBuf`.

**Внимание про concurrency.** Эти буферы заполняются в **serial pre-pass** до ParallelFor, читаются workers'ами в parallel main pass (read-only по индексу), не модифицируются после. Безопасно. Не используем эти буферы из других системы — каждая система держит свои.

**P2. VisionSystem map `wallsByChunk` — заменяем на `sync.Pool` или ручной reset.**

Map'у нельзя просто `m[:0]`. Варианты:
- **(a) Сохранять map'у как field, `clear(m)` в начале Update.** Go 1.21+ `clear` оставляет capacity. Самое простое.
- **(b) `sync.Pool` для map'ы.** Get/Put pattern. Сложнее, выгоды на одиночной reused map'е нет.
- **(c) Snapshot walls как flat slice вместо map'ы.** Walls indexed by `[]wallEntry{Chunk, ...}`, lookup по chunk через linear scan или sort+binary search. Лучше для cache, но переписывает access pattern в `process` функции.

Выбираем **(a)** — `clear(sys.wallsByChunk)` в начале Update, map остаётся allocated. Если Go 1.21 не везде есть — `for k := range m { delete(m, k) }`. Проверяем go.mod на minimum version.

**P3. Small-N serial fallback в `WorkerPool.ParallelFor`.**

Текущий fallback в worker_pool.go:
```go
if p == nil || p.workers <= 1 || total == 1 {
    fn(0, total)
    return
}
```

Расширяем порогом — на маленьком N split не амортизирует overhead'а:

```go
// SerialThresholdHint — ниже этого N ParallelFor выполняет работу
// синхронно в текущей горутине. Откалибрировано на ParallelFor-overhead'е
// (~5-10 μs на 8 workers) против step-cost'а (~1 μs на unit).
const SerialThresholdHint = 64

if p == nil || p.workers <= 1 || total <= SerialThresholdHint {
    fn(0, total)
    return
}
```

На тест-scene (12 units, 3 squads) каждая система пойдёт по serial-пути — ParallelFor-overhead исчезнет. На целевом scale (1000+ units) автоматически активируется параллель.

**Развилка**: per-system threshold vs global. Per-system аккуратнее (VisionSystem дороже на one-unit-cost'е чем UnitMovementSystem, threshold должен быть ниже). Но это тюнинг под целевой scale, которого ещё нет. Global = 64 — разумный стартовый дефолт, можно подвинуть в Phase 14 при первых перформанс-тестах с сотнями entity. Решаем — **global** для Phase 11.6.

**P4. Per-worker leaveBuffer в FormationSystem (preemptive race fix).**

Сейчас:
```go
leaveBuffer := make([]ecs.Entity, 0)
sys.pool.ParallelFor(len(work), func(start, end int) {
    for i := start; i < end; i++ {
        sys.processSquad(world, work[i], &leaveBuffer)  // RACE if append from multiple workers
    }
})
```

С `cohesionEjectionEnabled = false` append не вызывается, race теоретическая. Phase 15 включит флаг → race реальная. Фиксим сейчас, пока тема свежая.

Pattern: per-worker буфер, передаётся в processSquad через worker-local context:

```go
// processSquad accumulates stragglers into a worker-local buffer.
func (sys *FormationSystem) processSquad(world *ecs.World, w formationWork, localBuf *[]ecs.Entity) {
    // ... existing logic; *localBuf = append(*localBuf, mem) — no race because each
    // worker owns its own pointer.
}

func (sys *FormationSystem) Update(ctx core.UpdateContext) {
    // ... snapshot
    workers := sys.pool.Workers()
    if workers < 1 { workers = 1 }
    workerBufs := make([][]ecs.Entity, workers)  // per-worker output

    sys.pool.ParallelFor(len(sys.workBuf), func(start, end int) {
        // Determine which worker we're in — derive from the chunk index. Since
        // ParallelFor splits into `workers` contiguous chunks, the chunk index
        // = start * workers / total (roughly). Simpler: pass a workerIdx via a
        // per-chunk closure (see implementation note).
        // ...
    })

    for _, b := range workerBufs {
        for _, e := range b {
            sys.squadService.Leave(e)
        }
    }
}
```

**Implementation note про worker index:** `ParallelFor` сейчас не передаёт worker-index в `fn`. Расширим API:

```go
// ParallelForIndexed is like ParallelFor but passes a chunk index (0..workers-1)
// to fn — useful for per-worker scratch buffers. Each chunk is invoked exactly
// once, so the chunkIdx is also stable per work-range.
func (p *WorkerPool) ParallelForIndexed(total int, fn func(chunkIdx int, start, end int))
```

Это **non-breaking** — старый `ParallelFor` остаётся, новый — opt-in. FormationSystem использует Indexed. Остальные системы — старый API (им worker-index не нужен).

**P5. Race-detector test routine + smoke test в core/.**

Добавляем в `core/worker_pool_test.go` test, который имитирует workload UnitMovement: snapshot N elements, ParallelFor → каждый worker пишет в свой slot (race-safe), читает из общего slice (race-safe). Race detector должен быть чист.

Плюс одну строку в `CLAUDE.md` про recommended dev workflow:
```bash
go test -race ./core/      # для core/ — quick
go run -race . -workers=4  # для full sim — погонять 30 сек
```

Не критично для Phase 11.6 closure, но улучшает development hygiene.

---

## Пять мильстоунов

### M11.6.1 — Profile/diagnose unit_movement regression

**Цель.** Подтвердить hypothesized root cause (allocations + ParallelFor-overhead) измерением до фикса. Снимает baseline для M11.6.2-3 verification.

**Делаем:**
- Запустить test scene с `-tags trace -trace=/tmp/before.jsonl`, погонять 30 сек с движущимися squad'ами. `jq '.systems.unit_movement' /tmp/before.jsonl | sort -n | tail -20` — top median ms.
- `Ctrl+P` snapshot во время движения — записать numerical baseline для unit_movement / vision / formation / squad_macro_path.
- Опционально: `go test -bench=. -benchmem ./systems/...` если есть benchmark'и (вероятно нет, но можно набросать quick BenchmarkUnitMovement с фикс-сценой 12 units).
- Сохранить baseline в комментарий в этот файл (или в commit-message).

**Проверяем.** Числа на руках. Знаем ожидаемое улучшение после M11.6.2-3 (allocations исчезнут, ParallelFor выключится для 12 units → expected median 0.005-0.010 ms).

### M11.6.2 — Reusable snapshot buffers (4 системы)

**Цель.** UnitMovement / Vision / Formation / SquadMacroPath держат snapshot буферы как fields. `Update` начинает с `buf = buf[:0]`, не `make`. Map `wallsByChunk` тоже reused через `clear()`.

**Делаем:**
- `systems/unit_movement.go`: добавить `workBuf []unitWork`, `neighbourBuf []unitNeighbour` в struct. Reset в начале Update.
- `systems/vision.go`: добавить `unitsBuf []visionUnit`, `seersBuf []seerWork`, `wallsByChunk map[ChunkCoord][]losWall`. Reset/clear в Update. **Проверить go.mod minimum version** — если Go 1.21+, можно `clear(m)`; иначе ручной delete loop.
- `systems/formation.go`: добавить `workBuf []formationWork`. Reset.
- `systems/squad_macro_path.go`: добавить `workBuf []macroPathWork`. Reset.
- `NewXxxSystem` конструкторы инициализируют буферы с разумной initial capacity (`make([]T, 0, 64)`) чтобы первый Update не делал growth-allocations.

**Проверяем.** `go run . -workers=4` — visual smoke test (юниты двигаются как раньше). `Ctrl+P` snapshot — unit_movement / vision / formation / squad_macro_path median должны просесть на 30-50% (allocations исчезли). `go test ./core/` — pool tests passing.

### M11.6.3 — Small-N serial fallback в ParallelFor

**Цель.** ParallelFor на N < 64 выполняется serial inline. Текущий тест-scene (12 юнитов / 3 squad'а) полностью serial, ParallelFor overhead исчезает.

**Делаем:**
- `core/worker_pool.go`: добавить `SerialThresholdHint = 64` constant. Обновить `ParallelFor`:
  ```go
  if p == nil || p.workers <= 1 || total <= SerialThresholdHint {
      fn(0, total)
      return
  }
  ```
- Comment про rationale (per-system threshold возможно в Phase 14 если scale потребует).

**Проверяем.** `Ctrl+P` snapshot после M11.6.2 + M11.6.3 — unit_movement должен опуститься обратно к ~0.005 ms (sub-display threshold, отображается как «0.00»). Vision/formation/squad_macro_path аналогично — все теперь serial, без overhead.

### M11.6.4 — Per-worker leaveBuffer в FormationSystem + ParallelForIndexed API

**Цель.** Preemptive race-safety fix. Когда Phase 15 включит `cohesionEjectionEnabled = true`, FormationSystem безопасен.

**Делаем:**
- `core/worker_pool.go`: добавить `ParallelForIndexed(total, func(chunkIdx, start, end int))`. Internally — same logic как ParallelFor, но передаёт `chunkIdx` в fn. Старый ParallelFor остаётся для обратной совместимости.
- `systems/formation.go`: `Update` использует `ParallelForIndexed`. `workerBufs := make([][]ecs.Entity, workers)` создаётся в Update (или reused как field — pattern P1). processSquad принимает `*[]ecs.Entity` локального буфера. Serial post-pass merges все `workerBufs[i]` в один и применяет.
- Заметка: даже хотя ejection off, pattern активирован — buffer-merge-pass проходит на пустом workerBufs, no-op overhead.

**Проверяем.** Visual smoke test — squad'ы держат формацию как раньше. Опциональный stress test: temporarily включить `cohesionEjectionEnabled = true`, run `go run -race . -workers=4`, дать squad'у длинный приказ через лес где члены отстают — должны eject'нуться без race detector hits. После теста — revert флаг к false.

### M11.6.5 — Race-detector smoke + CLAUDE.md note + closure

**Цель.** Race-test infrastructure готов. Документация обновлена. Phase 11.6 closure.

**Делаем:**
- `core/worker_pool_test.go`: добавить test `TestParallelForRaceSafe` — N parallel writers пишут в disjoint indices общего slice, потом главный goroutine читает и проверяет. Запускается с `go test -race ./core/`.
- `CLAUDE.md`: добавить в «Adding a new system» section короткий para:
  > **Race-detector check.** Системы с `sys.pool.ParallelFor(...)` должны гонять `go test -race ./core/` (для базовых case'ов) и периодически `go run -race . -workers=4` на 30+ сек движения. Race-detector ловит почти все concurrent write'ы, кроме false-sharing на cache-line'ах — последнее не блокирующее, оптимизация Phase 14+.
- ROADMAP.md: Phase 11.6 → ✅.
- ISSUES.md: проверить — никаких новых regression'ов после M11.6.2-4. Если есть — записать.

**Проверяем.** `go test -race ./core/` — pass. `go run -race . -workers=4` 30 сек — no race report. Final `Ctrl+P` snapshot: median'ы 4 параллельных систем суммарно меньше pre-11.5 baseline'а (за счёт removed allocations + serial fallback).

---

## Что считаем «закрытием Phase 11.6»

- 4 параллельные системы (UnitMovement / Vision / Formation / SquadMacroPath) используют reusable snapshot buffers — no per-tick `make()`.
- VisionSystem `wallsByChunk` map reused через `clear()`.
- `WorkerPool.ParallelFor` имеет serial fallback на N < 64 (`SerialThresholdHint`).
- `WorkerPool.ParallelForIndexed` API добавлен; FormationSystem использует его + per-worker leaveBuffer (race-safe preemptive).
- `core/worker_pool_test.go` имеет race-safety test, проходит с `-race`.
- `CLAUDE.md` упоминает race-detector dev workflow.
- unit_movement median ms вернулся к sub-display уровню (~0.005 мс или меньше).
- Симуляция семантически идентична — юниты двигаются как раньше, формации держат, приказы выполняются.

После этого — обновление ROADMAP, Phase 11.6 → ✅, переход к Phase 12 (Unit roles) с чистым перформанс-baseline'ом.

---

## Заметки на полях

- **Buffer reuse не делает систему race-unsafe.** Snapshot заполняется в **serial pre-pass**, читается workers'ами в read-only по фиксированным индексам. Если бы workers ещё _append'или_ в общий buf — race. Но они только читают `buf[i]` и пишут в свои компонентные pointer'ы. Safe.

- **`clear(map)` требует Go 1.21+.** Проверить `go.mod` (`go 1.21` или выше). Если ниже — обновить версию модуля или fallback на ручной `for k := range m { delete(m, k) }`. Я склоняюсь к обновлению go.mod до 1.21 — релиз был 2 года назад, нет резона держать старее.

- **`SerialThresholdHint = 64` — почему 64?** Эмпирически: ParallelFor с 8 workers'ами добавляет ~5-10 μs overhead. На step-cost'е ~1 μs/unit это окупается при N ≥ ~40. Округлено до 64 как round-number power-of-2. На реальной сцене бенчмарки могут показать что 32 или 128 лучше — это можно подвинуть.

- **Per-system threshold vs global.** Сейчас global. Если когда-нибудь VisionSystem (дороже step-cost) станет хотеть threshold=32 а UnitMovement держать 64 — добавится поле `SerialThreshold int` в систему. Не сейчас.

- **`ParallelForIndexed` vs передача chunk-id через locals.** Можно было обойтись без нового API: создать N closures с захватом id и каждую отдельно отдать в `jobs` channel. Но это требует exposed access к jobs channel'у, нарушает encapsulation pool'а. Чистый API через Indexed — лучше.

- **Per-worker leaveBuffer overhead — реально насколько?** На текущей сцене (3 squad'а, all serial из-за M11.6.3) — `workerBufs` это `[][]ecs.Entity` длины 1 (`workers = 1` для serial). Один аллокированный slice. Merge pass проходит мгновенно. Overhead в полностью-active-режиме (8 workers) — 8 slice headers (~200 байт), GC-cleanable. Ничтожно.

- **Vision `wallsByChunk` могла бы быть flat slice вместо map'ы.** Это option (c) из P2. Не делаем сейчас — `clear(map)` достаточен для перформанса 11.6, flat slice — это оптимизация Phase 14+ (combat) когда стен будет 1000+ и cache-pressure начнёт давить. Альтернатива остаётся в backlog'е.

- **VisionSystem `units` + `seers` дублируют entity?** Технически да — `visionUnit{ent, pos, chunk}` для targets + `seerWork{ent, pos, yaw, vision, aware}` для seers. Каждый Unit попадает в оба буфера. Это правильно для текущей формы (targets дёшевые value-copy, seers держат pointer'ы). Можно слить в один тип-универсал, но получится unused-field bloat. Оставляем как есть.

- **`go.mod` go version.** Проверить — если 1.19 или 1.20, обновить до 1.21 минимум (для `clear()`). 1.22+ ещё лучше (range-over-int loop'ы). Pin'ить higher не обязательно.

- **Что если Phase 12 (Roles) добавит ещё системы которые ParallelFor'ятся?** Скажем будущая `RoleAssignmentSystem` или per-role behaviour. Pattern reusable buffers — копи-пэйст с UnitMovement. Закодифицировать в `CLAUDE.md` "Adding a new system" section как best practice уже сейчас.

- **Профайлер сам показывает unit_movement в 0.05 мс — это всё или только Active tier?** Существующий Profiler (Phase 7.5) суммирует по тирам внутри Tick. После Phase 11.5 LOD-tier'ов в этих системах нет → один LODPolicy.ActiveEvery=0 = вызов каждый тик. Так что 0.02-0.05 мс — это total для всех 12 units. После 11.6 ожидаем ~0.005 мс.

- **Что НЕ в Phase 11.6.** Spatial hashing (Phase 14). Активация time-slicing (Phase 14). Per-role LOD (Phase 12 / 13). Hot-tune worker count (Phase 21+). Audio-system параллелизация (никогда, audio это serial по API raylib). Render-LOD (Phase 21+). Save/Load (Phase 25).
