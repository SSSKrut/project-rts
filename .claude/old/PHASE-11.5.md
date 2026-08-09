# Phase 11.5 — рабочий план

Архитектурный refactor: переход от binary LOD-gating симуляции к универсальной симуляции + hash-bucket time-slicing + worker pool. Без этой фазы дальнейшие phase (combat / vehicles / aviation / particles) будут strugglить с масштабом дивизионных боёв, и каждая будет вынуждена обходить LOD-gating индивидуально (мы это уже видели в Phase 11 — фикс ISSUES #4 не сработал из-за тонкостей LOD-tier transitions).

Phase 11.5 — **переоснащение, не feature**. Игрок ничего нового не увидит, но архитектура станет готова к 1000+ юнитам и параллельной обработке. Без неё Phase 14 Combat встретит O(N²) vision/raycast'ы и однопоточную симуляцию, что задавит даже на 200 юнитах.

**Что в Phase 11.5 сознательно НЕТ.** Сами hash-bucket'ы _активно_ не используются — bucketCount = 1 для всех систем (то есть каждая система пробегает всех entity каждый Update). Это даёт chassis для time-slicing'а готовый, но не активирует его пока. Активация — Phase 14 (vision/AI станут дорогими). Particle system — Phase 14. Spatial hashing — Phase 14. Aviation — Phase 16.5. Hot-tune worker pool size — заглушка в API, реальная реализация — Phase 21+ (UI settings panel).

ROADMAP — высокоуровневый трекер. Этот файл — рабочий, обновляется по ходу выполнения.

---

## Решения, которые лочим до начала кода

**P1. Universal simulation: drop LOD-tier-gating из всех симуляционных систем.**

Текущие системы с per-tier branching: `UnitMovementSystem`, `FormationSystem`, `SquadMacroPathSystem`, `VisionSystem`, `OrderResolverSystem` (если есть). После refactor'а у каждой — **единый filter** без LODActive/LODRelevant/LODDormant. Update обрабатывает все matching entity за один проход.

`LODPolicy` остаётся как структура (для совместимости с `core.App.Tick` диспетчером), но используется по-новому:
- `ActiveEvery: <interval>` — единственный интервал, на котором система фактически запускается.
- `RelevantEvery: core.LODDisabled`, `DormantEvery: core.LODDisabled` — для всех refactor'ed систем.

Семантически это становится «running interval» policy. Возможно в будущем переименуем `LODPolicy → RunPolicy`, но пока не трогаем, чтобы не менять API сигнатур половины кодовой базы.

**P2. Hash-bucket time-slicing helper — в `core/time_slicing.go`.**

```go
// ShouldProcessBucket возвращает true если entity попадает в bucket текущего
// тика. Используется внутри parallel-for'а: каждая горутина пропускает
// entity не из своего bucket'а.
//
// bucketCount = 1 ⇒ всегда true (entity всегда обрабатывается). Дефолт для
// Phase 11.5 — bucketCount=1 на всех системах (стартовый no-op). Phase 14
// поднимет до 60+ для vision / AI.
func ShouldProcessBucket(entityID uint32, bucketCount uint32, frameIdx uint32) bool {
    if bucketCount <= 1 {
        return true
    }
    return entityID%bucketCount == frameIdx%bucketCount
}
```

`frameIdx` — global tick counter в `core.App`. Инкремент в `Tick`. Доступно через `app.FrameIndex()` для systems.

Per-system `BucketCount` хранится как поле системы, default = 1, public для настройки из main.go / debug-tooling.

**P3. Worker pool в `core/worker_pool.go`. `runtime.NumCPU()` default + CLI flag override + hot-tune API заглушкой.**

```go
type WorkerPool struct {
    workers int
    jobs    chan workerJob
    quit    chan struct{}
    wg      sync.WaitGroup
}

type workerJob struct {
    fn   func(start, end int)
    done chan struct{}
}

func NewWorkerPool(workers int) *WorkerPool

// ParallelFor разбивает [0, total) на ровные chunk'и (по workers горутин),
// блокирует пока все не закончат. fn получает [start, end) диапазон —
// итерирует тот сам через своё условие.
func (p *WorkerPool) ParallelFor(total int, fn func(start, end int))

// Resize изменяет число воркеров. ЗАГЛУШКА для Phase 11.5 — просто
// логирует "Resize requested". Реальная реализация — Phase 21+ (UI settings).
func (p *WorkerPool) Resize(n int)

// Stop корректно останавливает all worker'ов на shutdown'е.
func (p *WorkerPool) Stop()
```

В `main.go`:
```go
var workersFlag = flag.Int("workers", 0, "worker pool size (default = NumCPU)")
flag.Parse()
n := *workersFlag
if n <= 0 {
    n = runtime.NumCPU()
}
pool := core.NewWorkerPool(n)
defer pool.Stop()
```

Pool привязывается к app через `app.Workers = pool` или передаётся в системы explicit'но через field. Я склоняюсь к **передавать explicit'но в конструкторе системы** (`NewUnitMovementSystem(pool)`) — это явнее и тестируемее, чем lookup через app.

**Important constraints.** Все raylib calls — main thread only. Внутри `ParallelFor.fn` нельзя делать DrawMesh / LoadTexture / UploadMesh / etc. Это policy, не enforced — комментарии в worker_pool.go проговаривают.

**P4. Per-system parallelization policy.**

| System | Strategy | Notes |
|---|---|---|
| `UnitMovementSystem` | snapshot serial → parallel step → no post-pass | Snapshot = neighbours list (XZ positions of all units). Parallel main = each worker steps own range of units. No archetype mutations. |
| `VisionSystem` | snapshot serial → parallel per-seer → no post-pass | Snapshot = unit positions by chunk + walls by chunk. Parallel main = each seer's vision query. Awareness writes own. |
| `FormationSystem` | snapshot serial → parallel per-squad → serial post-pass | Snapshot = squad centers. Parallel main = each squad writes ActionQueue. Serial post = apply leaveBuffer (cohesion removed in Phase 11.5, see P9, but pattern stays). |
| `SquadMacroPathSystem` | snapshot serial → parallel per-squad → no post-pass | Snapshot = squad centers + current orders. Parallel main = independent A* per squad. MacroPath write own. |
| `OrderResolverSystem` | serial | Archetype mutations (Completed orders removed, head advance). Parallel split не даёт выигрыша. |
| `LODSystem` | serial | Now render/streaming-only. См. P5. |
| Все остальные (TerrainStreaming/Gen/Load/Mesh, BuildingSystem, etc.) | unchanged | Можем параллелить per-chunk в будущих фазах когда станет узким местом. |

**Чтобы избежать data race'ов в parallel main:**
- ActionQueue писать только через `ClearActions` / `PushAction` — это операции over local component pointer, никаких shared map'ов.
- `posMap.Get(e)` — concurrent read safe (Ark archetype storage immutable между tick'ами для уже-добавленных entity).
- Никаких modify-shared-resource в parallel-pass'е. RoadGraph / TransitionRegistry / etc. — read-only в гонке.

**P5. LODSystem становится render/streaming-only.**

После refactor'а LOD-tier маркеры **не читаются** ни одной симуляционной системой. Они остаются нужны:
- TerrainStreamingSystem (chunk lifecycle).
- Render culling (можно скипать DrawMesh для дальних entity — Phase 21+ render-LOD).
- Map marker дисплея — _не_ нужно через LOD-маркеры, см. P7.

LODSystem продолжает обновлять маркеры, но **только chunks + Mobile dummies** (entity'и которые ему всё ещё нужны). Юниты НЕ получают LODActive/LODRelevant/LODDormant маркеров после Phase 11.5 — фильтр LODSystem'а исключает `Unit`. Это уменьшает архетипное thrashing и упрощает posFilter.

Альтернатива: оставить LOD-маркеры на units «на всякий случай для будущего render-LOD». Я **против** — текущий LOD имеет binary on/off, не render-LOD (он не уменьшает детализацию, он замораживает). Будущий render-LOD должен иметь свою policy (frustum + distance), не основанную на текущей системе. Так что чистим сейчас.

**P6. Удаляем initial `lodRelevantMap.Add(ent, ...)` для юнитов в main.go.**

Юниты больше не имеют LOD-маркеров. Spawn без `LODRelevant`:
```go
// Старое:
lodRelevantMap.Add(ent, &components.LODRelevant{})
// Новое: ничего, юнит просто existsет без LOD-маркера
```

Renderер юнитов читает `Filter[Unit + WorldPos + Stance]` независимо от LOD — рисует всех всегда. Если позже нужен render-LOD (далекий юнит превращается в спрайт), добавляется отдельный маркер `RenderLOD{Level}` под другую policy.

**P7. `MapMarkerCacheSystem` @ 250 мс — отдельная система для плавных squad-меток на карте.**

```go
type MapMarkerCache struct {
    Position map[ecs.Entity]components.WorldPos  // squad → центр
    // Phase 12+ добавит CommanderRole, SquadColor, OrderKind для лучших меток.
}

type MapMarkerCacheSystem struct {
    cache         *MapMarkerCache  // через ecs.Resource[MapMarkerCache]
    squadFilter   *ecs.Filter2[components.Squad, components.CommandRoster]
    posMap        *ecs.Map[components.WorldPos]
}

func (MapMarkerCacheSystem) LODPolicy() core.LODPolicy {
    return core.LODPolicy{
        ActiveEvery:   250 * time.Millisecond,
        RelevantEvery: core.LODDisabled,
        DormantEvery:  core.LODDisabled,
    }
}
```

Каждые 250 мс пробегает по всем Squad'ам, считает `SquadCenter` (берётся из существующего `SquadCenter` helper'а с world.Alive проверкой), пишет в `MapMarkerCache.Position[squad]`. Map renderer читает cache, _интерполирует_ от previous-frame position к current cache position c factor 0.18 (как уже сделано в Phase 11 M11.7) — это даёт плавность даже на 250 мс intervals.

LOD-tier'а у squad'ов больше нет (squad-entity не имеет WorldPos, и LODSystem её не видит). Так что MapMarkerCacheSystem обрабатывает всех squad'ов без фильтрации. Time-sliced пока не нужен (на 100 squad'ов это копейки).

**P8. Cohesion fix: leash 4 → 8, auto-ejection отключаем до Phase 15.**

В `systems/formation.go`:
- `CohesionLeashCoeff` меняется с 4.0 на 8.0 (опция (a) из обсуждения). Для Line Spacing=2 м это leash = 16 м вместо 8 м.
- **Auto-ejection (squadService.Leave(member)) отключаем условным флагом** `cohesionEjectionEnabled = false` для Phase 11.5. Stragglers всё ещё детектируются (для будущей Tactical AI), но не выкидываются из squad'а. Stragger'у пишется normal MoveTo к его formation offset — он будет пытаться догнать squad.

Phase 15 (Tactical AI) включит ejection обратно как часть Doctrine logic (например, Patrol doctrine = tight cohesion, Assault = loose). Сейчас просто turn off feature.

Закрывает ISSUES #5: единственный причина «stuck» юнитов уходит.

**P9. PHASE-11.5 не добавляет hash-bucket активного use'а — bucketCount=1 везде.**

Это значит каждая parallel-aware система _всё ещё_ пробегает все entity каждый Update. Time-slicing хеlper зарегистрирован, но `BucketCount = 1` фактически делает его no-op. Это **сознательно**: Phase 11.5 даёт инфраструктуру, Phase 14 Combat активирует (когда vision raycast'ы станут реально дорогими).

Точно так же `ParallelFor` на 12 юнитах ничего не выиграет (overhead больше gain'а). На сцене Phase 11 после refactor'а перфоманс может немного **просесть** (~0.1 ms на overhead worker pool'а). На 1000 юнитах будет 4× ускорение. Это правильный trade-off.

**P10. App.FrameIndex() добавляется как глобальный счётчик.**

```go
type App struct {
    // existing fields
    frameIndex uint32
}
func (app *App) Tick(delta time.Duration) {
    app.frameIndex++
    // existing
}
func (app *App) FrameIndex() uint32 { return app.frameIndex }
```

Передаётся в системы через `UpdateContext.FrameIndex` (нужно добавить поле в UpdateContext). Используется в `ShouldProcessBucket`.

---

## Шесть мильстоунов

### M11.5.1 — `core.WorkerPool` + CLI flag + main.go wire-up

**Цель.** Существует worker pool из `runtime.NumCPU()` горутин (или `-workers=N` flag override). `ParallelFor(total, fn)` работает корректно (split-execute-wait). Pool корректно останавливается на shutdown'е. Ни одна система ещё не использует pool — это инфраструктура.

**Делаем:**
- `core/worker_pool.go`: `WorkerPool` struct, `NewWorkerPool(n)`, `ParallelFor(total, fn)`, `Resize(n)` заглушка с log'ом, `Stop()`.
- `main.go`: `flag.Int("workers", 0, ...)` парсинг, instantiate pool, defer Stop.
- Unit-test pool на чисто-go тесте (можно как `core/worker_pool_test.go`): запустить `ParallelFor` на 1000 элементов с 4 worker'ами, проверить что все обработаны ровно один раз.

**Проверяем.** Pool с N=4 + ParallelFor(1000, ...) обрабатывает каждый элемент ровно раз (через atomic counter в test'е). `pool.Stop()` не вешает основной thread.

### M11.5.2 — `core.ShouldProcessBucket` + `App.FrameIndex` + `UpdateContext.FrameIndex`

**Цель.** Hash-bucket helper доступен. `app.FrameIndex()` инкрементируется каждый Tick. `UpdateContext` передаёт frameIndex в системы.

**Делаем:**
- `core/time_slicing.go`: `ShouldProcessBucket(entityID, bucketCount, frameIdx uint32) bool`.
- `core/app.go`: добавить `frameIndex uint32` поле, метод `FrameIndex()`, инкремент в `Tick`. Добавить `FrameIndex uint32` в `UpdateContext`.
- Все системы (`InitUI` / `Update`) получают сигнатурное расширение `UpdateContext` — компилятор укажет где использовать.

**Проверяем.** Bucket=1 всегда true. Bucket=10 — для entity.ID=5 true каждый 10-й кадр. Unit-test есть.

### M11.5.3 — Drop LOD-tier-gating: UnitMovement / Formation / SquadMacroPath / Vision / OrderResolver

**Цель.** Перечисленные системы более не имеют tier-branching. Один Filter, один цикл per Update. ISSUES #4 и #6 автоматически закрыты. Текущая test scene (12 юнитов) работает без visible regressions.

**Делаем:**
- `systems/unit_movement.go`: удалить `activeFilter` / `relevantFilter` / `dormantFilter`, оставить один `unitFilter` = `Filter5[Unit, WorldPos, Motion, ActionQueue, Stance]`. LODPolicy: `ActiveEvery: 0, RelevantEvery: LODDisabled, DormantEvery: LODDisabled`. Update — один проход.
- `systems/vision.go`: то же — единый `unitFilter`. LODPolicy → `ActiveEvery: 500 * time.Millisecond` (старый Active interval).
- `systems/formation.go`: единый filter без commander LOD проверки. LODPolicy → `ActiveEvery: 100 * time.Millisecond`.
- `systems/squad_macro_path.go`: единый filter, без commander LOD проверки. LODPolicy → `ActiveEvery: 1 * time.Second`.
- `systems/order_resolver.go`: уже serial; LODPolicy → `ActiveEvery: 0` (каждый тик).

**Проверяем.** Test scene запускается, 3 squad'а идут на дальние таргеты, anchor увозим за 300 м — все продолжают двигаться плавно. Profile snapshot: tick ms примерно равно prior baseline (на 12 юнитах разница неуловимая).

### M11.5.4 — Удалить LOD-маркеры с юнитов; LODSystem skipает Unit

**Цель.** Юниты больше не имеют LODActive/LODRelevant/LODDormant маркеров. LODSystem продолжает обрабатывать chunks и mobile dummies (если они ещё есть в test scene), но не юнитов.

**Делаем:**
- `main.go` unit spawn: удалить `lodRelevantMap.Add(ent, &components.LODRelevant{})` для каждого юнита.
- `systems/game_systems.go::LODSystem.InitUI`: добавить `ecs.C[components.Unit]()` в `Without(...)` для posFilter.
- Render filter'ы в main.go: `unitRenderFilter` уже не имеет LOD constraint (Filter3[WorldPos, Unit, Stance] — корректно). 3D рендер всех юнитов как и раньше.

**Проверяем.** `Ctrl+P` snapshot: LODActive/LODRelevant/LODDormant counts больше не включают units. Test scene выглядит идентично.

### M11.5.5 — Parallelize hot-path systems (snapshot-step-apply)

**Цель.** UnitMovementSystem, VisionSystem, FormationSystem, SquadMacroPathSystem используют WorkerPool через `ParallelFor`. На текущей сцене (12 юнитов) overhead pool'а виден в profile, но не блокирует — на больших сценах ускорение будет 3-4×.

**Делаем:**
- Каждая из 4 систем: рефакторим Update в **snapshot + parallel + apply**.
  - UnitMovement: snapshot neighbours (как сейчас, serial), затем `pool.ParallelFor(len(units), func(start, end) { for i := start; i < end; i++ { step(units[i], ...) } })`. Никаких mutations beyond own pos/motion.
  - Vision: snapshot units + walls by chunk. `pool.ParallelFor(len(seers), ...)`. Awareness write own.
  - Formation: snapshot squad centers + macro targets. `pool.ParallelFor(len(squads), ...)`. Per-squad pass пишет ActionQueue членов (write own — каждый member принадлежит одному squad, write disjoint).
  - SquadMacroPath: `pool.ParallelFor(len(squads), ...)`. A* независим per squad.
- Конструкторы систем принимают `*core.WorkerPool` (через `NewUnitMovementSystem(pool)` etc.).
- main.go: передаёт pool каждой системе при construct.

**Race-detector test:** `go run -race . -workers=4`, погонять test scene 30 сек, не должно быть race report'ов.

**Проверяем.** На 12 юнитах frame ms +0.1-0.3 ms overhead. Race detector чист. Симуляция корректна (юниты движутся как раньше).

### M11.5.6 — `MapMarkerCacheSystem` + Cohesion fix + GAMEDESIGN refresh ref + closure

**Цель.** Map marker'и плавно интерполируются через cache. Cohesion fix (leash 8, ejection off). Cleanup + documentation.

**Делаем:**
- `systems/map_marker_cache.go`: `MapMarkerCache` resource + `MapMarkerCacheSystem` @ 250 ms.
- `main.go`: `ecs.AddResource(app.World, &mapMarkerCache)` + system register.
- `ui/map_render.go`: drawSquadMarkers читает из `MapMarkerCache.Position` вместо on-the-fly `SquadCenter`. Lerp factor остаётся 0.18.
- `systems/formation.go`: `CohesionLeashCoeff = 8.0`. Auto-ejection branch (`leaveBuffer = append(leaveBuffer, mem)`) обёрнута в `if cohesionEjectionEnabled` constant, который ставится `false`. Stragglers всё ещё «детектируются» (поле в blackboard'е или counter — для будущей TAI), но не leave.
- ISSUES.md: пометить #4 / #5 / #6 как «closed in Phase 11.5», переместить в history. #2 (Tab лаг) остаётся.

**Проверяем.** Финальный sanity-pass: 3 squad'а с приказами на дальние таргеты, anchor двигается через карту — все идут без freeze. Cohesion fix: маркер-узкая-формация (Spacing 2 м) при манёвре через дерево не теряет членов. Map marker'и плавные.

---

## Что считаем «закрытием Phase 11.5»

- `core.WorkerPool` существует, размер = `runtime.NumCPU()` дефолт / `-workers=N` override / `Resize()` заглушкой.
- `core.ShouldProcessBucket` helper доступен; `App.FrameIndex` инкрементируется per tick; `UpdateContext.FrameIndex` передаётся системам.
- LOD-tier-gating убран из UnitMovement / Vision / Formation / SquadMacroPath / OrderResolver. Одна Update-ветка, один Filter per system.
- LOD-маркеры удалены с юнитов; LODSystem skipает Unit-архетип; LOD остаётся для chunks / dummies.
- UnitMovement / Vision / Formation / SquadMacroPath параллелизованы через WorkerPool (snapshot-step-apply pattern). Race-detector чист.
- MapMarkerCacheSystem @ 250 мс. Map renderer читает кэш с интерполяцией.
- Cohesion: leash 8.0, auto-ejection off (вернётся в Phase 15 как часть doctrines).
- ISSUES #4, #5, #6 — closed. #2 остаётся.
- GAMEDESIGN §1 принципы 6, 7, 8 актуальны (universal sim, time-slicing, worker pool) — уже сделано в этой сессии.
- ROADMAP order: 11.5 → 12 — актуально.

Pipeline: `... → ground_stick → unit_movement → vision → order_resolver → squad_macro_path → formation → map_marker_cache → lod (chunks-only) → ...`.

После этого — Phase 11.5 → ✅, переход к Phase 12 (Unit roles).

---

## Заметки на полях

- **Phase 11.5 — invisible to player.** Никаких новых features. Юниты двигаются как раньше. Map выглядит как раньше. Цель — здоровая архитектура для следующих 5+ фаз. Хочется иметь это как **закрытое** к моменту начала Phase 14 Combat — combat без worker pool'а на 200+ юнитах не побежит.

- **ParallelFor overhead на маленьких N.** На 12 юнитах ParallelFor с 4 worker'ами добавляет ~50-100 μs overhead (синхронизация + channel'ы). Это видно в profile, но абсолютно не блокирует. На 1000 юнитах overhead тот же, gain 4×. Это правильный trade-off — мы не оптимизируем под текущий тест-scene, мы готовим к целевому масштабу.

- **Race condition detection.** Запускать `go run -race .` периодически в течение Phase 11.5 development'а. Особенно после M11.5.5. Race-detector ловит большинство concurrent write'ов, но не все edge case'ы (например, false-sharing cache-line'ов). Реальный профилирующий test — Phase 14 / 21 когда сцена станет тяжёлой.

- **Determinism — закладываем не сейчас.** Per discussion: replay / multiplayer не нужны на данном этапе. Если когда-нибудь — добавится через sorted-order pre-pass в каждой parallel-aware системе (sort entity по ID, разбить в фиксированные ranges, parallel-for — гарантирует тот же порядок применения). Структурная правка, не архитектурная.

- **TerrainStreamingSystem / BuildingSystem / TerrainGen / TerrainMesh — пока serial.** Они вызываются on-demand (chunk lifecycle), не на каждом тике. Параллелить можно (per-chunk independent), но gain виден только при стриминге множества чанков одновременно — это case ещё не наступил. Phase 24 (Content pipeline) — естественное место добавить параллелизацию когда большие миры начнут стримиться.

- **OrderResolverSystem остаётся serial.** Архетипные мутации (Completed → cleanup) требуют serial apply. Number of orders в системе мал (≤ count of squads × queue length ≈ 50 max). Стоимость ничтожна, параллелить не имеет смысла.

- **GroundStickSystem — оставляем serial.** Это тонкая операция (floor-aware Y-resolve), 12 юнитов это микросекунды. Phase 14 когда юнитов станет много — пара-options:
  - Parallel-for over units (independent — каждый юнит читает свою floor coverage). 
  - Или вообще убрать (если у нас Phase 16.5 aviation, где Y free, GroundStick становится opt-in per `OnGround` marker).
  Решим позже.

- **App.frameIndex и TimeScale.** frameIndex инкрементируется в Tick **до** scaled-delta. Это значит paused игра (TimeScale=0) **тикает frameIndex** — render-loop работает, frame counter растёт. Хорошо для UI animation'ов / debug overlay'ев. Если когда-нибудь понадобится sim-frame counter (для seed-based RNG), добавим отдельный `simFrame` который останавливается при pause.

- **Что произойдёт если `pool` = nil в системе?** Системы должны иметь fallback: если pool nil, выполнять serial (просто `for i := 0; i < total; i++ { fn(i, i+1) }`). Это полезно для unit-test'ов где pool не нужен.

- **`ShouldProcessBucket` на bucketCount=1 — no-op.** Стартовая конфигурация. Phase 14 поднимет vision bucketCount до 60+, tactical AI до 300+. Не в Phase 11.5 — здесь только chassis.

- **MapMarkerCache vs SquadCenter on-the-fly.** Сейчас map renderer (Phase 11) вызывает `SquadCenter` напрямую в render loop. Это работает для 3 squad'ов, но на 100+ squad'ах будет дорогостоящим (особенно с `world.Alive` checks per member). MapMarkerCache @ 250 мс делает эту работу 4 раз/сек вне render loop'а, render просто читает map. На 100 squad'ах = 400 SquadCenter calls/sec вместо 100×60=6000. Дешевле.

- **Cohesion auto-ejection turn-off vs uninstall.** Я **оставляю** auto-ejection code, просто gate'ю флагом `cohesionEjectionEnabled = false`. Phase 15 (Tactical AI) добавит per-doctrine override (Patrol = true с tight leash, Assault = false), без перетряхивания формаций. Если бы я код удалил — Phase 15 пришлось бы его восстанавливать с нуля. С флагом — поворачивается обратно одной строкой.

- **`leaveBuffer` всё ещё нужен.** Хоть auto-ejection и off, мы оставляем сам buffer-pattern в FormationSystem на будущее. Это паттерн для архетипных мутаций после parallel-pass'а. В Phase 11.5 buffer всегда пустой, но синтаксически система готова.

- **Worker pool — singleton или per-app?** Я делаю singleton-style: один pool на app, передаётся в системы при construct. Это упрощает lifecycle (один Stop в shutdown'е), и Goroutine count бьёт в `runtime.NumCPU()`. Альтернатива — pool на системы (каждая система имеет свой pool) — даёт изоляцию, но больше горутин. Не нужно сейчас, может стать нужным если разные системы захотят разные размеры pool'ов.

- **Что если будущая Phase захочет async I/O?** Например, parallel terrain bake с диска. Worker pool подходит для CPU-bound, не для blocking I/O. Решение Phase Content pipeline: либо отдельный I/O pool, либо `runtime.GOMAXPROCS` increases temporarily. Не сейчас.

- **Phase 12 (Roles) НЕ зависит от Phase 11.5.** Можно идти параллельно если есть силы. Но архитектурно «Roles → Combat» лучше идёт после «Architectural refactor → Roles → Combat» — Combat будет реально тяжёлый без worker pool'а.
