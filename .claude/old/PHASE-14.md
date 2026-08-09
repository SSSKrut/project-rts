# Phase 14 — рабочий план

Combat core: первый рабочий цикл «увидел → решил → выстрелил → попал/промазал → урон / suppression / threat». `HP` per-Unit + `DamageSystem` + смерть-as-despawn. `WeaponSystem` параллельно через worker pool: snapshot targets → parallel raycast → serial-apply damage. Чтение `EngagementRules` (Phase 13 standing rule наконец-то получает реального reader'а), reading `OrderParamAttackMove` (Phase 13 scaffold становится живым). Два новых Order kind: `AttackTarget` (фокус на конкретную сущность) + `SuppressFire` (заливание сектора огнём). `SuppressionPropagation`: попадания / близкие промахи поднимают `Suppression.Level` соседей — первый реальный writer для поля, лежавшего пустым с Phase 9. `ThreatSource` короткоживущая ECS-сущность в эпицентре каждого выстрела — Phase 15 SurvivalInstinct будет её читать. `Faction` component: первое hostility-разделение (player vs enemy); test scene добавляет враждебный squad. Визуальная обратная связь — минимальная: `rl.DrawLine3D` tracer от muzzle к hit point с TTL'ом, sphere-impact, HP bar над cap'ом (тот же паттерн что Stamina bar из Phase 13).

**Что в Phase 14 сознательно НЕТ.** Particle system (tracer'ы / impacts / smoke / dust как CPU-параллельный instanced renderer) — Phase 14.5: визуал в Phase 14 — DrawLine3D + sphere'ы, без эффектов. Spatial hashing (`core.SpatialHash` resource grid 32×32 m) — Phase 14.5: на 12+12 юнитах O(N²) приемлемо. Weapon-bar в Inspector (Sea Power-style projection всех Equipment.Primary/Secondary selected entity'ей) — Phase 14.5. Aim-mode cursor для click-weapon-then-target — Phase 14.5. `OrderParamWeaponPref` reader (Phase 13 scaffold) — Phase 14.5, потому что без weapon-bar его некак установить. Map ping system + event log panel — Phase 21 (или 14.5 если останется бюджет). Armor / penetration / cover-shadow / cover-evaluation — Phase 24 / Phase 15. Реактивное поведение пехоты на Suppression (find cover, prone, scramble) — Phase 15 SurvivalInstinct (Phase 14 только пишет Suppression.Level и ThreatSource). Wounded state / corpses / lootable equipment — Phase 15 / 25. Grenade trajectories (физика подствольника / гранат) — Phase 14.5 или 15. Reloading animation / time-cost — Phase 25 polish. Friendly fire UI warning — Phase 21. Fire-and-movement доктрины (один suppresses, другой advances) — Phase 15. Target leading / ballistic drop — Phase 15 / 24.

ROADMAP — высокоуровневый трекер. COMMAND-MODEL.md §5 (per-weapon control), §7 (реактивное поведение / контракт между фазами) — спецификация. Этот файл — рабочий план фазы.

---

## Решения, которые лочим до начала кода

**P1. `HP` — отдельный компонент per-Unit.**

```go
// components/hp.go
type HP struct {
    Current float32 // 0..Max
    Max     float32
}
```

`Max` ставится `RoleService.AssignRole` per-role:

| Role | HP Max |
|---|---|
| Rifleman / Leader / Grenadier / Medic / Radio / Engineer / Demo | 100 |
| MachineGunner | 110 (защитный жилет + дополнительная масса) |
| Sniper / ATGunner | 90 (lighter loadout assumption) |

Числа placeholder, balance — Phase 25. Stamina идёт parallel'но: HP is "alive" status, Stamina is "tired" status — orthogonal axes.

**P2. Смерть = despawn (Phase 14 simplification).**

`HP.Current <= 0` → `DamageSystem.applyDeath(unit)`:
1. `SquadService.Leave(unit)` — открепить от ростера (handles compaction + auto-despawn пустого squad'а).
2. Destroy Equipment.Primary / Secondary entity'и (mirror'ит `RoleService.AssignRole` teardown logic).
3. `world.RemoveEntity(unit)`.

Wounded state, corpses, drop-loot — Phase 15+. Phase 14 — clean despawn; ping в console log для дебага.

**P3. `WeaponSystem` — параллельный, snapshot → parallel raycast → serial apply damage.**

Структура (же паттерн что UnitMovementSystem / Vision из Phase 11.5/11.6):

```go
type WeaponSystem struct {
    filter        *ecs.Filter… // Unit + Awareness + Equipment + ...
    pool          *core.WorkerPool

    // Reusable buffers
    workBuf       []weaponWork    // snapshot rows
    damageBuf     []damageEvent   // shared event queue, serial apply
    threatBuf     [][]threatSpec  // per-worker buffer, serial spawn
    tracerBuf     [][]tracerSpec  // per-worker buffer for visual events
}
```

Per-tick flow:
1. **Serial snapshot**: walk firing-eligible units; resolve target via Awareness + EngagementRules gate; capture muzzle pos + aim point + weapon ref.
2. **Parallel raycast** (через `WorkerPool.ParallelForIndexed` + per-worker buffers): для каждого Snapshot — raycast по LOS (walls + props), apply dispersion, decide hit/miss. Записать в per-worker buffer события (damage / threat / tracer).
3. **Serial apply**: merge per-worker buffers → apply HP damage, spawn ThreatSource entity'и, spawn tracer/impact records.

LOS reuse — Phase 6 / Phase 7 уже имеют raycast helpers против walls + props (`anyLosWallBlocks`, `anyLosPropBlocks`). Phase 14 переиспользует.

**P4. Hybrid bullet physics — instant raycast, без in-flight bullet entity'ей.**

GAMEDESIGN.md cross-cuts: каждый выстрел = один raycast от muzzle к aim point. Hit/miss/cover вычисляется по результату. Никаких persistent bullet entity'ей в симуляции.

Aim point = target unit's torso center + dispersion offset. Dispersion = `Weapon.Dispersion` (новое поле, см. P10) × range / 100 м. Higher Pace / Suppression → wider dispersion (Phase 14 simple model: `dispersion *= 1 + 0.3*movingFactor + 0.5*suppressionLevel`).

**P5. `SuppressionPropagation` — radius-based, decay over time.**

При каждом выстреле (hit or miss):
- Hit: target unit получает `Suppression.Level += 0.5` (direct hit — реалistically pinned)
- Miss: каждый unit в радиусе 5 м от impact'а получает `Suppression.Level += 0.2 * (1 - dist/5)` (близкий промах — psych pressure)
- Threat direction: пишется в `Suppression.ThreatDir` (vector от impact к unit) — Phase 15 будет читать

Decay: each tick `Suppression.Level -= SuppressionDecay * dt` (default 0.1/s), clamped to [0..1]. Decay handled in WeaponSystem post-pass (parallel-safe — каждый unit пишет свою Suppression).

**P6. `ThreatSource` — короткоживущая ECS-сущность.**

```go
// components/threat.go
type ThreatSource struct {
    Origin    WorldPos  // where the threat came from (muzzle pos, NOT impact)
    Severity  float32   // 0..1 — fire intensity / round count
    SpawnTime float32   // session-time at spawn
    TTL       float32   // seconds before despawn (default 3.0)
}
```

Spawned by WeaponSystem (один per выстрел или per N выстрелов — Phase 14 simple: один per shot). Phase 15 SurvivalInstinct читает: weighted average ThreatDir → выбирает cover slot.

Cleanup system `ThreatDecaySystem` каждый тик despawn'ит истёкшие entity'и. Простой Filter + RemoveEntity post-pass.

**P7. `Faction` component — первое hostility-разделение.**

```go
// components/faction.go
type Faction struct {
    ID uint8 // 0 = player, 1 = enemy red, 2..7 reserved
}
```

Спавнится в `unitFactory` (теперь принимает faction param) или в SquadService.CreateFromTemplate (новый param). Default = 0 (player) для backward-compat.

Test scene Phase 14: 3 existing player squads (Faction=0) + 1 new enemy squad (Faction=1) на противоположной стороне. WeaponSystem проверяет `target.Faction != self.Faction` перед firing-gate. Render layer: `squadColor` берёт base hue из Faction, jitter из entity ID — player squads blue-ish, enemy red-ish.

**P8. Два новых Order kind: `OrderKindAttackTarget` + `OrderKindSuppressFire`.**

`OrderKindAttackTarget`:
- `OrderTarget.Entity` = enemy unit / squad / vehicle
- Squad концентрирует огонь на этой цели до уничтожения / cancel'а / target out-of-LOS-for-N-seconds
- Overrides `EngagementRules.Mode == HoldFire` (explicit player order trumps standing rule)

`OrderKindSuppressFire`:
- `OrderTarget.Pos` = центр сектора
- `OrderParamSuppress{Radius, AmmoCap}` опциональный компонент
- Squad заливает огнём сектор, fires hostile-direction blind. Используется AttackMove flag pattern — units могут двигаться при выполнении (squad's MacroPath держит position).
- Cancel: low ammo (если AmmoCap reached) OR player Stop OR target area без enemy LOS-contacts N сек

Resolver обновление (`resolveTargetIntoOrder` в command.go): hit на enemy unit/squad entity → AttackTarget; hit на пустую местность с pie-menu choice → SuppressFire.

**P9. `EngagementRules` reader: WeaponSystem gating logic.**

Перед firing decision per-unit:

```
allow := false
switch rules.Mode {
case HoldFire:
    // Default no. Override if unit's squad has active AttackTarget OR SuppressFire order
    allow = squadHasAttackOrSuppressOrder()
case ReturnFire:
    // Only if unit recently shot at OR threat in awareness
    allow = unit.Suppression.Level > 0.1 || awarenessHasHostileNearby()
case FreeFire:
    // Default yes
    allow = true
}
if !allow {
    return // skip firing
}
// Target type gate (Phase 14: only Inf exists; Phase 16 adds Arm/etc.):
if target is Inf && !rules.FireOnInf { return }
// Per-weapon range / RoF / ammo / LOS checks proceed
```

`AttackMove` flag reading (Phase 13 scaffold becomes live): if unit's current Order has `OrderParamAttackMove` AND order is MoveTo → `allow` остаётся true даже под движением (otherwise WeaponSystem требует stationary unit для firing accuracy).

**P10. WeaponKind stats — refinement Phase 12 placeholder'ов.**

Phase 12 `primaryStats` имел grubby placeholder numbers. Phase 14 уточняет с реалистичными значениями:

| WeaponKind | Damage | RangeM | RoF (shots/sec) | Ammo | Dispersion |
|---|---|---|---|---|---|
| AK47 | 28 | 300 | 4.0 | 30 | 0.03 |
| PKM | 30 | 500 | 8.0 | 100 | 0.05 (burst-fire) |
| SVD | 70 | 600 | 0.5 | 10 | 0.005 (precision) |
| RPG7 | 200 (vs Inf splash) / 600 (vs Arm) | 200 | 0.1 | 3 | 0.02 |
| GP25 | 50 (splash) | 150 | 0.3 | 8 | 0.04 |
| Makarov | 18 | 30 | 3.0 | 8 | 0.06 |

Dispersion = radians (small-angle) — multiplier на target lateral offset at range. Phase 14.5 / 25 — реалистичный balance pass.

Splash damage (RPG, GP25): Phase 14 = single hit only (radius=0). Phase 14.5 (с particle system) добавляет real splash.

**P11. Damage application — flat, no armor.**

```go
hp.Current -= weapon.Damage * stanceMul[target.Stance]
```

Stance multiplier:
- Stand: 1.0 (full silhouette exposed)
- Crouch: 0.8 (reduced silhouette)
- Prone: 0.5 (smallest silhouette)

Phase 14 — flat. Phase 24 (content pipeline) или Phase 15 добавит armor / penetration / damage-falloff over range / cover-damage-reduction.

**P12. Visual feedback — DrawLine3D tracer + sphere impact + HP bar.**

Tracer: пр shot WeaponSystem spawn'ит `tracerSpec{From, To, Color, TTL}` в transient buffer. Renderer (main.go 3D pass) walks buffer, `rl.DrawLine3D` per tracer, fades alpha по `(TTL - age) / TTL`. Default TTL = 0.15 sec. Color: white для AK / PKM, yellow для tracer rounds (every 5th), red для high-velocity (SVD).

Impact: `impactSpec{Pos, Color, TTL}`. Renderer `rl.DrawSphere` radius=0.1m, fade alpha. Default TTL = 0.25 sec.

Both spec'и live в `core.VisualEvents` resource (новое) — простой ring-buffer slice с per-frame cleanup. Не ECS-entities (overhead не оправдан для 100s-of-events / frame). Phase 14.5 заменит на Particle system который ECS-entity-based.

HP bar: 2D screen-projected pill над unit cap (mirror Stamina bar Phase 13). Show только при `Current < Max`. Width 50 px, height 3 px, color green→yellow→red zones. Y-offset выше Stamina bar (Stamina bar выше cap'а; HP bar выше Stamina bar).

**P13. `OrderParamAttackMove` reader — WeaponSystem проверяет.**

Phase 13 attached the marker; Phase 14 reads:
1. Find unit's active Order via SquadMember → Squad → OrderQueueHead.First.
2. If Order has `OrderParamAttackMove` AND OrderKind == MoveTo → unit допускается стрелять во время движения.
3. Иначе fire-while-moving requires `ReturnFire` или `FreeFire` rules AND awareness target (standard gate).

Без AttackMove (просто MoveTo): WeaponSystem skip'ит firing — squad движется без contact discipline. Это и есть semantic distinction MoveTo vs AttackMove из GAMEDESIGN §3.

**P14. Test scene additions: один enemy squad + ScenarioTestCombat.**

main.go test-scene расширяется:
- Existing 3 player squads (LightInfantry / MGTeam / ATTeam) → Faction=0
- 1 new enemy squad: TmplMotorRifle (8 units) на противоположной стороне в ~60 м от player base → Faction=1
- Enemy squad initial Order: DefendPosition в их spawn area (Phase 14 simple: они стоят и ждут)
- Player engages by giving orders manually

Phase 14.5 / 15 → enemy patrols, reactive movement. Phase 14 — static enemies для отладки firing loop.

---

## Семь мильстоунов

### M14.1 — `HP` + `Faction` components + `DamageSystem` (death-despawn)

**Цель.** Существуют `components.HP` (Current/Max) и `components.Faction` (ID uint8). `RoleService.AssignRole` устанавливает HP.Max per-role (P1 таблица) и Faction (новый аргумент в AssignRole или в unitFactory). `DamageSystem` (новый service object либо лёгкий System) применяет накопленные damage events: уменьшает HP, при HP≤0 — calls `SquadService.Leave(unit)` + destroy equipment + `RemoveEntity`. Test scene расширен enemy squad (Faction=1).

**Делаем:**
- `components/hp.go`: `HP{Current, Max}` struct.
- `components/faction.go`: `Faction{ID uint8}` + const'ы FactionPlayer=0, FactionEnemyRed=1.
- `systems/role_service.go`: `AssignRole` устанавливает HP per role + spawns Stamina (existing) — теперь и HP. Faction задаётся в unitFactory.
- `main.go::unitFactory` принимает faction param. `SquadService.CreateFromTemplate` тоже — Phase 14 simple: extend signature, default=Faction{} (player).
- `systems/damage.go` (новый): `DamageSystem` или `DamageService` — реализация `applyDeath(unit ecs.Entity)` (destroy primary/secondary, SquadService.Leave, RemoveEntity).
- Test scene: 1 enemy squad spawn (TmplMotorRifle, Faction=1, DefendPosition order).

**Проверяем.** `go build` чист. Test scene: 4 squads visible (3 player + 1 enemy). Enemy squad имеет Faction=1, HP=100 на каждом unit'е. Inspector single-unit view добавлен HP row (под Stamina). Manual debug: call DamageSystem.Apply(enemy_unit, 150) → unit despawn'ится, squad's roster количество уменьшится.

### M14.2 — `WeaponSystem` core (snapshot → parallel raycast → serial apply)

**Цель.** WeaponSystem live в pipeline после Vision. Per-tick walks units с valid awareness target, делает raycast, accumulates damage events, accumulates threat / tracer specs. Serial post-pass применяет damage и spawn'ит ThreatSource'ы. Per-weapon RoF cooldown + ammo decrement. Visual feedback — `core.VisualEvents` resource populated with tracer + impact specs.

**Делаем:**
- `components/weapon.go`: добавить `LastFiredAt float32` (session-time of last shot) для RoF cooldown — alternative: на Unit как `WeaponCooldown{Until float32}` map'ить per-weapon — simpler keep on Weapon component.
- `components/weapon.go`: добавить `Dispersion float32` field (radians).
- `core/visual_events.go` (новый): `VisualEvents` resource — slice'ы `[]TracerSpec`, `[]ImpactSpec` with TTL. Cleared / decayed by main.go each frame.
- `systems/weapon.go` (новый): WeaponSystem struct, InitUI, Update (parallel-aware). Snapshot includes Unit + WorldPos + Stance + Equipment + Awareness + Faction + SquadMember + Suppression.
- WeaponSystem.Update:
  1. Serial snapshot — collect firing-eligible units (alive, has primary weapon, has awareness target with valid Faction, RoF cooldown ready). Resolve target Pos + LOS-check (reuse Phase 6 anyLosWallBlocks via shared helper or inline).
  2. Parallel per-snapshot — apply dispersion, raycast (against units in awareness radius for unit-hit; against walls/props for LOS), decide hit/miss. Append to per-worker damage / threat / tracer buffers.
  3. Serial post-pass — merge buffers; apply damage (HP decrement, death-despawn if ≤0); spawn ThreatSource entity'и; append tracer/impact to VisualEvents.
- `main.go`: register WeaponSystem after Vision, before particle/render. Add `core.VisualEvents` resource.

**Проверяем.** Test scene start → player squad с RoE=FreeFire видит enemy squad на дистанции → tracer lines от player muzzles flying. Enemy HP падает в Inspector view. Через ~30 сек enemy squad полностью wiped (HP=0 → despawn). Console log: "WeaponSystem: 12 shots, 8 hits, 4 misses". `go test -race ./core/` clean.

### M14.3 — `EngagementRules` reader + `OrderParamAttackMove` reader

**Цель.** WeaponSystem gating decision полностью включает RoE check (Mode + target type) и AttackMove flag. Phase 13 standing rules становятся live. Per-role default'ы из Phase 12 наконец-то имеют видимый эффект: Sniper не стреляет без приказа (HoldFire); MG team стреляет первая в FreeFire+Inf; AT не реагирует на пехоту (FireOnInf=false).

**Делаем:**
- `systems/weapon.go::shouldFire(unit, awareness, rules, order)`: реализация P9 decision flow. Inputs: unit's effective EngagementRules + active Order + awareness target's Faction.
- Reading `OrderParamAttackMove`: walk SquadMember → Squad → OrderQueueHead.First → check `orderAttackMoveMap.Has(orderEnt)`. Если есть AND OrderKind == MoveTo → allow fire-while-moving; else firing требует stationary unit (Motion.Speed < threshold).
- `systems/weapon.go::resolveTargetType(target)`: возвращает infantryFlag / armorFlag для FireOn* gate. Phase 14: только Inf (vehicles появятся Phase 16, до тех пор все targets — Inf).

**Проверяем.** Test scene: Inspector toggle RoE Mode → HoldFire → squad перестаёт стрелять немедленно. Set back to FreeFire → возобновляется. Sniper в составе squad'а (если есть, TmplRecon вкл Sniper) — по умолчанию не стреляет (per-role HoldUntilOrdered=true via BehaviorRules from Phase 13 — но Phase 14 BehaviorRules ещё не reader, так что override через EngagementRules.Mode=HoldFire на per-Unit override Phase 21+). Phase 14: enforce'им на squad-level — squad's RoE Mode applies to all members. AttackMove: Alt+RMB на enemy squad → squad MoveTo'ит к target и одновременно стреляет (target в awareness). Без Alt: MoveTo без firing.

### M14.4 — `OrderKindAttackTarget` + `OrderKindSuppressFire` + resolver

**Цель.** Два новых Order kind в enum. Resolver (`resolveTargetIntoOrder` в command.go) детектирует enemy entity hit → AttackTarget; pie-menu выбор "Suppress" + terrain hit → SuppressFire. `OrderResolverSystem` per-kind completion: AttackTarget completes когда target dead / out-of-LOS-for-N-sec / Squad has no ammo. SuppressFire completes когда ammo low / timer / cancel.

**Делаем:**
- `components/order.go`: add `OrderKindAttackTarget`, `OrderKindSuppressFire` к OrderKindCode enum.
- `components/order.go`: add `OrderParamSuppress{Radius, AmmoCap, StartTime}` опциональный компонент.
- `command.go::resolveTargetIntoOrder`: hit на entity с Faction != self → AttackTarget. Pie menu choice handled через kindOverride.
- `command.go::HitTester`: add unit / squad entity detection (вне-building hits на enemy units). Currently HitTester only detects Building / Trench. Phase 14 extends к units (raycast против unit collider или distance threshold around projected screen pos).
- `systems/order_resolver.go`: per-kind completion для AttackTarget (target dead/missing) и SuppressFire (ammo / timer / no contacts N sec).
- `systems/squad_macro_path.go`: AttackTarget и SuppressFire — Squad стоит at current position (no macro path movement) если target в range; otherwise MoveTo target's general area (within weapon range). Phase 14 simple: AttackTarget держит squad in-place, доверяя что они уже в range. Out-of-range case — log warning, Order failed.

**Проверяем.** Test scene: select player squad, RMB на enemy unit → AttackTarget order issued (Inspector shows kind). Squad focuses fire on that target до его уничтожения; затем Order completes (Inspector says "Idle"). SuppressFire — RMB-hold → pie menu (если segment added) → выбор Suppress → MMB-drag-ish (or pie menu choice with terrain target) → squad opens fire на сектор. Не уничтожает specific target — просто заливает огнём.

### M14.5 — `SuppressionPropagation` + `ThreatSource` decay

**Цель.** Suppression.Level пишется реальными hit/miss events. ThreatSource entity'и spawn'ятся per-shot. Decay системы (для Suppression и для ThreatSource TTL) работают per-tick. Phase 15 SurvivalInstinct получает contract: writer/reader split documented.

**Делаем:**
- `components/threat.go`: `ThreatSource{Origin, Severity, SpawnTime, TTL}` struct.
- `systems/weapon.go::serialApply` (or post-pass): for each hit/miss, walk units in 5m radius around impact, `suppression.Level += hitMul * (1 - dist/5)`; write `suppression.ThreatDir = normalize(impact - unit)`. Direct-hit target gets `level += 0.5`.
- `systems/threat_decay.go` (новый, или append to WeaponSystem post-pass): per-tick walk all ThreatSource entities, `if elapsed - SpawnTime > TTL: RemoveEntity`. Lightweight.
- `systems/suppression_decay.go` (or inline в WeaponSystem): per-tick `suppression.Level -= 0.1 * dt`, clamp [0..1].
- Inspector single-unit view: Suppression row (existing scaffold from Phase 7) теперь shows live values. Phase 14 — численное "Supp: 0.42"; Phase 21 — bar.

**Проверяем.** Combat scene: enemy hit'ы на player unit → его Suppression.Level растёт, видно в Inspector. После того как огонь стих → decay'ит к 0 за ~10 секунд. ThreatSource entity count через `Ctrl+P` snapshot — поднимается во время firefight, падает после.

### M14.6 — Visual feedback: tracer / impact / HP bar

**Цель.** В 3D сцене видны tracer-линии от muzzle к target points (fade over 150ms) и impact spheres на hit points (fade over 250ms). Над каждым юнитом HP bar при Current < Max (mirror Stamina bar). Map renderer: enemy squads tinted differently (red-ish base hue from Faction=1).

**Делаем:**
- `core/visual_events.go` (если не в M14.2): `TracerSpec{From, To rl.Vector3, Color rl.Color, SpawnTime, TTL float32}`, `ImpactSpec{Pos, Color, SpawnTime, TTL}`. Slice + AppendTracer / AppendImpact methods. Per-frame `Decay(elapsed)` drops expired.
- `render_world.go::drawTracers` / `drawImpacts`: walk VisualEvents, fade alpha by `(TTL - age) / TTL`, `rl.DrawLine3D` / `rl.DrawSphere`.
- main.go: pull VisualEvents into 3D render pass (after terrain/units/buildings, before label overlay).
- `render_world.go::drawUnitHPBar`: 2D screen-projected mini-bar above cap, mirroring drawUnitStaminaBar from Phase 13. Y-offset +0.4m above stamina bar so they stack.
- main.go render loop: добавить `drawUnitHPBar(renderPos, *st, role, hp.Current, hp.Max, panel3DContent)` рядом с stamina bar call.
- `ui/map_render.go::squadColor` (or whoever computes squadColor): split base hue by Faction. Player squads = blue/green palette; enemy = red/orange. Same entity-ID jitter on top для distinguish squads of same faction.

**Проверяем.** Combat scene visually shows firing: white tracer lines flying between squads. Impacts визуально pop. Damaged units show shrinking HP bar (green → yellow → red). Map view immediately shows red enemy marker vs blue/green player markers. `go run -workers=4` performance: 12+12 units shooting at each other — should hold 60 fps on dev machine.

### M14.7 — Stats rebalance + closure

**Цель.** Per-weapon stats (P10) применены к Phase 12 placeholder'ам. Test scene engagement плейтест'ом — балансное чувство (squads не wipe'ятся за 2 секунды и не стреляют 5 минут без эффекта). ROADMAP update, PHASE-14.md → ✅, переход к Phase 14.5 (visuals + UX полировка).

**Делаем:**
- `systems/role_service.go::primaryStats`: уточнить numbers per P10 таблице. Dispersion field в Weapon заполняется.
- Plaintest loop: run combat scenario several times, tune RoF / damage / dispersion until firefights feel ~30-45 sec для 4 vs 8 squads at 50m range. Document final numbers в notes.
- ROADMAP.md: Phase 14 → ✅; активная фаза становится 14.5 (или 15 если 14.5 не нужна).
- PHASE-14.md → archive `.claude/old/PHASE-14.md`.
- Memory: hotkey-collisions если новые есть (Phase 14 не должна добавлять hotkey, всё через ПКМ + Inspector — verify).

**Проверяем.** Test scene Phase 14: дать playthrough'ить пару раз. Subjective: combat feels like a tactical engagement, not StarCraft sniper duel. Phase 15 inputs ready: Suppression.Level пишется, ThreatSource spawn'ятся, AttackTarget/SuppressFire доступны.

---

## Что считаем «закрытием Phase 14»

- 3 новых компонента: `HP`, `Faction`, `ThreatSource`. Optional `OrderParamSuppress`.
- 2 новых Order kind в enum: `OrderKindAttackTarget`, `OrderKindSuppressFire`.
- `Faction` спавнится в RoleService / SquadService с per-template faction param.
- `WeaponSystem` parallel-aware: snapshot → parallel raycast → serial apply damage / threat / tracer. RoF cooldown + ammo decrement + dispersion.
- `EngagementRules` (Phase 13 standing rule) и `OrderParamAttackMove` (Phase 13 marker) — оба читаются WeaponSystem'ом. Real gating.
- `DamageSystem` (или DamageService) — applyDeath: Leave + destroy equipment + RemoveEntity. Squad auto-despawn'ит пустой ростер.
- `SuppressionPropagation` пишет `Suppression.Level` + `ThreatDir` на units в радиусе 5m от impact. Decay в WeaponSystem post-pass.
- `ThreatSource` entity'и short-lived, spawn'ятся per-shot, expire по TTL через `ThreatDecaySystem` (или inline cleanup).
- `core.VisualEvents` resource: tracer + impact specs с per-frame decay. Renderer walks list.
- HP bar в 3D над cap'ом (когда Current < Max). Inspector single-unit view расширен HP row.
- Faction-based map color: enemy squads красные, player squads сине-зелёные.
- Test scene: 4 squads (3 player + 1 enemy MotorRifle). Combat работает out-of-the-box.
- Per-weapon stats refined по P10 таблице.
- Pipeline: добавляются 2-3 system'а в порядок. `... → vision → weapon → threat_decay → (existing rest) → ...`. Suppression decay либо inline в WeaponSystem либо отдельный System.

После этого — обновление ROADMAP, Phase 14 → ✅, переход к **Phase 14.5** (Particle system + weapon-bar + aim mode + spatial hash + map ping + event log) или сразу Phase 15 (Tactical AI) если 14.5 не блокирует.

---

## Заметки на полях

- **Death = despawn — Phase 14 simplification.** Wounded state (юнит на земле, можно вылечить медиком), corpses (Prop entity на месте death), lootable equipment (поднять weapon на пол) — Phase 15 / 25. Phase 14 needs the simplest viable death so Combat loop testable.

- **HP перcent decimal precision.** `HP.Current float32` — позволяет dot damage над 0.5 / 1.0 без integer rounding. Если Phase 14.5 / Phase 24 add armor / penetration which produce floating-point values, the field accepts naturally.

- **WeaponSystem parallel race-safety.** Standard pattern: workers write only to per-worker buffers + own entity's component pointers. The damage queue is per-worker; serial post-pass merges. ThreatSource spawn — archetype mutation, must be in serial post-pass. Tracer/Impact records — per-worker append к VisualEvents (which is read-only during parallel section).

- **RoF cooldown on Weapon component vs on Unit.** Two design choices:
  - (a) `Weapon.LastFiredAt float32` — cooldown lives on the weapon entity. Pros: each weapon kind has its own cooldown; switching weapons (Active changes) automatically resets cooldown. Cons: extra component access per shot.
  - (b) `Unit.WeaponCooldown{Until float32}` — cooldown on the firing unit, regardless of weapon. Pros: single map access; faster snapshot. Cons: switching weapons doesn't reset.

  Going with (a) для cleanliness; perf на 12 юнитах negligible. If profiler shows hot — switch к (b) в Phase 14.5.

- **Dispersion = small-angle radians.** A 0.03 rad dispersion at 100m → 3m lateral spread. Realistic для AK74 (0.05-0.1 MOA at 100m). Modifier для движения / suppression:
  ```
  effective_dispersion = base_dispersion * (1 + 0.3*movingFactor + 0.5*suppressionLevel)
  ```
  `movingFactor` = 1 если Motion.Speed > 1.0; иначе 0. Phase 14 simple — Phase 15 ML может ввести skill modifier per-unit.

- **Awareness as firing target source.** Phase 7 VisionSystem пишет Awareness.LastSeen FIFO (8 slots). WeaponSystem reads: prefers most recent target, then closest. Phase 14 не делает sophisticated target selection (приоритет по threat level / role) — это Phase 15 TargetPriority.

- **Suppression decay rate.** 0.1/s default — full suppression (Level=1.0) clears в 10 sec без новых hits. Tunable. Phase 15 doctrine может modify (Recon squads — faster decay, demoralized infantry — slower).

- **ThreatSource cost.** Per-shot spawn = potentially много entity'ей при intense firefight (8-shot/sec MG × 5 squads = 40/sec). TTL=3s → ~120 alive at peak. ECS entity cost negligible (~50 bytes archetype + ThreatSource + WorldPos). Phase 14.5 may dedupe (multiple shots from same muzzle → one merged ThreatSource), но Phase 14 simple = per-shot.

- **Faction в test scene.** Player squads spawn before enemy. SquadService.CreateFromTemplate signature расширяется `(template, pos, formation, roleService, unitFactory, faction)`. Default faction = FactionPlayer (0) — backward compat.

- **Map color по Faction.** `squadColor(id, faction)` или `squadColor(squad)` — second simpler (function reads Faction internally). Updates `ui/map_render.go` to take SquadColor func that знает о Faction. Inspector uses same.

- **`OrderKindAttackTarget` completion when target dies mid-fight.** Target despawn'ит (via DamageSystem.applyDeath) → `world.Alive(target.OrderTarget.Entity) == false`. OrderResolverSystem detects → Order → Completed. Squad's queue advances.

- **`OrderKindSuppressFire` ammo cap.** Default `OrderParamSuppress.AmmoCap = 100` (rounds across squad). When `sum(ammo_consumed_during_this_order) >= AmmoCap` → Completed. Phase 14 simple: timer-based fallback if AmmoCap not set (default 30 sec engagement). Phase 21 UI shows progress.

- **Friendly fire — Phase 14 simple: any unit hit by raycast takes damage regardless of Faction.** Realistic: stray rounds can hit friendlies. WeaponSystem doesn't filter raycast hits by Faction (firing decision filters target selection by Faction; raycast resolution is physics). Phase 21 может add UI warning "you shot friendly X".

- **Particle system deferred к Phase 14.5.** Phase 14 visual = DrawLine3D + DrawSphere которые сильно проще архитектурно. Particle system нужен для smoke / dust / fire-secondary / debris — visual polish lift, не gameplay. Splitting keeps Phase 14 focused.

- **Spatial hash deferred к Phase 14.5.** На 12+12 units, O(N²) suppression propagation = 576 distance checks per shot × 10 shots/sec = 5760 checks/sec. Negligible. Phase 14.5 / 15 при scale-up.

- **Weapon-bar deferred к Phase 14.5.** Sea Power-style click-weapon → aim mode → click target — отличный UX, but requires UI mode state, cursor change, ESC-cancel logic. Not blocker для Combat loop. Phase 14 player issues fire через AttackTarget order (RMB on enemy).

- **Map ping deferred к Phase 14.5 / 21.** Phase 14 — console log для debug. Map ping requires MapPing ECS entity + render system + event source wiring. Worth its own focused milestone in 14.5.

---

## Открытые вопросы (требуют решения по ходу M14.x)

1. **HP Max per role — final numbers.** PHASE-13 noted physical-load rationale. Phase 14 P1 sets coarse table. Plaintest may reveal MG should be 120 (not 110) — finalize empirically M14.7.

2. **AttackMove + HoldFire combination.** Squad с Mode=HoldFire получает AttackMove order — что важнее? RECOMMENDED: Mode=HoldFire wins (player explicitly forbade firing — Alt was just "fire if you can" hint). Конечный gate'ит на Mode != HoldFire OR explicit AttackTarget order. Финализируем M14.3.

3. **Target leading.** Target moving — aim at current position vs lead by predicted speed × bullet-flight-time? Phase 14 simple: instant raycast (no flight time) → aim at current pos. No leading needed. Phase 15 / 24 для realistic ballistic adjustment.

4. **Reload time.** Phase 14 simple: no reload — ammo decrements until 0, then weapon goes silent. Phase 25 polish adds reload animation + 2-3 sec downtime when ammo runs out (auto-reload from inventory).

5. **Inventory ammo carrying.** Per-Equipment Phase 14: ammo lives on Weapon (existing field). One mag (Phase 12 placeholder = 30 for AK, 100 for PKM). When 0 → silent. Resupply / mag-swap — Phase 18 (Engineering / logistics) или 24.

6. **Cover shadow от стен — Phase 14 reads?** Damage reduction за cover (shielded by wall corner / sandbag / tree trunk) — Phase 14 not yet. Cover affects HIT decision (line blocked → miss) but doesn't reduce damage on partial-cover hit. Phase 15 / 24 для cover damage reduction model.

7. **Squad's RoE Mode vs per-Unit Sniper preference.** Phase 13 per-role default'ы set per-unit-ROE preference (Sniper = HoldFire), но Phase 13 P1 fixed per-Unit override deferred to Phase 21. Phase 14 enforces squad-level only. Sniper in mixed squad fires whenever squad's Mode permits. Per-Unit RoE override — Phase 21.

8. **Visual events buffer overflow.** 100s of tracers / impacts at peak. `core.VisualEvents` slice grows / shrinks unbounded. Cap at e.g. 200 each, drop oldest if exceeded. Phase 14.5 (particle system) replaces with proper ECS-entity-based pool.

9. **Friendly squad gets caught in SuppressFire sector.** Phase 14 = friendly fire on, so they take damage. Phase 21 UI warning. Phase 15 reactive — friendly squad in suppress zone should auto-move out (TacticalOverride).

10. **DamageSystem run timing.** Inline в WeaponSystem.serialApply vs separate System after WeaponSystem. RECOMMENDED: inline — keeps damage-events lifetime short (events created and consumed in same tick). Separate System adds queueing overhead with no benefit at Phase 14 scale. Финализируем M14.1 при имплементации.

11. **Order kind HitTester for unit entities.** Currently HitTester resolves Building / Trench / terrain. Phase 14 needs unit hit (для RMB-on-enemy → AttackTarget). Implementation: ray-cast против unit colliders within view-cone (or just iterate units и find nearest screen-projected). Phase 14 simple version probably fine; refine in Phase 21 UI.
