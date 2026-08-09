# Phase 12 — рабочий план

Unit roles: каждый юнит имеет роль из 10 базовых (Leader / Rifleman / MachineGunner / Grenadier / Sniper / ATGunner / Medic / RadioOperator / Engineer / DemoMan). Роль определяет дефолтное Equipment (placeholder weapons / gear), визуальный маркер (цвет, иконка), задел для будущей AI и combat-логики. Squad templates для удобного спавна типичных формаций (Motor Rifle / NATO Infantry / Recon / Engineering / AT Team). Inspector показывает роли в составе squad'а, map показывает role-иконку командира.

**Что в Phase 12 сознательно НЕТ.** Per-role AI behaviors (MG выбирает позицию, Medic ходит к раненому, Engineer строит, RadioOp gate'ит связь) — Phase 14 / 15 / 18 / 20. Per-role weapon logic (RoF, range, accuracy, ammo limits) — Phase 14 Combat. Lootable equipment / pickup-on-death — Phase 25 Polish. Per-role animations / model differentiation — Phase 25. RoE per-role override (sniper hold fire по умолчанию) — Phase 13 (Movement & Engagement). UI палитра role-icons custom'изация — Phase 21 (UI expansion). Drag-and-drop ролей в Inspector'е — Phase 21. Per-role stamina / load weight (MG медленнее) — Phase 13.

ROADMAP — высокоуровневый трекер. Этот файл — рабочий, обновляется по ходу выполнения.

---

## Решения, которые лочим до начала кода

**P1. `UnitRole` — component на юните, не на squad'е.**

Роль принадлежит юниту независимо от его текущего squad'а. Если юнит вышел из squad'а (соло-state), он сохраняет роль. Когда squad merge/split, роли каждого юнита остаются.

```go
// components/role.go
type UnitRole struct {
    Kind UnitRoleKind
}

type UnitRoleKind uint8
const (
    RoleRifleman UnitRoleKind = iota  // дефолт; первое значение для zero-value safety
    RoleLeader
    RoleMachineGunner
    RoleGrenadier
    RoleSniper
    RoleATGunner
    RoleMedic
    RoleRadioOperator
    RoleEngineer
    RoleDemoMan
)

// String — для Inspector / log.
func (k UnitRoleKind) String() string { ... }

// ShortLabel — для map-иконки командира. 1-2 character'а.
// L/R/MG/GL/SN/AT/MD/RO/EN/DM.
func (k UnitRoleKind) ShortLabel() string { ... }
```

**Дефолт = `RoleRifleman`** (значение 0 для zero-value safety: юнит без явного UnitRole component'а ведёт себя как Rifleman; но на практике Phase 12 кладёт UnitRole component при спавне).

**P2. `SquadTemplate` — helper для спавна типичных формаций, не component.**

Это **convenience**, не runtime concept. `SquadTemplate` — это рецепт «спавни 8 юнитов с такими-то ролями». Никаких component'ов squad'а с template не остаётся (Phase 12 — потому что мы не используем template для AI; будущая Phase 13/15 может ввести `SquadDoctrine` который пересечётся, но это другая сущность).

```go
// systems/squad_template.go
type SquadTemplate uint8
const (
    TmplLightInfantry SquadTemplate = iota  // Leader + 3 Rifleman (4 unit'а)
    TmplMotorRifle                          // Leader + 4 Rifleman + MG + Grenadier + Medic (8)
    TmplNATOInfantry                        // Leader + 4 Rifleman + MG + Grenadier + RadioOp (8)
    TmplRecon                               // Leader + Sniper + 2 Rifleman + RadioOp (5)
    TmplEngineering                         // Leader + 2 Engineer + DemoMan + 2 Rifleman (6)
    TmplATTeam                              // Leader + ATGunner + 2 Rifleman (4)
    TmplMGTeam                              // Leader + MG + 2 Rifleman (4)
)

// TemplateRoster возвращает список ролей для template'а. Длина = size of squad.
// Slot 0 всегда командир, остальные — в порядке tactical-priority.
func TemplateRoster(t SquadTemplate) []components.UnitRoleKind
```

Test scene Phase 12: 3 squad'а из 4 юнитов разных templates (LightInfantry / MGTeam / ATTeam) — все 4-юнитные, влезают в существующий cluster layout. Видны 4 разные роли (Leader / Rifleman / MG / AT) сразу при старте.

**P3. Equipment-сущности расширяются под per-role default loadout.**

Текущее (Phase 7+): `Weapon{Kind: WeaponAK47, Ammo, RangeM, RoF, Damage}`. `WeaponKind` enum имеет только `WeaponAK47`.

Phase 12 расширение: добавляем placeholder kinds для каждой роли. Без реальной combat-логики (это Phase 14), просто struct'ы с разумными числами.

```go
// components/weapon.go
const (
    WeaponAK47       WeaponKind = iota  // default Rifleman
    WeaponPKM                           // MG
    WeaponSVD                           // Sniper
    WeaponRPG7                          // ATGunner / Grenadier (AT)
    WeaponGP25                          // Grenadier (under-barrel)
    WeaponMakarov                       // Leader / Radio / Medic / etc. — secondary
)
```

Дополнительные `Equipment` slot'ы: `Secondary` уже есть в `Equipment{Primary, Secondary, Active}`. Используем для радиста (Secondary = Radio entity), медика (Secondary = Medkit), сапёра (Secondary = Spade), etc.

Phase 12 спавнит соответствующие entity'и при unit'-creation'е через `RoleService.LoadoutFor(role)`:

```go
// systems/role_service.go
type RoleService struct {
    weaponMap   *ecs.Map[components.Weapon]
    radioMap    *ecs.Map[components.Radio]
    medkitMap   *ecs.Map[components.Medkit]
    spadeMap    *ecs.Map[components.Spade]
    ownedByMap  *ecs.Map[components.OwnedBy]
    posMap      *ecs.Map[components.WorldPos]
    equipmentMap *ecs.Map[components.Equipment]
}

// AssignRole — после того как юнит спавнен (с UnitRole component'ом),
// создаёт его Equipment-сущности (Primary weapon + Secondary gear) и
// прописывает в Equipment component юнита. Idempotent: если у юнита уже
// есть Primary, заменяет (старый entity destroy'ится).
func (s *RoleService) AssignRole(unit ecs.Entity, role components.UnitRoleKind)
```

**Новые компоненты** (placeholder marker'ы, без поведения в Phase 12):
- `components.Radio{}` — отдельная entity при `Equipment.Secondary` для радиста. Будет читаться Phase 20 (Comms) для `RadioNetwork.HasRadioman`.
- `components.Medkit{}` — для медика. Phase 14 / 25 (heal mechanic).
- `components.Spade{}` — для сапёра. Phase 18 (Engineering build orders).

`hasRadiomanInRoster` в `systems/squad_service.go` (сейчас placeholder возвращает false) — после Phase 12 проверяет реально: бежит по units, читает Equipment.Secondary, проверяет наличие Radio component'а. **Phase 20 будет использовать это для action masking** — Phase 12 включает функцию, но никто из Phase 12 систем её ещё не читает.

**P4. Visual differentiation: cap color на юнит-cube'е + role-letter поверх.**

В `render_world.go::drawUnitCube`:
- Текущая логика: `rl.Color{R:80, G:95, B:55, A:255}` — оливковый куб.
- Phase 12: cube остаётся оливковым (одинаковая униформа). **На верх куба добавляем "cap" — маленький под-куб сверху**, цвет = `roleColor(unit.Role.Kind)`. Палитра 10 цветов deterministic.
- Дополнительно над cube'ом (на высоте +2 м над unit'ом) — **single-letter label** через `rl.DrawText3D` или billboarded 2D-text (рисуется в 2D-overlay фазе после EndMode3D, screen-projected from unit world pos).

Палитра role-цветов:

| Role | Cap color | ShortLabel |
|---|---|---|
| Leader | bright yellow (255, 220, 60) | L |
| Rifleman | olive (80, 95, 55) — same as body | R |
| MachineGunner | red (200, 50, 50) | MG |
| Grenadier | orange (230, 130, 50) | GL |
| Sniper | dark green (40, 100, 40) | SN |
| ATGunner | dark red (140, 30, 30) | AT |
| Medic | white (240, 240, 240) | MD |
| RadioOperator | blue (60, 100, 200) | RO |
| Engineer | brown (130, 100, 50) | EN |
| DemoMan | grey (100, 100, 100) | DM |

Опционально (если время): для команди (slot 0 of his squad) cap делать **чуть выше** (~0.3 м вместо 0.15) — командирский «беретик» как visual hierarchy.

**P5. Inspector role display.**

Текущий Inspector (Phase 10/11): roster показывается как «slot 0 entity ID, slot 1 entity ID, ...». Phase 12: каждая строка ростера = `[ShortLabel] entity_short_id` с background-цветом = role-color. Это даёт «pixel-art roster panel» — Leader выделен жёлтым, MG красным, etc.

Single-unit selection: вверху Inspector'а строка `Role: Leader` крупным шрифтом + role-color tint.

**Hover-sync** (already in Phase 10): hover-юнит подсвечивается в Inspector'е соответствующей строкой — теперь видно с role-меткой.

**P6. Map commander marker shows role icon.**

В `ui/map_render.go::drawSquadMarkers` сейчас рисуем круг squad-color'ом + сам круг указывает на «позицию squad'а». Phase 12 — над/рядом с кругом рисуем **role-icon командира** (slot 0). Два варианта:
- (a) Маленький symbol (3-4 px) NATO-style: круг = squad, треугольник внутри = infantry, ATGunner = anchor symbol, MG = crossed lines, etc. Дорого, нужен sprite-sheet или path-draw.
- (b) ShortLabel (1-2 character'а) внутри круга вместо bullet'а. Тривиально через `rl.DrawTextEx`.

Phase 12 — **(b)**. Внутри squad-color круга центровать ShortLabel белым / чёрным (контрастно к squad-color'у). Это даёт визуальную отличимость squad'а сразу: «синий с MG» vs «красный с Leader L».

**P7. SquadService API: `CreateFromUnits` остаётся; добавляется `CreateFromTemplate`.**

```go
// systems/squad_service.go (extension)

// CreateFromTemplate — high-level helper: спавнит N юнитов по template'у с
// правильными ролями + Equipment, создаёт Squad. Возвращает Squad-сущность.
// pos — позиция центра spawn'а; юниты разбрасываются на 2-метровой решётке.
//
// Wrapper над unit spawn (через RoleService) + CreateFromUnits.
func (s *SquadService) CreateFromTemplate(
    template SquadTemplate,
    pos components.WorldPos,
    formation components.FormationKind,
    roleService *RoleService,
    posMap *ecs.Map[components.WorldPos],
    unitFactory func(pos components.WorldPos) ecs.Entity,  // — main.go-controlled unit-create helper
) ecs.Entity
```

`unitFactory` callback — потому что unit spawn в main.go (a lot of components: `unitMap.Add(...)`, `stanceMap.Add(...)`, etc.). SquadService не может всё это делать сам без расширения API. Callback паттерн = main.go даёт factory, SquadService вызывает.

Старый `CreateFromUnits(units, formation)` остаётся. Если units не имеют UnitRole — они получают default Rifleman (Phase 12 ECS-zero-value поведение).

**P8. main.go test scene использует CreateFromTemplate.**

Текущий main.go (Phase 11) спавнит 12 units cluster'ами + 3 CreateFromUnits. Phase 12 переписывает: 3 CreateFromTemplate вызовов, каждый спавнит N units с соответствующими ролями.

```go
// Phase 12 starter squads:
//
// Squad 1 — Light Infantry (4 unit'а: Leader + 3 Rifleman) у дома 1.
// Squad 2 — MG Team (4 unit'а: Leader + MG + 2 Rifleman) у дома 2 / road.
// Squad 3 — AT Team (4 unit'а: Leader + ATGunner + 2 Rifleman) у бункера.
squadService.CreateFromTemplate(TmplLightInfantry, posCluster1, FormationLine, ...)
squadService.CreateFromTemplate(TmplMGTeam,        posCluster2, FormationWedge, ...)
squadService.CreateFromTemplate(TmplATTeam,        posCluster3, FormationColumn, ...)
```

Demonстрирует 4 разных ролей сразу. Расширить до 8-юнитной Motor Rifle template — нужно только увеличить cluster spacing.

**P9. Hover / selection — никаких изменений в семантике.**

Existing `selected []ecs.Entity` / `hovered ecs.Entity` остаются. Role info — это просто дополнительные данные в Inspector / render. Phase 9 / 10 / 11 семантика сохраняется.

**P10. `hasRadiomanInRoster` обновляется и начинает работать.**

В Phase 9 это была заглушка `return false`. Phase 12 — реальная проверка через Equipment.Secondary → Radio component check. Хотя `RadioNetwork.HasRadioman` всё ещё не читается ни одной системой (Phase 20 это сделает), флаг становится «правильным», что облегчает дебаг.

```go
// systems/squad_service.go
func hasRadiomanInRoster(w *ecs.World, units []ecs.Entity, equipmentMap *ecs.Map[components.Equipment], radioMap *ecs.Map[components.Radio]) bool {
    for _, u := range units {
        if !w.Alive(u) { continue }
        eq := equipmentMap.Get(u)
        if eq == nil || eq.Secondary == (ecs.Entity{}) { continue }
        if w.Alive(eq.Secondary) && radioMap.Has(eq.Secondary) {
            return true
        }
    }
    return false
}
```

Сигнатура расширяется — `equipmentMap` и `radioMap` теперь параметры. SquadService держит их как handles после Phase 12.

---

## Семь мильстоунов

### M12.1 — `UnitRole` types + `UnitRole` component

**Цель.** Существует `components.UnitRole` component с `UnitRoleKind` enum (10 значений). `String()` / `ShortLabel()` helper'ы. На сцене ничего не меняется (component'ы добавляются в Phase 12 через AssignRole/Template, тут пока никто не добавляет).

**Делаем:**
- `components/role.go`: `UnitRole` struct, `UnitRoleKind` enum, `String()`, `ShortLabel()`, `RoleColor()` lookup.
- `components/role.go`: 10 role kinds с правильным порядком (`RoleRifleman = 0` дефолт).

**Проверяем.** `go build` чист. Unit-test или smoke check: `RoleRifleman.String() == "Rifleman"`, `RoleMachineGunner.ShortLabel() == "MG"`.

### M12.2 — `SquadTemplate` + `TemplateRoster` helper

**Цель.** Существуют 7 template'ов с правильными role distribution'ами. `TemplateRoster(t)` возвращает `[]UnitRoleKind` верной длины.

**Делаем:**
- `systems/squad_template.go`: `SquadTemplate` enum, `TemplateRoster(t)` функция с 7 case'ами.

**Проверяем.** `TemplateRoster(TmplLightInfantry)` = `[Leader, Rifleman, Rifleman, Rifleman]`. `TemplateRoster(TmplMotorRifle)` length = 8, начинается с Leader.

### M12.3 — `RoleService` + Equipment-сущности расширение + новые компоненты Radio/Medkit/Spade

**Цель.** Существует `RoleService` с `AssignRole(unit, role)`. Каждой роли соответствует правильный Primary weapon (Phase 7 WeaponKind enum расширен) + Secondary gear (Radio / Medkit / Spade / Makarov / etc.). Spawn equipment'а корректный — owned-by-unit, pos sync'нут.

**Делаем:**
- `components/weapon.go`: добавить `WeaponPKM`, `WeaponSVD`, `WeaponRPG7`, `WeaponGP25`, `WeaponMakarov`.
- `components/equipment.go` (новый, либо в existing components.go): `Radio struct{}`, `Medkit struct{}`, `Spade struct{}` — placeholder marker-компоненты.
- `systems/role_service.go`: `NewRoleService(w)` + handles. `AssignRole(unit, role)`: 
  - Destroy existing Primary / Secondary entity'и unit'а (если есть).
  - Spawn new Primary weapon entity с правильным WeaponKind (per role mapping).
  - Spawn new Secondary gear entity (если role требует — Medic / Radio / Engineer / Leader).
  - Update unit's Equipment component (Primary / Secondary entity references).
  - Add UnitRole component to unit (или update if existing).
- main.go: `roleService := systems.NewRoleService(app.World)` рядом с другими service-объектами.

**Проверяем.** Создать unit, `roleService.AssignRole(unit, RoleMachineGunner)` — у unit'а появилcomponent UnitRole с Kind=MG, Equipment.Primary указывает на weapon entity с Kind=WeaponPKM. Создать unit, AssignRole(RoleMedic) — Equipment.Secondary указывает на entity с Medkit component'ом.

### M12.4 — `CreateFromTemplate` в SquadService + main.go starter squads

**Цель.** SquadService умеет создавать Squad из template'а. Test scene Phase 12 переписан: 3 starter squads из templates (Light Infantry / MG Team / AT Team). Каждый юнит имеет правильную роль + equipment.

**Делаем:**
- `systems/squad_service.go::CreateFromTemplate`: spawn N units по `TemplateRoster(t)`, AssignRole каждому, CreateFromUnits с указанной formation.
- main.go: заменить 3 текущих `CreateFromUnits` calls на `CreateFromTemplate`. Cluster позиции остаются (`unitPositions` массив больше не нужен — позиции вычисляются как `center + grid offset`).
- main.go: убрать ручной unit-spawn loop (Phase 11 спавнил 12 units в массиве, потом 3 CreateFromUnits). Phase 12: 3 CreateFromTemplate spawn'ит юнитов внутри себя.

**Проверяем.** Test scene видит 3 squad'а: cluster 1 = 4 unit'а (Leader + 3 Rifleman), cluster 2 = 4 unit'а (Leader + MG + 2 Rifleman), cluster 3 = 4 unit'а (Leader + ATGunner + 2 Rifleman). Через `Ctrl+P` snapshot — counts unit (12), weapon (Phase 11 был 12, Phase 12 может быть больше если Secondary тоже weapon — нет, Medkit и Spade и Radio это marker'ы не weapon).

### M12.5 — Visual differentiation в 3D: cap color + role label

**Цель.** Кубы юнитов имеют cap соответствующего role-color'а. Над каждым unit'ом виден ShortLabel роли (R / L / MG / AT / ...) как billboarded text.

**Делаем:**
- `render_world.go::drawUnitCube`: расширить сигнатуру для приёма `UnitRoleKind` (или `*UnitRole` component). Body cube — олива. Cap cube сверху (Y = stance_height + 0.05, размер 0.5×0.15×0.5) — color = `RoleColor(role.Kind)`.
- `render_world.go::drawUnitRoleLabel` (новая helper-функция): через `rl.DrawText3D` либо 2D screen-projected text — рисует ShortLabel над unit'ом, fixed-size font.
- main.go render loop: `unitRenderFilter` расширить до `Filter4[WorldPos, Unit, Stance, UnitRole]`. Передавать role в drawUnitCube. Label рисуется отдельным проходом (можно в том же loop'е).
- Label-render performance: на 12 юнитах ничего страшного. На 1000 — нужно screen-frustum culling (рисовать только видимые). Phase 21+ концерн.

**Проверяем.** Кубы Squad 1 — у командира жёлтый cap, у троих оливковый. Squad 2 — командир жёлтый, один с красным cap (MG), два оливковых. Squad 3 — командир жёлтый, один с тёмно-красным cap (AT), два оливковых. Над всеми — буквы L / R / MG / AT.

### M12.6 — Inspector role display + map commander icon

**Цель.** Inspector показывает роли в составе squad'а (color-tinted строки + ShortLabel). При single-unit selection — крупная роль вверху. Map commander marker показывает ShortLabel внутри круга.

**Делаем:**
- `ui/inspector.go::drawRosterSection`: каждая строка — `[ShortLabel] entity_id`, background = `RoleColor(role.Kind)` с alpha 0.4 (полупрозрачно поверх panel background).
- `ui/inspector.go::drawUnitDetails`: при single-unit selection — крупная строка «Role: <Name>» с цветом role'а.
- `ui/map_render.go::drawSquadMarkers`: после draw кружка, в центре draw'ить commander's ShortLabel белым или чёрным (контраст auto-detect: dark squad-color → white text, light → black).
- main.go подключение: render code теперь читает `UnitRole` через map (Phase 12 добавляет в main.go `roleMap := ecs.NewMap[components.UnitRole](app.World)`).

**Проверяем.** Кликнуть Squad 2 → Inspector roster: 4 строки, верхняя жёлтая [L], одна красная [MG], две оливковые [R]. Map: над Squad 2 кружок с буквой "L" внутри. Кликнуть индивидуально MG-юнита → Inspector single-unit view, заголовок «Role: MachineGunner» красным.

### M12.7 — `hasRadiomanInRoster` real implementation + closure + GAMEDESIGN sanity

**Цель.** `hasRadiomanInRoster` начинает реально проверять Equipment.Secondary на Radio component. Code cleanup, ROADMAP update, Phase 12 → ✅.

**Делаем:**
- `systems/squad_service.go::hasRadiomanInRoster`: реальная реализация по P10.
- SquadService держит `equipmentMap` + `radioMap` handles.
- При CreateFromUnits / CreateFromTemplate `RadioNetwork.HasRadioman` теперь заполняется верно.
- ISSUES.md: ничего не закрывается, но проверить что новые baselines (Tab лаг, etc.) не worse — на 12 юнитах role-rendering должен быть ~negligible overhead.
- README ROADMAP: Phase 12 → ✅.

**Проверяем.** Squad 1 (Light Infantry без радиста): `RadioNetwork.HasRadioman = false`. Squad с TmplRecon (имеет RadioOperator) — true. Phase 20 будет читать это для action masking — сейчас можно verify через `Ctrl+P` или debug print.

---

## Что считаем «закрытием Phase 12»

- `UnitRole` component на каждом юните; `UnitRoleKind` enum с 10 значениями.
- `SquadTemplate` с 7 базовыми template'ами; `TemplateRoster(t)` helper.
- `RoleService` с `AssignRole(unit, role)`: spawn appropriate Primary weapon + Secondary gear entity'ей, update Equipment component.
- 5 placeholder marker-component'ов для gear (Radio / Medkit / Spade); WeaponKind enum расширен 5 новыми значениями.
- `SquadService.CreateFromTemplate`: high-level spawn squad по template'у с правильными ролями.
- Test scene: 3 starter squads из template'ов (Light Infantry / MG Team / AT Team), демонстрируют 4 разных ролей.
- Visual differentiation в 3D: cap-color + ShortLabel над каждым юнитом.
- Inspector: roster показывает role-цвета и ShortLabel'ы; single-unit view имеет role-заголовок.
- Map: commander-marker показывает ShortLabel внутри squad-color круга.
- `hasRadiomanInRoster` — реальная реализация через Equipment.Secondary → Radio check.
- Pipeline: без изменений (Phase 12 — content + render, не симуляция). `... → ground_stick → unit_movement → vision → order_resolver → squad_macro_path → formation → map_marker_cache → lod → ...`

После этого — обновление ROADMAP, Phase 12 → ✅, переход к Phase 13 (Movement & Engagement control — Pace / Stance / Posture / PathStyle / Stamina / RoE matrix).

---

## Заметки на полях

- **UnitRoleKind ordering — RoleRifleman = 0 для zero-value safety.** Юнит без `UnitRole` component'а (теоретически) ведёт себя как Rifleman. Это страховка от Phase 7 legacy code или будущих spawn'ов где забудем `AssignRole`. Альтернатива — паника на missing component — слишком жёсткая для контент-слоя.

- **Equipment-сущности как placeholder marker'ы (Radio / Medkit / Spade) — без полей.** Phase 20 / 14 / 18 расширят эти struct'ы с реальными atributes (Radio.Frequency, Medkit.HealUses, Spade.BuildSpeed). Сейчас просто маркеры наличия. Это keeps Phase 12 узкой — мы добавляем разнообразие visual'а, не gameplay.

- **`AssignRole` destroy старое Equipment'а.** Если в будущей сессии гонять reassign (например, медик подобрал MG → стал MGGunner), нужно destroy старое weapon entity и spawn новое. Idempotent design.

- **Cap-cube visual is placeholder.** Phase 25 (Polish) заменит на нормальные модели солдата с разными снаряжением. Сейчас 2-cube юнит (body + cap) — это **достаточно** чтобы игрок видел кто есть кто. NATO-style icon'ы на map — то же самое.

- **Role label через DrawText3D vs screen-projected.** raylib имеет `rl.DrawText3D` — это billboarded text прямо в 3D-space. Координаты — world-space, размер — world-units (нужно подобрать). Альтернатива — `rl.GetWorldToScreen` + 2D draw. DrawText3D проще, но текст может быть «прозрачным» через стены (нет depth test'а в text shader'е по умолчанию). Я склоняюсь к **screen-projected 2D** — depth-test'им вручную через distance, и можем легко тюнить размер. M12.5 определит окончательно.

- **SquadTemplate с TmplRecon (5 unit'ов) не подходит к 4-юнитному cluster'у test scene'а.** Phase 12 в test scene использует только 4-юнитные template'ы (LightInfantry / MGTeam / ATTeam). Остальные template'ы существуют, но не демонстрируются — игрок (или будущий test scene scenario) может их явно создать через debug-hotkey.

- **Опционально — debug-hotkey для spawn template.** Что-то типа `Ctrl+Shift+1..7` → spawn template по template-id под cursor'ом. Полезно для playtesting roles на других template'ах. Не делать в Phase 12 (нет приоритета), но note'нуть как low-effort polish.

- **Phase 12 не трогает Phase 9 семантику cohesion.** Стрэгглеры детектируются (если auto-ejection включён) одинаково для всех ролей. Phase 15 (Tactical AI) может ввести per-role leash override (MG быстрее «отстаёт» из-за веса оружия — больший leash; Scout meньший — строг cohesion). Сейчас нет.

- **AssignRole vs unit-spawn split.** Я разделил: unit-spawn — это main.go (создаёт entity с Unit + Stance + Motion + Collider + ...), AssignRole — это RoleService (добавляет UnitRole + Equipment-сущности). Это позволяет main.go spawn'ить generic unit (для test scene без ролей), потом «навешать» роль через service. Альтернатива — single spawn function в RoleService которая делает всё — менее гибкая.

- **CreateFromTemplate `unitFactory` callback.** SquadService не должен знать про все компоненты юнита (Stance, Motion, Vision, Suppression, Awareness, etc.). main.go даёт factory который возвращает «полноценный голый юнит без роли». SquadService вызывает factory N раз (по template size), потом RoleService.AssignRole каждому, потом CreateFromUnits. Это keeps abstraction layers чистыми.

- **Map commander icon — может потребоваться больший круг.** Текущий радиус squad-маркера 6-12 px (per Phase 10). Внутри помещается одна буква (`L`) при font 10-12 px. Двух-буквенные (`MG`, `AT`, `GL`) могут вылезти. Решения:
  - (a) Использовать только 1-character (Leader=L, MG=M, AT=A, etc.) — теряем читаемость.
  - (b) Поднять радиус маркера до 14 px при наличии командирской метки.
  - (c) Рисовать label рядом с кружком (offset), а не внутри.
  - Решим в M12.6 — попробуем (b) сначала, fallback'нём на (c) если визуально не выйдет.

- **Phase 13 (Movement & Engagement) сильно зависит от Phase 12.** PathStyle / Pace / RoE — это per-squad настройки, но default'ы могут быть **per-role** (Sniper default = HoldFire RoE, MG default = StandoffMedium, Scout default = PathStyle CoverSeek). Phase 13 закладывает эту инфраструктуру; Phase 12 даёт ей source-of-truth (role identification per unit).

- **Phase 14 (Combat) сильно зависит от Phase 12.** Per-role weapon stats (RoF, range, dispersion) — это та же placeholder Weapon-component, но Phase 14 заполняет реальные числа per role. Sniper точнее но медленнее RoF, MG быстрее RoF но дисперсия выше. Phase 12 даёт role identification + WeaponKind разнообразие, Phase 14 даёт логику.

- **DemoMan vs Engineer overlap.** Per GAMEDESIGN §2, я отметил «сильно пересекаются; возможно объединим». Phase 12 пока держит обе роли. Если в playtesting'е окажется что DemoMan не имеет distinct поведения от Engineer + special equipment — объединим в `RoleEngineer` с поднабором `EngineerCanDemolish` flag'ом. Не Phase 12 решение.

- **`hasRadiomanInRoster` сейчас не читается никем.** После Phase 12 поле `RadioNetwork.HasRadioman` правильно заполняется, но никто не блокирует ввод на основании этого. Phase 20 (Comms) активирует action masking. Сейчас просто чистый scaffold.
