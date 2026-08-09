# Phase 14.5 — рабочий план

Architectural & polish-after-Combat фаза. Два больших архитектурных дельты — **Spec table pattern** (collapse scattered enum-driven behavior на typed central registries с compile-time exhaustiveness check) и **Spatial hash infrastructure** (generic radius/AABB query resource, реальный reader в UnitMovement separation + WeaponSystem ray-vs-units + propagateSuppression + VisionSystem candidate filter). Плюс **Particle system** (ECS-entity-based, replaces транзитный `components.VisualEvents` ресурс) с smoke / dust / muzzle-flash / debris и **splash damage** для RPG7/GP25. Плюс **Weapon-bar UX bundle** — Sea Power-style Inspector панель + aim-mode cursor state + `OrderParamWeaponPref` reader (Phase 13 scaffold наконец-то становится живым). Плюс **Garrison routing fix** (Issue #7 — squad не входит нормально через двери) и **Phase 14 hotfixes** (Issues #9 AttackTarget vs HoldFire override, #10 out-of-range AttackTarget Fail).

**Что в Phase 14.5 сознательно НЕТ.** Grenades / arc-fired projectiles (физика подствольника, ballistic drop) — Phase 15 / 24, противоречит Phase 14 hybrid-bullet decision. Map ping system + event log panel — Phase 21 (отдельная UI-подсистема со своими entity'ями). Windows as breaching entry points — `OUT OF SCOPE` per user decision (требует broken-glass visualisation + route preview + breach animation/state — это полноценная фаза assault-gameplay, не polish). Spatial hash для vehicles (multi-resolution per-archetype) — Phase 16 когда appears реальная боль с heterogeneous query patterns. Reactive infantry behavior on Suppression (Phase 15 SurvivalInstinct). Armor / penetration / cover-shadow / cover-damage-reduction (Phase 24). Reloading animations (Phase 25). Wounded state / corpses (Phase 15/25). Friendly fire UI warning (Phase 21).

**Why now.** Phase 14 показал три boли: (a) enum-driven behavior разъезжается по 5+ местам, новый kind часто не интегрируется во все UI surfaces (AttackTarget забыл override HoldFire, не получил cursor change и т.п.); (b) `propagateSuppression` walk'ает всех units O(N) на каждый impact, нет shared spatial index'а; (c) `VisualEvents` resource — placeholder, не масштабируется на тысячи particles. Plus Garrison entry — Phase 5 load-bearing feature который не работает надёжно (Issue #7). Все четыре — architectural debt который дешевле закрыть до Phase 15 / 16, потому что они принесут больше readers за каждым подсистемой.

ROADMAP — высокоуровневый трекер. COMMAND-MODEL.md §5 (per-weapon control) — спецификация для weapon-bar bundle. CLAUDE.md fills patterns после милстоунов. Этот файл — рабочий план фазы.

---

## Решения, которые лочим до начала кода

**P1. Spec table pattern — single source of truth для enum-driven behavior.**

Каждый enum с side-effect в 2+ местах кода получает типизованный `XxxSpec` struct и `[Count]XxxSpec` array indexed by enum-code. Все читатели берут поля из spec'а, не switch'ат сами. Compile-time exhaustiveness через array-length trick:

```go
// components/order_spec.go (new)
type OrderKindSpec struct {
    Code              OrderKindCode
    Name              string  // "Move", "Garrison", "Attack", "Suppress"
    InPieMenu         bool
    NeedsEntity       bool    // hit-test requires entity target
    NeedsTerrain      bool    // hit-test accepts terrain Pos
    OverridesHoldFire bool    // AttackTarget/SuppressFire = true
    DrivesMacroPath   bool    // squad moves toward target (false = hold-in-place)
    Completion        CompletionRuleKind  // ArrivalRadius/TargetDeath/Timer/Never/EveryMemberOnFloor
    MapIconGlyph      rune
}

const OrderKindCount OrderKindCode = OrderKindSuppressFire + 1

var OrderKindSpecs = [OrderKindCount]OrderKindSpec{
    OrderKindMoveTo:         {...},
    // ...
}

// Compile-time exhaustiveness: добавил OrderKind в enum, забыл OrderKindSpec → не компилится.
var _ [OrderKindCount]OrderKindSpec = OrderKindSpecs
```

Pattern применяется в M14.5.0 (OrderKind first) и M14.5.1 (Stance, WeaponKind, EngagementMode — следующая волна).

**P2. `exhaustive` linter в pipeline.**

Адд `golangci-lint` (если ещё нет) + enable `exhaustive` check. Он ругается на switch'и по enum'у без всех case'ов И без default. Tag'и для opt-out где default достаточен (legitimate fall-through). Это **Go-аналог Rust exhaustive match** на linter-level — поломки видим в CI / pre-commit, не в playtest.

Установка минимальная: `~/.golangci.yml` (или `.golangci.yml` в репо) с `linters: enable: [exhaustive]`. M14.5.0 включает + чинит существующие violation'ы.

**P3. Spec migration scope в 14.5 — три enum'а.**

- **OrderKind** (M14.5.0) — самый болезненный, первым.
- **Stance** (M14.5.1) — 4 двойника таблиц (`unitMaxSpeed` / `unitStanceHeight` / `stanceDamageMul` / `weaponTargetY`) → один `StanceSpec`.
- **WeaponKind** (M14.5.1) — `primaryStats` + `tracerColorFor` + Phase 14.5 splash-radius — естественно собирается в `WeaponSpec`.

EngagementMode, PathStyle, Pace — пока **не трогаем**, switch'ей мало, боли мало. Применим pattern в следующих фазах по мере роста readers.

**P4. Spatial hash — одна generic implementation, один экземпляр на Units.**

```go
// core/spatial_hash.go (new)
const SpatialCellSize = 32.0  // metres — PHASE-14.5 P4

type spatialCell struct{ X, Z int32 }

type spatialEntry struct {
    Ent  ecs.Entity
    X, Z float32  // world-coord, inlined to avoid posMap.Get in query
}

type SpatialHash struct {
    cells    map[spatialCell][]spatialEntry
    cellSize float32
}

func NewSpatialHash(cellSize float32) *SpatialHash { ... }

// Rebuild — serial pass, reuses backing slices.
func (h *SpatialHash) Rebuild(snapshot []spatialEntry) { ... }

// ForEachInRadius — primary query API, zero-alloc callback.
func (h *SpatialHash) ForEachInRadius(x, z, r float32,
    fn func(ent ecs.Entity, distSq float32))

// QueryInto — out-param variant for callsites that want a slice.
func (h *SpatialHash) QueryInto(x, z, r float32, buf *[]ecs.Entity)
```

Один экземпляр live в main.go: `unitSpatialHash := core.NewSpatialHash(32.0)`, добавляется как resource через `ecs.AddResource(world, unitSpatialHash)`.

**Пример как добавить второй** (вписать в PHASE-14.5.md notes + CLAUDE.md): для Phase 16 vehicles достаточно `vehicleSpatialHash := core.NewSpatialHash(48.0)`, добавить второй `SpatialHashRebuildSystem` (или расширить существующий с per-filter snapshots), и vehicle-readers ходят за этим хешом. Generic-ность в API — на конкретные экземпляры разные cell-size'ы.

**P5. Rebuild — full per tick, serial, БЕФОРЕ UnitMovement.**

Snapshot всех Units (Filter1[Unit] + WorldPos) → `Rebuild(snapshot)`. Перед UnitMovement, чтобы separation steering мог использовать хеш в том же тике. Vision/Weapon/Suppression читают тот же хеш — 1-tick stale данные (юнит подвинулся max ~8 см за тик) acceptable для query radius 1.5 м+.

Incremental update (только moved entities) — **не делаем**: parallel-aware UnitMovement пишет позиции из workers, координация сложна, бенефит на 200 units мизерный.

**P6. Stale entity guard в reader'ах — обязательная инвариант.**

Между rebuild'ами entity может быть removed (DamageService.ApplyDeath). Hash вернёт его. **Все readers ОБЯЗАНЫ проверять `world.Alive(ent)` перед обращением.** Это документируется в:
- doc-comment на `ForEachInRadius` / `QueryInto`.
- CLAUDE.md section "SpatialHash invariants".
- Code-review checklist (memory entry).

Cheap; забыть = крэш. Этот invariant идёт в одну линию с уже-существующими Filter/Map gotchas.

**P7. Particle system — ECS-entity-based, replaces `VisualEvents`.**

```go
// components/particle.go (new)
type Particle struct{}  // marker

type ParticleKind uint8
const (
    ParticleTracer ParticleKind = iota
    ParticleImpact
    ParticleMuzzleFlash
    ParticleSmoke
    ParticleDust
    ParticleDebris
)

type ParticleVisual struct {
    Kind       ParticleKind
    Color      rl.Color
    Size       float32  // radius for sphere, length for tracer
    SpawnTime  float32
    TTL        float32
}

type ParticleVel struct {
    Vel rl.Vector3  // m/s; gravity applied per-tick for some kinds
}

type ParticleEnd struct {
    To rl.Vector3  // tracer end-point — line particles only
}
```

Particles spawn'ятся в WeaponSystem.serialApply (replaces VisualEvents.Append calls). Lifecycle owned by `ParticleSystem`:
1. **Update pass** (parallel-aware): advance Age, apply Vel*dt, apply gravity для тех kind'ов где нужно (smoke rises, debris falls). Drop expired (age > TTL) через per-worker buffers + serial post-pass RemoveEntity.
2. **Render pass** (в main 3D loop): walk Filter2[Particle, WorldPos] + Filter[ParticleVisual], render через DrawLine3D (tracer) / DrawSphere (impact, muzzle flash, dust) / DrawCube (debris). Smoke — TBD (sprite billboard? particle-mesh? — M14.5.4 decides).

`components.VisualEvents` resource удаляется (replaced by ECS entities). Существующие `drawTracers` / `drawImpacts` функции в render_world.go reworked под Particle filter.

**Particle cap**: TBD в M14.5.4. Soft cap ~2000 live particles (на пике firefight). Eviction по oldest-first если cap превышен — `ParticleSystem.update` сортирует by SpawnTime и evict'ит overflow. LOD: dormant chunk particles не update'ятся (выкидываются).

**P8. Splash damage — radius propagation, использует SpatialHash.**

RPG7 / GP25 ставят splash flag в `WeaponSpec`. resolveShot detects splash → impact event переключается на `splashEvent{pos, radius, falloff}` вместо single `damageEvent`. Serial post-pass walk'ает `unitSpatialHash.ForEachInRadius(pos, radius)` — каждый юнит в радиусе получает damage = `baseDamage * (1 - d/radius)^falloff`. Friendly fire включён (тот же design что direct shots).

Particle effects: splash spawn'ит N debris particles + smoke cloud (M14.5.5).

**P9. Garrison completion fix — новый CompletionRuleKind.**

```go
type CompletionRuleKind uint8
const (
    CompletionArrivalRadius CompletionRuleKind = iota  // MoveTo, Patrol, OccupyTrench (current default)
    CompletionTargetDeath                              // AttackTarget
    CompletionTimer                                    // SuppressFire
    CompletionNever                                    // DefendPosition
    CompletionEveryMemberOnFloor                       // Garrison (new)
)
```

`CompletionEveryMemberOnFloor`: walk squad's CommandRoster. Для каждого live member resolved `WorldPos` маппится в NavNode (через `FloorNavGrid` lookup на building'е). Если N/M members находятся на floor-cell внутри target building'а → completed. Phase 14.5 simple: `M = roster.Count` (все должны быть внутри). Phase 15 может ослабить (50%+).

Это лечит часть Issue #7 — squad больше не Completed когда commander заскочил, остальные снаружи. Но также требует investigation **почему остальные не входят** (см. M14.5.6 — может оказаться что NavService.FindPath не выдаёт floor-cell в качестве goal cell, и squad goal остаётся surface-cell перед дверью).

**P10. Weapon-bar UX bundle — Inspector section + aim mode + WeaponPref.**

**Inspector weapon-bar** — новая section в single-unit и single-squad views. Перечислены все живые Equipment.Primary + Secondary в selection. Группировка по WeaponKind (e.g. "AK47 × 4 ready, 1 reloading, 120 rds"). Per-weapon row: WeaponKind iconrow, Ammo counter, RoF cooldown indicator.

**Click на weapon row → aim mode**:
- Cursor меняется на crosshair-style.
- Pie menu suppressed (RMB-hold не открывает меню).
- Все обычные RMB-handler'ы routed через "aim-mode commit":
  - RMB на enemy unit → `IssueOrder(OrderKindAttackTarget, target, OrderParams{WeaponPref: weaponEnt})`.
  - RMB на terrain → `OrderKindSuppressFire` with weapon pref.
  - ESC → exit aim mode без issue.
- 3D cursor preview: тонкая линия от unit muzzle к hit point + small "selected weapon" badge у курсора.

**`OrderParamWeaponPref` reader** в WeaponSystem.shouldFire (после RoE gate):
- Если order has `OrderParamWeaponPref{Weapon ecs.Entity}` И юнит owns этот weapon (через `OwnedBy`) — пакет fire через preferred weapon.
- Fallback: Equipment.Active (which defaults to Primary).
- Если pref invalid (weapon мёртв, не у owner'а) — лог warning, fallback к Active.

Phase 14 scaffold `OrderParamWeaponPref` уже **существовать не должен** (я его не делал). Создаём в M14.5.7 как часть bundle'а.

**P11. Aim mode — modal input state в main.go.**

```go
var aimMode struct {
    Active     bool
    Weapon     ecs.Entity      // selected weapon entity
    OwnerHint  ecs.Entity      // for cursor-target preview
}
```

Капчуется в main input loop. Pre-empts (a) pieMenu.Begin (RMB skips pie path while aimMode.Active), (b) ESC handler exits. Не конфликтует с marquee/selection — LMB вне Inspector weapon-bar ничего не делает в aim mode.

**P12. ParticleSystem LOD policy — Active only.**

Particle update — потенциально expensive на массивных firefight'ах. Tier policy:
- Active: every tick (~16 ms cadence). Particles в зоне видимости — full fidelity.
- Relevant: every 250 ms. Particles за пределами 60м — slow update OK (visual fidelity слегка падает но cap pool spending lower).
- Dormant: disabled. Particle в dormant zone — despawn immediately (через ParticleSystem detection на ttl=0).

**P13. Compile-time check pattern — Go-pragmatic.**

Используем `var _ [Count]Spec = Specs` idiom для exhaustiveness — это Go-honest pattern, не "borrowing from Rust". Если масштаб разрастётся (10+ specs) — добавим code-gen helper `go run tools/specgen` который генерирует boilerplate. Сейчас (3 spec'а в 14.5) — overkill.

**P14. ISSUES.md hotfixes integrated в milestones.**

- Issue #9 (AttackTarget vs HoldFire override) — fixed via M14.5.0 (становится одной строкой `OrderKindSpecs[AttackTarget].OverridesHoldFire = true`).
- Issue #10 (out-of-range AttackTarget Fail) — fixed via M14.5.0 (новый CompletionRuleKind extension: per-Completion timeout-to-Fail при unmet condition N сек подряд).
- Issue #7 (Garrison routing) — M14.5.6 (отдельный investigation milestone).

---

## Девять мильстоунов

### M14.5.0 — Spec foundation: OrderKind + exhaustive linter + Phase 14 hotfixes (#9, #10)

**Цель.** Pattern «typed Spec table indexed by enum-code» live в коде на примере `OrderKindSpec`. Все 7+ мест чтения `OrderKindCode` (pie menu, map render, inspector chip, resolveTargetIntoOrder, checkCompletion, shouldFire, squad_macro_path short-circuit) переехали на чтение из `OrderKindSpecs`. Compile-time exhaustiveness check работает. `golangci-lint` с `exhaustive` enabled, баги в существующих switch'ах исправлены. Issues #9 и #10 закрыты как первый payoff Spec pattern'а.

**Делаем:**
- `components/order_spec.go` (новый): `OrderKindSpec` struct, `CompletionRuleKind` enum, `OrderKindSpecs` array, compile-time check.
- `OrderKindCount` const после последнего enum-value (`= OrderKindSuppressFire + 1`).
- `systems/order_resolver.go::checkCompletion` рефакторен под dispatch по `Spec.Completion` (один switch на CompletionRuleKind вместо OrderKindCode). Новый case `CompletionEveryMemberOnFloor` — заглушка, реализуется в M14.5.6.
- `systems/squad_macro_path.go::processSquad` short-circuit читает `Spec.DrivesMacroPath` (false → hold).
- `systems/weapon.go::shouldFire` — после Mode=HoldFire ветки проверка `if activeOrderSpec.OverridesHoldFire { allow = true }`. Закрывает #9.
- `command.go::resolveTargetIntoOrder` читает `Spec.NeedsEntity` / `NeedsTerrain` для kindOverride fallback'а.
- `ui/pie_menu.go::pieSegments` builds dynamically from `OrderKindSpecs` filter'а `InPieMenu == true`. `pieKindLabel` читает `Spec.Name`.
- `ui/map_render.go::drawOrderIcon` читает `Spec.MapIconGlyph` (новое поле). Дефолтный glyph для unknown / unspecified.
- `ui/inspector.go` queued-orders chip читает `Spec.Name`.
- `.golangci.yml` (новый или существующий): `linters: enable: [exhaustive]`. Baseline fix: пройти существующие switch'и, либо добавить case'ы, либо `//exhaustive:ignore-default-case-required` где legitimate.
- Issue #10 fix: extend `OrderKindSpec` поле `MaxOutOfRangeSeconds float32`. `checkCompletion` для AttackTarget: если target alive but distance(squadCenter, targetPos) > squad's weapon max range более N seconds → Fail. Default N = 8 s (стандартный «engagement timeout»). Сохраняется per-order через новый `OrderOutOfRangeSince float32` field или transient resource map.
- CLAUDE.md section "Spec table pattern" — документация паттерна для будущих фаз.

**Проверяем.** `go build` чист, `golangci-lint run` чист. Test scene: AT team (HoldFire default) получает AttackTarget на врага — открывает огонь немедленно (override живёт). MGTeam (FreeFire) на врага вне range > 8 sec — Order переходит в Failed, Inspector показывает "Failed: out of range". Добавление dummy `OrderKindFoo` в enum БЕЗ добавления в `OrderKindSpecs` → compile-error. Linter ругается если добавить switch без всех case'ов.

---

### M14.5.1 — Spec migration: StanceSpec + WeaponSpec

**Цель.** Pattern применён к двум следующим больным enum'ам. `Stance` collapses 4 таблицы-двойника в один `StanceSpec`. `WeaponKind` collapses `primaryStats` + `tracerColorFor` + splash флаг в `WeaponSpec`. Compile-time exhaustiveness работает для обоих.

**Делаем:**
- `components/stance_spec.go` (новый):
  ```go
  type StanceSpec struct {
      Code            StanceCode
      Name            string  // "Stand", "Crouch", "Prone"
      MaxSpeed        float32 // m/s
      BodyHeight      float32 // render cube height
      DamageMultiplier float32  // hit silhouette factor
      TargetCenterY   float32  // weapon aim point Y above foot
  }
  const StanceCount StanceCode = StanceProne + 1
  var StanceSpecs = [StanceCount]StanceSpec{ ... }
  ```
  Заменяет `unit_movement.go::unitMaxSpeed`, `render_world.go::unitStanceHeight`, `systems/weapon.go::stanceDamageMul`, `systems/weapon.go::weaponTargetY`.
- `components/weapon_spec.go` (новый):
  ```go
  type WeaponSpec struct {
      Kind        WeaponKind
      Name        string
      Ammo        uint16
      RangeM      float32
      RoF         float32
      Damage      uint16
      Dispersion  float32
      TracerColor rl.Color
      SplashRadius float32  // 0 = single-target; >0 = AoE
      SplashFalloff float32 // 1.0 = linear; 2.0 = quadratic
  }
  const WeaponKindCount WeaponKind = WeaponMakarov + 1
  var WeaponSpecs = [WeaponKindCount]WeaponSpec{ ... }
  ```
  Заменяет `systems/role_service.go::primaryStats`, `systems/weapon.go::tracerColorFor`.
- `systems/role_service.go::spawnPrimary` читает `WeaponSpecs[kind]` для всех полей.
- `systems/weapon.go` — `stanceDamageMul[code]` → `StanceSpecs[code].DamageMultiplier`. `weaponTargetY[code]` → `StanceSpecs[code].TargetCenterY`. `tracerColorFor(kind)` → `WeaponSpecs[kind].TracerColor`.
- `systems/unit_movement.go` — `unitMaxSpeed[code]` → `StanceSpecs[code].MaxSpeed`.
- `render_world.go::unitStanceHeight(code)` → `StanceSpecs[code].BodyHeight`.

**Проверяем.** Build чист. Test scene: визуально ничего не изменилось (numerical equivalence). Inspector single-unit Stance row показывает `Spec.Name` ("Stand" / "Crouch" / "Prone") — раньше через локальный `stanceLabel(code)` switch, теперь читаем spec. Добавление dummy `StanceFoo` в enum без spec → compile error.

---

### M14.5.2 — Spatial hash core + UnitMovement separation reader

**Цель.** `core.SpatialHash` resource existing в pipeline'е, rebuild'ится каждый тик BEFORE UnitMovement. UnitMovementSystem separation steering мигрирован на `ForEachInRadius` callback. Old `neighbourFilter` (Filter2[Unit, WorldPos]) либо удаляется, либо остаётся как fallback для тех readers которые не интегрированы.

**Делаем:**
- `core/spatial_hash.go` (новый): full implementation per P4 (NewSpatialHash, Rebuild, ForEachInRadius, QueryInto, ApproximateMemory helper для debug HUD).
- `systems/spatial_hash_rebuild.go` (новый): тонкий System, snapshot'ит Filter2[Unit, WorldPos] в reusable buffer, вызывает hash.Rebuild. Зарегистрирован между `unit_movement` и `vision` (но **before** `unit_movement`, чтобы separation сам в том же тике пользовался — re-проверка по плану P5). Wait — нужно положить ДО UnitMovement.
- main.go pipeline registration: `... → ground_stick → spatial_hash_rebuild → unit_movement → vision → weapon → ...`. SpatialHash живёт как `ecs.Resource[core.SpatialHash]`.
- `systems/unit_movement.go` — separation pass рефакторен: вместо walk'а `neighbourFilter` использует `hash.ForEachInRadius(unit.X, unit.Z, separationRadius, fn)`. Callback skip self, проверяет alive, applies separation force.
- Stale entity guard tests: `world.Alive(ent)` perm-check в callback документирован в `core/spatial_hash.go` doc-comment.
- CLAUDE.md section "SpatialHash invariants" — паттерн использования + invariants (rebuild lifecycle + alive-check rule).

**Проверяем.** `go build` + `go vet` чисты. `go test -race ./core/` + `go run -race . -workers=4` 30 sec тест — нет race detector hits. Performance: с 28 units separation pass должен быть ≤ старого варианта (overhead Rebuild ~5µs amortized). Снапшот `Ctrl+P`: `spatial_hash_rebuild` median ms < 0.1 ms. `unit_movement` median ms — не выше старого. Test scene: визуально squad behavior не изменился (формация держится, separation работает).

---

### M14.5.3 — Spatial hash readers: WeaponSystem + VisionSystem + propagateSuppression

**Цель.** Все три hot-path query'еры мигрированы на SpatialHash. Старые 3×3 chunk window loops удалены / упрощены. `propagateSuppression` больше не walk'ает всех units. Документация паттерна добавления второго хеша (для Phase 16 vehicles) — в CLAUDE.md + inline-comment в `core/spatial_hash.go`.

**Делаем:**
- `systems/weapon.go::resolveShot` — unit-vs-ray walk: вместо итерации `targetsByChunk` по 3×3 окну, используется `hash.ForEachInRadius(midpoint(muzzle, aim), rayLength/2 + maxTargetRadius, fn)`. Callback делает segment-vs-point hit-check. Старое `targetsByChunk` index удаляется.
- `systems/weapon.go::propagateSuppression` — `hash.ForEachInRadius(impact.X, impact.Z, suppressionRadius, fn)`. Удаляется per-shot O(N) walk через suppressionFilter. (suppressionFilter остаётся для decay pass — decay walks ВСЕХ units каждый тик, hash для этого не нужен.)
- `systems/vision.go::processVisionSeer` — candidate filter: вместо 3×3 chunk window используется `hash.ForEachInRadius(seer.X, seer.Z, vision.RangeM, fn)`. Tightens query — раньше окно было до 192 м, теперь точно по vision range. Wall walls раздел не меняется (walls — static, остаются `wallsByChunk` map'ом).
- `systems/weapon.go::targetsBuf` snapshot можно упростить — теперь WeaponSystem не строит targetsByChunk сам, использует общий SpatialHash. Per-shot нужно ещё доставать stance / faction по entity — это через map.Get в callback.
- Документация "Adding a second SpatialHash" в `core/spatial_hash.go`:
  ```
  // Phase 16+ pattern for vehicles:
  //   vehicleHash := core.NewSpatialHash(48.0)
  //   ecs.AddResource(world, vehicleHash)
  //   // ... in SpatialHashRebuildSystem, add a second Rebuild call snapshotting
  //   // Filter2[Vehicle, WorldPos] into vehicleHash.
  //   // Vehicle-readers query vehicleHash; unit-readers continue with unitHash.
  ```

**Проверяем.** Build + vet + race чисты. Test scene combat: 4-vs-8 firefight 60 sec, поведение визуально такое же (tracers / damage / suppression). Профилер snapshot после M14.5.3 — `weapon` system median должен **не вырасти** относительно M14.2 (на 28 units может быть чуть меньше из-за tighter query). `vision` system — то же. Race-test без hits. Регрессия check: friendly fire всё ещё работает (hash не должен фильтровать по faction, это reader-side).

---

### M14.5.4 — Particle system core: ECS-entity-based, replaces VisualEvents

**Цель.** Tracers + impacts больше не живут в `components.VisualEvents` slice — каждый particle = ECS-entity. `ParticleSystem` (parallel update + serial cleanup) живёт в pipeline. WeaponSystem.serialApply spawn'ит tracer/impact как entities. Render pass walks particle filter. `components.VisualEvents` resource удалён.

**Делаем:**
- `components/particle.go` (новый): `Particle` marker, `ParticleKind` enum (Tracer/Impact/MuzzleFlash/Smoke/Dust/Debris), `ParticleVisual{Kind, Color, Size, SpawnTime, TTL}`, `ParticleVel{Vel rl.Vector3}`, `ParticleEnd{To}` (line particles only).
- `systems/particle.go` (новый): `ParticleSystem` с `Filter3[Particle, WorldPos, ParticleVisual]`, Filter с/без ParticleVel. Update pass: для всех particles age += dt; для тех у кого Vel — pos += Vel*dt + gravity (selectively); collect expired (age > TTL) в per-worker buffers, serial post-pass RemoveEntity. Parallel-aware через WorkerPool.
- `systems/weapon.go::serialApply` — replaces `events.AppendTracer / AppendImpact` calls на `world.NewEntity()` + add Particle + WorldPos + ParticleVisual + (optionally) ParticleEnd. Spawn через handle map'ы (Set up in InitUI).
- `render_world.go::drawTracers` → `drawParticles(filter)` — walks particle filter, dispatch по ParticleVisual.Kind на DrawLine3D (Tracer) / DrawSphere (Impact, MuzzleFlash, Dust) / DrawCube (Debris). Smoke — billboard-sprite (simple textured quad facing camera) или fallback DrawSphere с low alpha.
- main.go: удаляем `visualEvents` resource + Decay call. Регистрация `particleSys` после `weapon`.
- Cap soft-limit: ParticleSystem отслеживает count, при превышении 2000 — sort by SpawnTime ascending, RemoveEntity oldest N.

**Проверяем.** Build + vet + race. Test scene combat: tracers визуально идентичны (или незначительно лучше — теперь particles могут lerp'аться красивее). Impact spheres появляются. Ammo entity count в `Ctrl+P` snapshot после firefight: видны 10-100 live particle entities. Particle pool после стихания firefight — отсасывается через TTL expiry. Performance: `particle` system median ms — < 0.3 ms на 28 units firefight.

---

### M14.5.5 — Particle expansion + splash damage (RPG7/GP25)

**Цель.** Smoke + dust + muzzle-flash + debris particle kinds spawn'ятся в visually-correct ситуациях. RPG7 и GP25 наносят splash damage с radius из WeaponSpec; particle visualisation matches (explosion-style debris + smoke cloud).

**Делаем:**
- `WeaponSpecs` — заполнить `SplashRadius` для RPG7 (3.5 м) и GP25 (2.5 м). Other weapons = 0 (single-target).
- `systems/weapon.go::resolveShot` — detect splash via `WeaponSpecs[kind].SplashRadius > 0`. Если splash:
  - Impact event тип меняется: вместо single damage event → `splashEvent{pos, radius, falloff, damage}`.
  - Tracer particle спавнится как раньше (muzzle → impact).
  - В serial post-pass `applySplashDamage(event)`: `unitSpatialHash.ForEachInRadius(pos, radius, fn)`. Callback computes damage = `baseDamage * pow(1 - dSq/radius², falloff)`; calls `damageService.Apply`.
  - Spawn 6-10 Debris particles + 1 Smoke cloud particle на impact point.
- Muzzle flash: каждый shot spawn'ит 1 MuzzleFlash particle на muzzle pos с TTL 0.08 sec, цвет от tracerColor.
- Dust: при impact в terrain (нет unit hit, нет wall block) — spawn 3-5 Dust particles с downward Vel + low gravity.
- Particle gravity table: Smoke → +Y 0.5 m/s (rises); Debris → -Y 9.8 m/s; Dust → -Y 1.0 m/s (slow settle); others = 0.

**Проверяем.** Test scene: дать AT team AttackTarget на врага в 80 м (с овверайдом HoldFire фикса). RPG7 shot — видна tracer, impact с smoke cloud + debris. Враги в 3-3.5 м от impact'а получают damage (HP падает у 2-3 юнитов одним RPG). MGTeam shot AK47 — muzzle flash, tracer, single impact, dust particles на terrain. Profiler particle count peaks ~150-200 во время firefight'а. `weapon` median ms — не выше +0.3 ms.

---

### M14.5.6 — Garrison routing fix (Issue #7)

**Цель.** Squad с Garrison order надёжно входит в здание через двери. Completion засчитывается только когда every roster member на floor-cell внутри target building'а. Investigation NavService через TransitionRegistry прошёл — bug root cause закрыт.

**Делаем:**
- **Investigation pass.** Repro Garrison несколько раз с debug overlay F (FloorNavGrid) + G (RoadGraph) + N (NavGrid). Логировать NavService.FindPath результат для Garrison — какие waypoints возвращаются? Доходят ли до floor-cells или останавливаются на surface-cell перед дверью?
- Hypotheses to verify (по приоритету):
  1. `NavService.FindPath` использует goal как surface NavNode, не находит floor-node. Fix: при Garrison goal = floor-NavNode (центр floor'а), не surface.
  2. `SquadMacroPathSystem` decimation шаг 6-8 м выкидывает door-transition waypoint. Fix: prefer-include transition cells в decimation (не выбрасывать waypoints у TransitionEdge endpoints).
  3. `FormationSystem` slot offsets разносят юнитов на разные стороны стен. Fix: внутри building'а formation collapses на single-file column (squad-aware contextual formation override).
- `OrderResolverSystem::checkCompletion` для `OrderKindGarrison` мигрирован на новый `CompletionEveryMemberOnFloor`:
  ```go
  case CompletionEveryMemberOnFloor:
      bld := buildingMap.Get(target.Entity)
      if bld == nil { return false }
      // Walk roster, check each member's pos is inside building Footprint AND
      // the member's grounded Y matches one of building's floors.
      inside := 0
      for i := 0; i < int(roster.Count); i++ {
          mem := roster.Members[i]
          if !world.Alive(mem) { continue }
          pos := posMap.Get(mem)
          if pointInFootprintAndOnFloor(pos, bld) { inside++ }
      }
      return inside == int(roster.Count)
  ```
- Fix appliedHere в `applyTemplateStandingRules` или `OrderKindSpecs[Garrison]` — `Completion: CompletionEveryMemberOnFloor`.
- **Note for player on partial entry**: если 3/4 inside, 1 стоит снаружи — Order остаётся InProgress, Inspector queued-orders chip показывает progress fraction. UI индикация — bonus, может в Phase 21.

**Проверяем.** Test scene: select squad, RMB на building с дверью → squad подходит, входит через дверь (commander first, остальные следом), все 4 юнита оказываются inside. Order переходит в Completed только когда последний внутри. До этого Inspector queued-orders shows "Garrison (3/4)".

**Risk:** Investigation может выявить более глубокую проблему в NavService / floor traversal которая не лечится быстрым fix'ом. В этом случае откатываемся к Phase 14 simple completion gate (center in AABB) + помечаем M14.5.6 как partial-fix, переносим оставшееся в Phase 15 / 16. Документируем findings в ISSUES.md.

---

### M14.5.7 — Weapon-bar Inspector + aim mode

**Цель.** Inspector single-unit и single-squad views содержат weapon-bar section с per-Equipment.Primary/Secondary rows. Клик по weapon row активирует aim mode (cursor changes, pie menu suppressed). RMB в aim mode выдаёт Order с `OrderParamWeaponPref` приклеенным. ESC выходит из aim mode.

**Делаем:**
- `ui/inspector.go::drawInspectorWeaponBar(ctx, units []ecs.Entity, x, y, width int32)` — новая section. Aggregates Equipment.Primary + Secondary across `units`. Группировка по WeaponKind (читает `WeaponSpecs[kind].Name`). Per-weapon row: weapon name + ammo (sum across units) + count rendered as chip (clickable).
- Inspector single-unit: weapon-bar показывает 2 строки (Primary + Secondary).
- Inspector single-squad: weapon-bar aggregated по всем members.
- Chip click handler — выставляет `aimMode.Active = true, Weapon = weaponEnt, OwnerHint = ownerEnt`. Returned через InspectorCtx или callback.
- `command.go::aimMode` — package-level struct (per P11).
- main.go input loop:
  - При aimMode.Active: pieMenu.Begin skip (RMB-press не открывает меню); cursor 2D-overlay рисует crosshair-style icon вместо default.
  - ESC pressed → exit aim mode.
  - RMB pressed → resolve через `resolveRMBOrderWithParams` с `OrderParams.WeaponPref: &components.OrderParamWeaponPref{Weapon: aimMode.Weapon}`. Затем `aimMode.Active = false`.
- `OrderParamWeaponPref` — новый компонент в `components/order_param_weapon.go`:
  ```go
  type OrderParamWeaponPref struct {
      Weapon ecs.Entity  // which weapon entity to fire
  }
  ```
- `SquadService.OrderParams.WeaponPref *components.OrderParamWeaponPref`, `IssueOrder` attaches to spawned order entity если non-nil.

**Проверяем.** Test scene: select squad, Inspector show weapon-bar. Click "AK47 ×3 ready 90 rds" chip → cursor changes to crosshair; pie menu RMB-hold НЕ открывает меню. RMB на enemy → AttackTarget order с WeaponPref attached (Inspector queued-orders shows "AK47" badge on order chip). ESC → cursor returns to normal, aim mode off.

---

### M14.5.8 — OrderParamWeaponPref reader + AttackMove/HoldFire UI indicator + closure

**Цель.** WeaponSystem.shouldFire (или snapshot pass) читает `OrderParamWeaponPref` и выбирает соответствующий weapon entity для shot resolution. Phase 13 scaffold `OrderParamWeaponPref` наконец-то имеет real reader. UI indicator для AttackMove+HoldFire conflict (memory TODO) — Inspector chip dimmed/struck-through когда squad's Mode HoldFire И active order has AttackMove. ROADMAP closure, PHASE-14.5.md → old/.

**Делаем:**
- `systems/weapon.go::Update` snapshot pass — после `pickTarget` и `shouldFire`, проверка active order's WeaponPref:
  ```go
  weaponEnt := eq.Active
  if head := sys.orderQueueMap.Get(squad); head != nil && head.First != (ecs.Entity{}) {
      if pref := sys.orderWeaponPrefMap.Get(head.First); pref != nil {
          if world.Alive(pref.Weapon) && belongs(pref.Weapon, shooter) {
              weaponEnt = pref.Weapon
          }
          // else: invalid pref, fallback to Active, log warning once per order.
      }
  }
  weapon := sys.weaponMap.Get(weaponEnt)
  // ... rest of snapshot uses this weapon
  ```
- `belongs(weaponEnt, owner)` helper — checks `OwnedBy.Owner == owner` AND `weaponEnt is eq.Primary OR eq.Secondary`.
- `ui/inspector.go` AttackMove chip rendering — добавить strike-through / dim-color treatment когда `engagement.Mode == HoldFire AND active order has OrderParamAttackMove`. Tooltip: "AttackMove ignored — squad's Rules of Engagement is HoldFire. Change Mode to Free/Return to enable opportunistic firing."
- Memory entry `project_attackmove_holdfire_ui.md` → marked closed (или removed).
- Balance pass: 4-vs-8 firefight, RPG splash, weapon-pref clicked routing — все feel correctly. Document baseline performance numbers в notes.
- ROADMAP.md: Phase 14.5 → ✅. Активная фаза → Phase 15 (Tactical AI).
- PHASE-14.5.md → `.claude/old/PHASE-14.5.md`.
- ISSUES.md: #7 (Garrison) closed if M14.5.6 succeeded; otherwise updated with partial-fix notes. #9, #10 closed.

**Проверяем.** Test scene playthrough: select squad с AT team, заклик AttackMove (Alt+RMB) на enemy в режиме HoldFire → Inspector chip отображается strike-through, squad молчит как и раньше. Smith Mode на FreeFire через chip — strike-through пропадает, squad открывает огонь. Click weapon-bar AK47 in selected squad → cursor changes → RMB на enemy → AttackTarget order specifically fires AK47 (Sniper в squad'е держит SVD молчанием, потому что WeaponPref указал AK47).

---

## Что считаем «закрытием Phase 14.5»

- **Spec table pattern** — `OrderKindSpec`, `StanceSpec`, `WeaponSpec` live в коде с compile-time exhaustiveness check. Documented в CLAUDE.md как канонический pattern для enum-driven behavior.
- **Exhaustive linter** в `.golangci.yml` enabled, существующие switch'и compliant.
- **Spatial hash** generic implementation в `core/spatial_hash.go`, один экземпляр для Units, 32m cells, integrated в UnitMovement separation + WeaponSystem ray-vs-units + propagateSuppression + VisionSystem candidate filter. CLAUDE.md документирует invariants (rebuild lifecycle, alive-check rule) + pattern для добавления второго хеша.
- **Particle system** ECS-entity-based — `Particle / ParticleVisual / ParticleVel / ParticleEnd` компоненты, `ParticleSystem` Update + serial cleanup, replaces `components.VisualEvents`. 6 ParticleKind значений (Tracer, Impact, MuzzleFlash, Smoke, Dust, Debris).
- **Splash damage** для RPG7 (radius 3.5 m) / GP25 (radius 2.5 m). Linear / quadratic falloff через WeaponSpec.
- **Garrison routing fix** (Issue #7) — completion gate `CompletionEveryMemberOnFloor`, NavService routing investigated and fixed (или partial fix documented в ISSUES.md).
- **Phase 14 hotfixes** — Issue #9 (AttackTarget overrides HoldFire) closed, Issue #10 (AttackTarget Fail on out-of-range) closed.
- **Weapon-bar UX bundle** — Inspector weapon-bar section, aim-mode cursor state, `OrderParamWeaponPref` reader в WeaponSystem.
- **AttackMove + HoldFire UI indicator** — Inspector chip strike-through когда конфликт.
- Pipeline добавляются 2 system'а: `spatial_hash_rebuild` (before unit_movement), `particle` (after weapon). Final order: `... → ground_stick → spatial_hash_rebuild → unit_movement → vision → weapon → particle → threat_decay → order_resolver → ...`.

После этого — ROADMAP update, Phase 14.5 → ✅, **переход к Phase 15 (Tactical AI)** или Phase 16 (Vehicles) в зависимости от приоритетов.

---

## Заметки на полях

- **Spec table sparse fields.** Некоторые поля `OrderKindSpec` будут пустыми для большинства строк (e.g. `MapIconGlyph` только для kinds которые карту рисуют). Это OK pattern; альтернатива (sub-spec extensions) добавляет complexity без значимого payoff на 6 kinds. Если spec разрастётся до 10+ полей с дырками — refactor на per-aspect spec'ы (OrderKindUISpec + OrderKindBehaviorSpec).

- **Exhaustive linter false positives.** Некоторые switch'и легитимно имеют default (e.g. RoleColor) — там фоллбэка на Rifleman достаточно. Используем `//exhaustive:enforce` для opt-in вместо global enable, ИЛИ `default:` + комментарий чтобы linter knew it's intentional. M14.5.0 решает в процессе baseline-fix'а.

- **Spatial hash cell size — повторное обсуждение.** 32m выбран per user decision. Если 14.5 playtest покажет что separation steering получает много false positives (юниты "видят" друг друга через 30 м — bigger query area захватывает unrelated units), можно тюнить на 16 m в M14.5.7 closure pass. Cell size — runtime constant, не архитектурный (RecreateHash() возможен).

- **Particle render performance.** raylib `DrawLine3D` / `DrawSphere` не instanced — каждый particle = отдельный draw call. На 2000 live particles это может быть GPU-bound. Если профайлер покажет — Phase 14.5 closure добавит batching через `DrawTriangleStrip3D` или подобное. Particle pool sizing — tunable константа.

- **Aim mode visual feedback.** Минимум — cursor change на crosshair + 2D HUD chip "Aiming with: AK47 [Esc to cancel]". Polish (Phase 21): 3D arc preview ствола, fade enemy under cursor подсветка. Phase 14.5 ships только базовую UX, не визуальный fancy.

- **WeaponPref validation timing.** Чек `world.Alive(pref.Weapon)` каждый снапшот может быть дорого если orders много. Optimisation: invalidate WeaponPref в `OrderResolverSystem` когда detect'им что weapon мёртв — set marker `OrderParamWeaponPrefInvalid` и WeaponSystem skip'ает быстрее. Phase 14.5 simple: per-snapshot check; refactor в Phase 21 если боль.

- **Garrison fix как proxy для floor-aware queries.** Если M14.5.6 investigation покажет что NavService плохо routes к floor-cells, fix может расшириться на refactor `NavService.FindPath` API — `FindPath(start, goalNavNode)` вместо `FindPath(start, goalPos)`. Это touches Phase 7 architecture. Если scope становится big — выделяем в M14.5.6 только partial-fix (completion gate fix), остальное в Phase 15. Документируем в ISSUES.md что осталось.

- **VisualEvents → Particle migration regressions.** При замене API возможны баги: tracer не появляется (entity spawn fails) или fade неправильный. M14.5.4 включает visual regression check.

- **AttackMove + HoldFire override после M14.5.0.** После того как Issue #9 закрыт (AttackTarget overrides HoldFire), AttackMove order with HoldFire mode тоже должен быть overridable? Plan §AttackMove docs — нет, AttackMove — opportunistic, HoldFire wins (Phase 14 Q2 lock). Это **не меняется в 14.5**. Override только для explicit AttackTarget / SuppressFire. UI indicator (strike-through chip) — communicate this distinction визуально.

- **Particle system Floor traversal.** Particles в подвале / на 2 этаже — должны ли respect floor-Y или drift через перекрытия? Phase 14.5 simple: gravity-only, не collide с walls / floors. Smoke может drift через перекрытия (acceptable visual artefact, Phase 24 polish). Tracer ракет через окно — должен идти через geometry, не collide. Particle не делает LOS-блок check.

- **Hash readers и SpatialHash в parallel pass'е.** SpatialHash.cells читается параллельно из WeaponSystem workers — это безопасно, потому что Rebuild — серийный pre-pass, и no writes во время parallel read. Reader callbacks не должны мутировать hash (alive-check инвариант это покрывает). Документируем явно в hash doc-comment.

---

## Открытые вопросы (требуют решения по ходу M14.5.x)

1. **CompletionRuleKind ↔ OrderKindCode mapping. M14.5.0.**  Сейчас один-к-одному (каждый order kind имеет фиксированный completion rule). Хочется ли позволить переопределение per-order (e.g. AttackTarget с custom timeout from OrderParam)? **Phase 14.5 simple: 1-to-1; Phase 15 может extension.** Финализируем M14.5.0 при имплементации.

2. **Garrison fix scope.** M14.5.6 investigation может выявить что root cause — в NavService через TransitionRegistry, и fix требует значительного рефактора. Если scope > 4-5 часов работы — split: только completion-gate fix (P9) в 14.5, NavService rework в Phase 15. Финализируем после investigation pass'а.

3. **Weapon-bar layout в Inspector.** Один horizontal scrollable bar, или vertical list, или grid? Squad с 8 unit'ами имеет до 16 weapon entities. Многие группируются (4× AK47 = одна строка), но recon с Sniper + 3 Rifleman = 4 unique weapons. UX-decision M14.5.7 при первом render'е.

4. **Aim mode cursor — кастомный sprite или raylib system cursor?** raylib имеет `rl.MouseCursorCrosshair` (`rl.SetMouseCursor(rl.MouseCursorCrosshair)`). Простое решение. Кастомный sprite — лучше визуально но требует sprite asset. Phase 14.5 simple: system cursor.

5. **Particle pool cap soft / hard.** 2000 soft cap (evict oldest). Hard cap = 4000 → refuse spawn? Или dynamic adjustment (Steve = scale down ParticleVisual.TTL when load high)? Phase 14.5 simple: 2000 soft + evict oldest. Tunable in M14.5.4.

6. **Splash damage friendly fire UI warning.** Phase 14 friendly fire on, без warning. Splash damage делает friendly fire более болезненным (один RPG может убить 3 union members). Добавить warning chip "FF risk" если splash zone intersects friendly squad? Phase 21 deferral OK; M14.5.8 может ship warning console log как stopgap.

7. **AttackMove vs HoldFire UI — chip strike-through enough?** Может tooltip + colored border + Animation? Phase 14.5 simple: strike-through + tooltip. Iteration в Phase 21 polish если нужно.

8. **Particle gravity per-kind table — где живёт?** В ParticleSystem как const map, или в `ParticleSpec` table? Если в spec — это четвёртый Spec table в фазе. Phase 14.5 decision: inline в ParticleSystem (kind switch для gravity). Если разрастётся — promote в ParticleSpec в Phase 14.6 или 15.

9. **`exhaustive` linter — какой config?** strict mode (error на missing case без default) or default-permissive (allow default'ы)? Хочется strict но это требует много `//exhaustive:ignore` на legitimate cases. Phase 14.5 simple: enable strict, opt-out где явно нужен default, document pattern в CLAUDE.md.

10. **Spatial hash query result ordering.** ForEachInRadius walks ячейки в row-major порядке внутри radius — order entities внутри callback не сортирован by distance. Если reader нужен closest-first (например WeaponSystem ray-vs-units выбирает nearest hit) — reader сам sort'ит. Альтернатива: API `ForEachInRadiusSorted` — overhead sort'а внутри hash. Phase 14.5 simple: unsorted, reader sort при необходимости.
