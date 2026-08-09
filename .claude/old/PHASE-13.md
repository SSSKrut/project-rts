# Phase 13 — рабочий план

Movement & Engagement standing rules: три standing-rule компонента на Squad — `MovementProfile` (Pace / Stance default / Posture / PathStyle), `EngagementRules` (Mode + 4 target toggles), `BehaviorRules` (auto-toggle gate'ы для Phase 15) — плюс `Stamina` per-Unit, плюс per-role default'ы через `RoleService.AssignRole`. NavService.FindPath принимает PathStyle, NavGrid bake расширяется `CoverDistance` byte для CoverSeek. UnitMovementSystem читает Pace × Stance × Stamina с auto-fallback в Walk при истощении. Sneak больше не Order kind — становится preset для `Ctrl+RMB` через `OrderParamMovementProfile` override. Inspector получает три quick-bar секции (Movement / Engagement / Behavior) с пресетами и toggle'ями.

**Что в Phase 13 сознательно НЕТ.** Реальные читатели `BehaviorRules` (SurvivalInstinct, ScatterProtocol) — Phase 15. Реальный читатель `EngagementRules.Mode` (WeaponSystem) — Phase 14. Audio detection effect для Posture — Phase 15. `Standoff` / `SectorYaw` / `SectorHalfDot` в EngagementRules — Phase 14 (нужны weapon ranges). Per-Unit standing rules override UI — Phase 21. Doctrines (Patrol/Assault/Stealth/Defense) как macro-presets — Phase 15. Tree visualization Order'ов — Phase 21. Marker context menu — Phase 21. Weapon-bar — Phase 14. Ghost preview + facing-drag — Phase 13.5 (отдельная под-фаза). Joint orders / Embark / Dismount — Phase 17. VehicleSystems scaffold — Phase 16+. Map ping / event log / auto-pause matrix — Phase 21. `OrderKind=DisperseOnLine` — Phase 19. `hasRadiomanInRoster` уже работает с Phase 12, не трогаем.

ROADMAP — высокоуровневый трекер. `COMMAND-MODEL.md` §2 / §4 / §6 / §7 / §9 — спецификация. Этот файл — рабочий план фазы, обновляется по ходу выполнения.

---

## Решения, которые лочим до начала кода

**P1. Все три standing-rule компонента живут на Squad-сущности.**

`MovementProfile`, `EngagementRules`, `BehaviorRules` — компоненты на Squad-entity, не на Unit. Per-Unit override (например, sniper в составе squad'а имеет свой MovementProfile.Stance=Prone) **отложен до Phase 21** (UI expansion). В Phase 13 squad — единственная единица настройки.

Обоснование: 95% UX-сценариев — изменение настроек всего отряда. Per-unit override — продвинутый workflow, требует drag-and-drop в Inspector'е и не критичен для базовой tactical playability. Phase 13 фокусируется на squad-level плотности UI.

Solo units (без squad'а — теоретически возможно, например после disband'а через `U`) используют system-defaults в UnitMovementSystem без MovementProfile lookup'а. Они и сейчас редко появляются в playthrough'е.

**P2. `Stamina` — per-Unit компонент. MaxLevel зависит от роли (per-role weight modifier).**

```go
// components/stamina.go
type Stamina struct {
    Current     float32  // 0..MaxLevel
    MaxLevel    float32  // зависит от Equipment weight (per-role)
    RecoverRate float32  // per-second, постоянная per-unit
}
```

Per-role MaxLevel modifier (применяется в `RoleService.AssignRole`):

| Role | MaxLevel mul | Reason |
|---|---|---|
| MachineGunner | 0.7 | Тяжёлый PKM + лента |
| ATGunner | 0.7 | RPG + резервные ракеты |
| Engineer | 0.8 | Инструменты + лопата |
| DemoMan | 0.8 | Заряды |
| RadioOperator | 0.85 | Р-159 (~10 кг) |
| Все остальные | 1.0 | Стандартный комплект |

Базовое значение `MaxLevel=1.0` (нормализованная шкала 0..1). RecoverRate = `0.05/s` для всех (per-role вариации откладываем до Phase 15 doctrines).

**P3. Stamina depletion / regen rules.**

Из `GAMEDESIGN.md` §5 + уточнения:

| Pace | Speed mul | Stamina drain (per second) |
|---|---|---|
| Walk | 1.0 | 0.0 |
| Run | 1.6 | 0.10 |
| Sprint | 2.2 | 0.50 |

Stance speed (уже в UnitMovementSystem с Phase 7): Stand=1.0, Crouch=0.6, Prone=0.3 base. Финальная скорость = `BasePaceSpeed × StanceMul`. **Исключение**: Sprint+Prone структурно разрешён (4D-куб не запрещаем), но скорость 2.2 × 0.3 = 0.66 — игра не падает.

Recovery: при `Pace==Walk` AND `Stance ∈ {Stand, Crouch}` (Prone не восстанавливается — физическая модель). Rate = 0.05/s.

Auto-fallback: при `Stamina.Current == 0` → MovementProfile.Pace temporarily forced to Walk через **отдельный transient marker** `StaminaExhausted`. Восстановление до `Stamina.Current > 0.3` снимает marker, возвращает к Squad's MovementProfile.Pace. Это не меняет MovementProfile (игроку не нужно переключать обратно), это override per-unit на время исчерпания.

**P4. PathStyle implementation: `CoverDistance uint8` byte в NavCell, baked в SpatialBakeSystem.**

Фиксируется COMMAND-MODEL §10.7. Расширение `components.NavCell`:

```go
type NavCell struct {
    Cost          uint8  // existing
    Flags         uint8  // existing
    CoverDistance uint8  // новое: 0=на cover slot, 255=далеко (>=255 cells), линейная между
}
```

Дополнительный 1 байт × 1024 cells = +1 KB на чанк bake (~10% increase). Acceptable.

`SpatialBakeSystem.BakeNav` Pass дополняется: после генерации cover slots — для каждой cell вычисляется `min(distance to nearest cover slot in 9-chunk window)` и пишется в `CoverDistance` (clamped to 0..255 в cell-units).

`NavService.FindPath(start, end, opts)` где `opts NavOpts{PathStyle PathStyle}`. Cell cost modifier:

| PathStyle | Modifier |
|---|---|
| Direct | × 1.0 (no change) |
| RoadPrefer | road cells × 0.5, off-road × 1.5 |
| RoadAvoid | road cells × 2.0, off-road × 1.0, cover-tagged × 0.8 |
| CoverSeek | `cells where CoverDistance < 8` × 0.7, остальные × 1.0 |

CoverSeek threshold 8 cells (~8 м) — cover в радиусе. Tunable через константу.

**P5. `OrderParamMovementProfile` — опциональный компонент на Order, override squad default на длину Order'а.**

```go
// components/order_param_movement.go
type OrderParamMovementProfile struct {
    Profile MovementProfile  // полный override (все 4 поля)
}
```

UnitMovementSystem читает приоритет:
1. `StaminaExhausted` marker → forced Pace=Walk
2. Order has `OrderParamMovementProfile` → use that profile
3. Squad's `MovementProfile` → default

При `Order.State == Completed/Cancelled/Failed` → squad возвращается к своему default'у (просто потому что Order больше не InProgress). Это контракт без явного «revert» step'а.

Use cases: `Ctrl+RMB` → MoveTo с MovementProfile=Stealth preset. `Double-RMB` → MoveTo с Pace=Sprint override. После arrival — squad back to its setting.

**P6. Sneak удалён из `OrderKindCode` enum.**

В Phase 11 `OrderKindSneak` — отдельный kind. После Phase 13:
- `OrderKindSneak` удаляется из enum
- Resolver и pie menu больше не предлагают Sneak as Order
- `Ctrl+RMB` создаёт обычный `OrderKindMoveTo` + `OrderParamMovementProfile{Profile: PresetStealth}`
- Migration: тестовая сцена не использует Sneak Order'ы напрямую (Phase 11 был demo), безопасно удалить enum value

`SqWatch` в OrderResolverSystem'е и любых других местах, где переключение на Sneak — заменяется на «применить Stealth preset через OrderParamMovementProfile».

**P7. `RoleService.AssignRole` пишет UnitRole + Equipment как раньше; standing rules — на Squad через `SquadService.CreateFromTemplate` или explicit `applyTemplateDefaults`.**

`AssignRole(unit, role)` остаётся unit-level (Equipment + UnitRole). Standing rules **не** ставятся per-Unit (см. P1). Вместо этого:

`SquadService.CreateFromTemplate(template, ...)` после spawn'а юнитов делает шаг:
```go
// Aggregation: squad берёт defaults командира.
leaderRole := TemplateRoster(template)[0]  // slot 0 = Leader
movementProfile := MovementDefaultForRole(leaderRole)
engagementRules := EngagementDefaultForRole(leaderRole)
behaviorRules   := BehaviorDefaultForRole(leaderRole)

// Если template'у соответствует «специализированный» preset — переписываем:
switch template {
case TmplRecon:
    movementProfile.PathStyle = PathStyleCoverSeek
    movementProfile.Posture   = PostureQuiet
    engagementRules.Mode      = HoldFire
case TmplATTeam:
    engagementRules.Mode      = HoldFire
    engagementRules.FireOnInf = false
    engagementRules.FireOnArm = true
case TmplMGTeam:
    movementProfile.Stance    = Crouch
}

squadMap.Set(squad, MovementProfile{...})
squadMap.Set(squad, EngagementRules{...})
squadMap.Set(squad, BehaviorRules{...})
```

Per-role `MovementDefaultForRole` / `EngagementDefaultForRole` / `BehaviorDefaultForRole` lookup'ы — функции в `systems/role_service.go`, заполненные по таблице из COMMAND-MODEL §4.

**P8. Idempotency: refresh standing rules только если их ещё нет.**

Если на Squad уже есть `MovementProfile` (player edited через Inspector) и player делает что-то, что могло бы trigger'нуть re-apply (например, hot-reload template'а — гипотетически) — **не overwrite'им**. RoleService и SquadService проверяют `Has[T]` перед `Set[T]`. Это предотвращает неожиданный сброс player-edited настроек при role-change.

**P9. Stance hierarchy в UnitMovementSystem.**

Per-tick UnitMovementSystem читает приоритет stance'а:

1. Текущая Action в queue имеет `Kind=ActionKindStance` → применяется немедленно (как сейчас в Phase 7)
2. Squad's `MovementProfile.Stance` → если Unit.Stance != MovementProfile.Stance, transition (мгновенно для Phase 13, smooth animation — Phase 25)
3. Иначе — Unit.Stance остаётся как есть

Phase 13 не вводит transition cost (мгновенная смена). Phase 25 добавит time penalty (стоя→прон 0.5s, прон→стоя 0.3s).

Player hotkey Z/X/C → меняет MovementProfile.Stance squad'а (не Action) → все юниты transition. Это совпадает с tradition'ом RTS «Z=stand, X=crouch, C=prone применяется ко всему отряду».

**P10. Hotkey mapping (lock'нем при build'е UI в M13.6/M13.7).**

Pre-allocation:
- `[` / `]` — prev/next MovementProfile preset
- `Z` / `X` / `C` — set Squad.MovementProfile.Stance to Stand / Crouch / Prone
- `'` (quote) — toggle Posture Standard ↔ Quiet
- `Ctrl+RMB` — MoveTo + OrderParamMovementProfile=Stealth preset
- `Double-RMB` (window 300ms) — MoveTo + Pace=Sprint override
- `Alt+RMB` — Force AttackMove (existing in §4 GAMEDESIGN, scaffolded for Phase 14)

`Z/X/C` коллизии с другими hotkey'ями — проверим в M13.7. Если Z уже занят (сейчас не занят, верифицировать в коде) — fallback на отдельный stance modifier.

**P11. Inspector quick-bar layout (Single-squad view).**

Три новые секции в Inspector single-squad view, ниже существующего roster section:

```
┌─ Movement ─────────────────────────────┐
│ Presets: [Default][Cautious][Rush]     │
│          [Sprint][Stealth][Crawl]      │
│ Pace:    [Walk ▼]  Stance:  [Stand ▼]  │
│ Posture: [Std  ▼]  PathStyle:[Direct▼] │
│ Stamina avg: [████████░░] 85%          │
└────────────────────────────────────────┘

┌─ Engagement ───────────────────────────┐
│ Mode: [Hold] [Return] [Free]           │
│ Targets: [✓]Inf [✓]Arm [ ]Air [ ]Struct│
└────────────────────────────────────────┘

┌─ Behavior ─────────────────────────────┐
│ [✓] Allow auto-reposition under fire   │
│ [✓] Allow auto-stance change           │
│ [ ] Hold until ordered                 │
│ [✓] Allow return fire                  │
│ Suppression threshold: [────●──] 0.30  │
└────────────────────────────────────────┘
```

Ширина — Inspector panel width (15% screen в Field preset). 6 preset chips компонуются в 2 ряда по 3. Dropdowns стандартные raygui-style (или immediate-mode эквивалент в нашем UI стеке). Toggle chips — square check + label. Slider для SuppressionThreshold — single bar.

Section headers — bold labels. Цвет accent = neutral grey.

**P12. Per-unit Stamina visualization.**

В 3D-render: тонкий горизонтальный bar над unit's role-cap, отображается **только когда `Stamina.Current < 0.8 × MaxLevel`**. Ширина bar = 0.5 м world-space. Высота 2 px. Цвет:
- Green: `Current/Max ≥ 0.5`
- Yellow: `0.2 ≤ Current/Max < 0.5`
- Red: `Current/Max < 0.2`

Реализация — 2D screen-projected (как existing role labels из Phase 12). Перформанс: 12 юнитов × 1 simple rect = ~negligible.

В Inspector single-unit view: строка `Stamina: 0.45 / 1.0` с inline mini-bar.

**P13. BehaviorRules — scaffold-only в Phase 13.**

Поля заполняются через RoleService и редактируются через Inspector toggle'и, но **никто их не читает в Phase 13**. SurvivalInstinctSystem (Phase 15) и ScatterProtocol (Phase 15) — реальные читатели. Это формальный contract:

```
Phase 13 writes:  BehaviorRules.HoldUntilOrdered, AllowAutoReposition, AllowAutoStance, AllowReturnFire, SuppressionThreshold
Phase 14 writes:  Suppression.Level (от попаданий)
Phase 15 reads:   все вышеперечисленное; ставит/снимает TacticalOverride marker
Phase 13 readers: НЕТ
```

Это не bug — это deliberate scaffold по паттерну Phase 9 (Suppression поле было пустым до Phase 14).

---

## Семь мильстоунов

### M13.1 — Standing-rule компоненты + Stamina (data + helpers)

**Цель.** Существуют четыре новых компонента: `MovementProfile`, `EngagementRules`, `BehaviorRules`, `Stamina`. Связанные enum'ы (`Pace`, `Posture`, `PathStyle`, `EngagementMode`) и preset helpers (`MovementPreset`, `ApplyPreset`). На сцене ничего не меняется — компоненты не добавляются ни к одной entity (это M13.2 делает через RoleService/SquadService).

**Делаем:**
- `components/movement_profile.go`: `MovementProfile` struct, `Pace` enum (Walk/Run/Sprint), `Posture` enum (Standard/Quiet), `PathStyle` enum (Direct/RoadPrefer/RoadAvoid/CoverSeek). `MovementPreset` enum (Default/Cautious/Rush/Sprint/Stealth/ProneCrawl), `ApplyPreset(p MovementPreset) MovementProfile` lookup table.
- `components/engagement_rules.go`: `EngagementRules` struct (Mode + 4 target bools + Standoff/SectorYaw/SectorHalfDot — последние три зарезервированы Phase 14, заполняются нулями). `EngagementMode` enum (HoldFire/ReturnFire/FreeFire). `StandoffPolicy` enum scaffold (Close/Med/Long/Any).
- `components/behavior_rules.go`: `BehaviorRules` struct (4 bools + SuppressionThreshold float32).
- `components/stamina.go`: `Stamina` struct (Current/MaxLevel/RecoverRate). Marker `StaminaExhausted struct{}` для transient override.
- `components/order_param_movement.go`: `OrderParamMovementProfile{Profile MovementProfile}` опциональный компонент на Order entity.

**Проверяем.** `go build` чист. Smoke checks: `ApplyPreset(PresetStealth).Pace == Walk`, `ApplyPreset(PresetStealth).Posture == Quiet`. Все ECS-component'ы register'ятся в `ecs.NewMap[T]` без panic.

### M13.2 — Per-role defaults в RoleService + Squad aggregation в SquadService

**Цель.** `RoleService` имеет lookup'ы `MovementDefaultForRole` / `EngagementDefaultForRole` / `BehaviorDefaultForRole` по таблице из COMMAND-MODEL §4. `RoleService.AssignRole` устанавливает `Stamina` per-Unit с per-role MaxLevel modifier. `SquadService.CreateFromTemplate` после spawn'а ростера применяет squad-level defaults агрегацией от leader'а + template-specific override (Recon/AT/MG специализации). Idempotent: не overwrite'ит уже существующие компоненты.

**Делаем:**
- `systems/role_service.go`: новые helpers `MovementDefaultForRole(role) MovementProfile`, `EngagementDefaultForRole(role) EngagementRules`, `BehaviorDefaultForRole(role) BehaviorRules`, `StaminaMaxForRole(role) float32`. Реализации по P2 + COMMAND-MODEL §4.
- `systems/role_service.go::AssignRole`: после spawn'а Equipment — добавляет `Stamina` component на unit с правильным MaxLevel + RecoverRate=0.05.
- `systems/squad_service.go::CreateFromTemplate`: после spawn'а ростера + AssignRole каждому → агрегация defaults от leader (slot 0) → template-specific override (switch по template per P7) → `squadMap.Set(squad, MovementProfile{...})`, аналогично EngagementRules + BehaviorRules. Idempotency: проверять `Has[T]` перед `Set[T]`.
- `systems/squad_service.go`: SquadService держит `movementProfileMap`, `engagementRulesMap`, `behaviorRulesMap`, `staminaMap` handles.

**Проверяем.** Сцена при старте: 3 starter squads (Phase 12 — Light Infantry / MG Team / AT Team) каждый имеет `MovementProfile`, `EngagementRules`, `BehaviorRules` на squad-entity. Light Infantry: `Mode=FreeFire, Stance=Stand` (leader default). MG Team: `Stance=Crouch` (template override). AT Team: `Mode=HoldFire, FireOnArm=true, FireOnInf=false`. Каждый юнит имеет `Stamina{Current=Max, MaxLevel=role-mul × 1.0}`. MG юнит: `MaxLevel=0.7`. Можно verify через `Ctrl+P` snapshot или debug print.

### M13.3 — UnitMovementSystem reads MovementProfile + Stamina

**Цель.** UnitMovementSystem применяет Pace × Stance speed multiplier из MovementProfile (или OrderParamMovementProfile override). Stamina истощается при Run/Sprint, восстанавливается при Walk+Stand/Crouch. При `Stamina.Current == 0` → ставится `StaminaExhausted` marker → Pace forced to Walk → восстановление → marker снимается. Stance hierarchy per P9.

**Делаем:**
- `systems/unit_movement.go`: extend per-unit update step:
  1. Вычислить effective MovementProfile: `OrderParamMovementProfile` (если есть на текущем Order) → squad's MovementProfile → defaults.
  2. Если есть `StaminaExhausted` marker → forced Pace=Walk.
  3. effective speed = `BasePaceSpeed[Pace] × StanceMul[Stance]`.
  4. Update Stamina: drain by `StaminaDrain[Pace] × dt`, regen if Walk+Stand/Crouch.
  5. Add/remove `StaminaExhausted` marker через deferred mutation buffer (Ark forbids in-loop archetype changes).
  6. Stance auto-transition: если `Unit.Stance != effective.Stance` AND нет current Action{Kind=Stance} → mutate Unit.Stance к target (deferred).
- `systems/unit_movement.go`: добавить snapshot буферы для StaminaExhausted add/remove (per pattern из Phase 11.6).

**Проверяем.** Spawn squad, переключить Pace на Sprint через debug-key (или временно через Inspector toggle если M13.6 уже частично готов) → Stamina всех юнитов начинает падать. Через ~2с (1.0 / 0.5/s) → Stamina=0 → юниты замедляются до Walk speed. Через ~6с (0.3 / 0.05/s) restore'ится → возврат к Sprint.

### M13.4 — NavService PathStyle + NavGrid CoverDistance bake

**Цель.** SpatialBakeSystem генерирует `CoverDistance` byte для каждой `NavCell` (расстояние до ближайшего cover slot в 9-chunk window, clamped 0..255). NavService.FindPath принимает `NavOpts{PathStyle}`, применяет cell cost modifier по таблице из P4. SquadMacroPathSystem передаёт squad'овский PathStyle в FindPath.

**Делаем:**
- `components/nav.go` (или где сейчас NavCell): добавить `CoverDistance uint8` поле в `NavCell`.
- `systems/spatial_bake.go::BakeNav` (или соответствующий Pass): после baking cover slots — для каждой cell в 9-chunk window вычислить `min(distance to nearest CoverSlot.Pos)` в cell-units, write to `NavCell.CoverDistance` (clamped to 255). Brute-force O(cells × slots) — для 32×32 cells × ~50 slots = 50K ops per chunk, acceptable.
- `systems/nav_service.go::NavOpts`: добавить `PathStyle components.PathStyle` поле.
- `systems/nav_service.go::FindPath`: при computeCellCost — multiply by PathStyle modifier. Road tag check (existing road-bias already in Phase 6) → переиспользуем; новое — CoverDistance check для CoverSeek (`CoverDistance < 8 → × 0.7`).
- `systems/squad_macro_path.go`: при вызове FindPath — передавать `NavOpts{PathStyle: squad.MovementProfile.PathStyle}`.

**Проверяем.** На test scene: squad ставит MoveTo через карту с дорогой и зданиями. PathStyle=Direct → прямой путь через поле. PathStyle=RoadPrefer → детур через дорогу. PathStyle=RoadAvoid → обход дороги через лес. PathStyle=CoverSeek → путь зигзагом через cover-tagged местность. Hold-`N` debug overlay (existing) показывает NavGrid cell costs — color-coded.

### M13.5 — `OrderParamMovementProfile` + Order modifiers + Sneak removal

**Цель.** `Ctrl+RMB` создаёт MoveTo с Stealth preset через `OrderParamMovementProfile`. `Double-RMB` (window 300ms) → MoveTo с Sprint preset. `Alt+RMB` scaffolds AttackMove (placeholder, Phase 14 даёт реальный behavior). `OrderKindSneak` удалён из enum. Resolver и pie menu обновлены.

**Делаем:**
- `components/order.go` (или где OrderKindCode enum): удалить `OrderKindSneak` из enum. Compile errors → patches в resolver / pie / UI.
- `systems/order_resolver.go::resolveTargetIntoOrder`: ничего не меняет для Sneak (он больше не выбирается resolver'ом).
- main.go или input handler: при detection `Ctrl+RMB` → spawn Order с kind=MoveTo + add `OrderParamMovementProfile{Profile: ApplyPreset(PresetStealth)}`. При `Double-RMB` (LastRMBClickTime - now < 300ms) → spawn Order с kind=MoveTo + add `OrderParamMovementProfile{Profile: ApplyPreset(PresetSprint)}`.
- `ui/pie_menu.go` (если существует) или resolver: убрать Sneak segment, добавить Sprint / Crawl как modifier'ы (UI решение в M13.6).

**Проверяем.** Squad selected → `Ctrl+RMB` на точке → spawn MoveTo Order с `OrderParamMovementProfile{Pace=Walk, Stance=Crouch, Posture=Quiet, PathStyle=RoadAvoid}` + `EngagementRules.Mode=HoldFire` (последнее — pending? см. open question 1). UnitMovementSystem (от M13.3) читает override → squad движется в crouch + walk speed. После arrival (Order=Completed) → возврат к squad's standing MovementProfile (Stand+Walk+Direct). `Double-RMB` → temporary Sprint, после arrival → fallback. Build чист, нет references на deleted `OrderKindSneak`.

### M13.6 — Inspector quick-bars (Movement / Engagement / Behavior)

**Цель.** Inspector single-squad view имеет три новые секции под roster (см. P11 layout). Player может clicking по preset chips / dropdowns / toggle'ям менять Squad's standing rules в реальном времени. UnitMovementSystem (M13.3) and FindPath (M13.4) сразу видят новые значения.

**Делаем:**
- `ui/inspector.go::drawSingleSquadView`: после `drawRosterSection` — три новые helpers `drawMovementSection`, `drawEngagementSection`, `drawBehaviorSection`.
- `drawMovementSection`: 6 preset chips (rows: 3+3) + 4 labelled dropdowns. Click chip → `squad.MovementProfile = ApplyPreset(p)`. Dropdown change → mutate single field. Stamina avg (mean Current/MaxLevel of roster) — bar.
- `drawEngagementSection`: 3-button row (Hold/Return/Free) — radio behavior. 4 toggle chips (Inf/Arm/Air/Struct).
- `drawBehaviorSection`: 4 toggle rows + slider for SuppressionThreshold (0..1, step 0.05).
- Inspector mutate path: writes to squad's `MovementProfile` / `EngagementRules` / `BehaviorRules` — same handles SquadService использует.
- Multi-select case: если selectedSet contains > 1 squad → отображать «—» в полях (mixed values), click устанавливает на все selected squads (bulk operation).

**Проверяем.** Click на squad → Inspector раскрывается с новыми секциями. Click `Stealth` chip → MovementProfile меняется → squad начинает следующий MoveTo в crouch+walk+road-avoid. Click `Hold Fire` → EngagementRules.Mode=HoldFire (visual change в UI; behavioural reader появится в Phase 14). Toggle `Hold until ordered` → BehaviorRules.HoldUntilOrdered=true (scaffold-only). Multi-select 2 squads → mixed-value indicator → click `Default` preset → both apply.

### M13.7 — Hotkey shortcuts + Stamina visualization + closure

**Цель.** Hotkey'и `[`, `]` cycles MovementProfile presets. `Z/X/C` устанавливают Stance squad'а. `'` toggles Posture. Per-Unit Stamina bar над cap'ом отображается при Stamina < 80%. Test scene и smoke check'и завершены, ROADMAP update, Phase 13 → ✅.

**Делаем:**
- main.go input handling: добавить hotkey routing для `[` / `]` / `Z` / `X` / `C` / `'` (только при focus = 3D или Map panel; при focus = Inspector — не перехватываем, иначе `[` будет ломать input field'ы будущих textboxes).
- `[` / `]`: cycle через ApplyPreset enum для всех selected squads.
- `Z` / `X` / `C`: set MovementProfile.Stance = Stand/Crouch/Prone для всех selected squads.
- `'`: toggle Posture для всех selected squads.
- `render_world.go::drawUnitStamina` (новая helper): screen-projected mini-bar над unit's role-cap. Color по P12 ranges. Только когда `Current < 0.8 × MaxLevel`.
- `ui/inspector.go::drawSingleUnitView`: добавить Stamina row с inline bar.
- ISSUES.md проверка: ничего нового не закрывается, но verify что новые baselines (Tab лаг от расширенного Inspector) не worse — на 12 юнитах render-overhead negligible.
- README ROADMAP: Phase 13 → ✅.

**Проверяем.** Select squad → press `]` → preset cycles to next (visual change in Inspector + behavior change). Press `Z` → all squad members → Stand. Press `'` → Posture toggles. Sprint squad → Stamina bars появляются над юнитами (yellow → red). Walk → bars скрываются после восстановления. `Ctrl+P` snapshot — counts: 3 squads, 12 units, +3 MovementProfile, +3 EngagementRules, +3 BehaviorRules, +12 Stamina.

---

## Что считаем «закрытием Phase 13»

- 4 новых компонента: `MovementProfile`, `EngagementRules`, `BehaviorRules`, `Stamina` + `OrderParamMovementProfile` опциональный + `StaminaExhausted` marker.
- Новые enum'ы: `Pace` (Walk/Run/Sprint), `Posture` (Standard/Quiet), `PathStyle` (Direct/RoadPrefer/RoadAvoid/CoverSeek), `EngagementMode` (HoldFire/ReturnFire/FreeFire), `MovementPreset` (Default/Cautious/Rush/Sprint/Stealth/ProneCrawl). `StandoffPolicy` enum scaffold.
- `OrderKindSneak` удалён из `OrderKindCode` enum. Все references мигрированы на MoveTo + OrderParamMovementProfile.
- `RoleService` имеет per-role default lookup'ы (Movement / Engagement / Behavior / StaminaMax). Spawn'ит Stamina per-Unit с правильным MaxLevel.
- `SquadService.CreateFromTemplate` агрегирует squad-level defaults от leader + template-specific override.
- `NavCell.CoverDistance uint8` поле + bake в SpatialBakeSystem. NavService.FindPath читает PathStyle через NavOpts.
- UnitMovementSystem читает Pace × Stance из MovementProfile (или OrderParamMovementProfile override). Stamina depletion / regen / auto-fallback к Walk при exhaustion.
- Inspector single-squad view имеет 3 новые секции (Movement / Engagement / Behavior) с preset chips, dropdowns, toggle'ями, slider. Multi-select поддерживается с mixed-value indicator.
- 3 starter squads имеют визуально разные default standing rules в Inspector (LightInfantry / MGTeam / ATTeam — каждый со своим Mode / Stance / target preference).
- Hotkey'и: `[` / `]` (cycle preset), `Z` / `X` / `C` (Stance), `'` (Posture toggle). `Ctrl+RMB` (Sneak preset MoveTo), `Double-RMB` (Sprint MoveTo), `Alt+RMB` (AttackMove placeholder).
- Per-Unit Stamina bar в 3D при Current < 80%. Inspector single-unit view — Stamina row.
- Pipeline: добавляется чтение MovementProfile в UnitMovementSystem; SpatialBakeSystem bake'ит CoverDistance дополнительно. `... → spatial_bake (+CoverDist) → terrain_mesh → ground_stick → unit_movement (reads MovementProfile + Stamina) → vision → order_resolver → squad_macro_path (FindPath with PathStyle) → formation → ...`

После этого — обновление ROADMAP, Phase 13 → ✅, переход к **Phase 13.5 (Ghost preview & facing-drag)**.

---

## Заметки на полях

- **`OrderParamMovementProfile` overrides только movement, не EngagementRules.** Stealth preset по описанию также включает `RoE=HoldFire`, но это standing rule, не movement. Решение: при `Ctrl+RMB` создаётся OrderParamMovementProfile (movement override) **и опционально** OrderParamEngagementOverride (если решим добавить в M13.5). Compromise: Phase 13 ставит только movement override; HoldFire-during-stealth — это TODO, finalize в open question 1. Если решим, что Stealth preset должен включать temporary HoldFire — добавим `OrderParamEngagementOverride` симметрично.

- **Stance hotkeys Z/X/C — verify что не заняты.** В CLAUDE.md контролы: WASD движение, Shift sprint anchor, RMB orbit, X drops crater, G/N/C/V/F/Y debug overlays (hold). **`X` уже занят crater, `C` уже занят CoverMap overlay**. Конфликт. Решение: Stance hotkey'и — `Shift+1/2/3` или `Tab+Z/X/C` или другая комбинация. Финализируется в M13.7 при build'е input routing.

- **Baking CoverDistance брутфорсом — ок для Phase 13.** 32×32 cells × 50 cover slots × 9 chunks = ~460K ops per bake event. SpatialBakeSystem уже не быстрый (P11.5 / 11.6 параллелит). На фоне cover map gen — приемлемое расширение. Если профайл покажет hot — Phase 14+ оптимизация через KD-tree / spatial hash для cover slots.

- **Stamina avg в Inspector Movement section — sum/count ad-hoc.** На 12-unit squad — копейки. На 30-unit (Phase 19+) — всё ещё ок. Если станет hot — кеш в SquadService.

- **AttackMove (Alt+RMB) — scaffold в Phase 13, реальное поведение в Phase 14.** Спавнится Order с `OrderKind=MoveTo` + флаг `OrderParamAttackMove{}` (новый component). Phase 14 WeaponSystem проверяет наличие флага → разрешает огонь по visible enemy без cancel'а MoveTo. Phase 13 просто spawn'ит Order с флагом, никто не читает — visual marker может отображать «AT» в Inspector queued orders.

- **Double-RMB detection — простое timing.** `LastRMBClickTime` field в input state. При RMB-press: если `now - LastRMBClickTime < 300ms` → это double-click → Sprint. Иначе normal click. `LastRMBClickTime = now` всегда. Подводный камень: triple-RMB будет ловиться как «double + single», нужно reset'ить timestamp после double-detection.

- **Idempotency P8 — strict semantics.** При reassign role или re-create from template — RoleService.AssignRole **не** трогает Stamina (сохраняет current value). Это позволяет hot-swap ролей в playthrough без сброса fatigue. SquadService — не трогает MovementProfile / EngagementRules / BehaviorRules если уже есть.

- **CoverSeek threshold (8 cells).** Tunable константа. На playtest'е может оказаться, что 8 cells слишком короткое окно (юниты дёргаются между cover'ами) или слишком длинное (нет реального preference). Финализируется empirically; ставлю 8 как стартовую точку.

- **Posture без эффекта в Phase 13.** Field присутствует в MovementProfile, переключается в Inspector / hotkey, но никто не читает (audio detection — Phase 15). Для тестирования: Inspector отображает текущее значение, чтобы видеть что toggle работает. Phase 15 наполнит.

- **Multi-select bulk operations.** Inspector с N squads selected → preset chip click применяется ко всем. Mixed-value (squad'ы с разным Pace) — отображать «—» в dropdown'е, заменять на выбранное при clicking. Это standard Blender / DAW UX.

- **Stance auto-transition в Phase 13 — мгновенная.** Если MovementProfile.Stance меняется со Stand на Prone — Unit.Stance меняется в один tick. Анимация (smooth lerp) + time-cost (стоя→прон 0.5s) — Phase 25 (Polish). Сейчас юнит резко «падает» — visually-jarring но functional.

- **`Suppression.Level` в BehaviorRules.SuppressionThreshold compare.** Phase 13 не записывает Suppression.Level (Phase 14 — WeaponSystem пишет). Threshold существует как scaffold для Phase 15 — играть его playtest'ом нельзя до Phase 14. Разумный default 0.3 — empirical guess, refined Phase 15.

- **Phase 13.5 (Ghost preview) — не блокирует Phase 14.** Можно начинать Combat фазу параллельно или сразу после Phase 13. 13.5 — это independent UX polish.

- **`hasRadiomanInRoster` — без изменений с Phase 12.** Standing rules не пересекаются с radio system. Phase 20 (Comms) активирует action masking на основе RadioNetwork.HQReachable.

---

## Открытые вопросы (требуют решения по ходу M13.x)

1. **Stealth preset включает RoE=HoldFire или нет?** Если да — нужен также `OrderParamEngagementOverride` компонент симметрично OrderParamMovementProfile. Это удваивает scope override-механики. Альтернатива: Stealth preset ограничен movement (Walk+Crouch+Quiet+RoadAvoid), HoldFire — отдельная toggle игроком. Recommendation: ограничить Phase 13 movement-only, HoldFire через explicit Inspector toggle. Финализируем при build'е M13.5.

2. **AttackMove flag reading — нужен в Phase 13?** Phase 14 WeaponSystem прочитает флаг. Phase 13 spawn'ит, но не читает. Visual marker в queued orders Inspector — да или нет? Recommendation: показывать «AT» в Inspector chip для AttackMove orders. Финализируем M13.5.

3. **SquadService aggregation от leader — что если leader погиб mid-game?** Squad'у нужен «promotion» mechanic (slot 1 становится новым leader)? Phase 13 не обрабатывает death (Combat = Phase 14). Recommendation: defer полностью до Phase 14/15, в Phase 13 squad's MovementProfile/etc живут независимо от leader survival.

4. **Inspector mixed-value display.** Если 2 selected squads имеют разные `EngagementRules.Mode` — как отобразить radio button row? Recommendation: 3 buttons показываются неактивными (полупрозрачными), click на любую → assigns to all. Финализируем M13.6.

5. **Stamina recovery while crouched — yes or no?** P3 говорит: regen при Walk + (Stand OR Crouch). Реализм: crouch не отдых, но и не нагрузка. Recommendation: разрешить (Walk+Crouch OK для regen), запретить только Prone. Финализируем M13.3 при тесте.

6. **CoverSeek vs road-bias коллизия.** Если cell — road AND есть cover рядом — какой modifier применяется? Текущий P4: PathStyle определяет один доминирующий profile, modifier'ы не комбинируются. RoadPrefer × CoverSeek не коэкзистируют (выбирается один PathStyle). Это намеренно или ограничение? Recommendation: считаем feature, не bug — PathStyle — single mode. Если игроки попросят compound styles — Phase 21+. Финализируем M13.4.

7. **Hotkey conflicts** (P10 + заметка про Z/X/C). Финализируем mapping в M13.7 после проверки existing input routing.
