# Phase 17.6 — рабочий план

Контроль и взаимодействие со зданиями. Переработка ПКМ-flow: круговое pie-menu вырывается из RMB-flow, заменяется прямоугольным контекстным popup для зданий. ПКМ-tap на здание становится `OccupyBuilding` (новый kind равномерного распределения по этажам) вместо текущего Garrison-default. Camera orbit переезжает с RMB на MMB — RMB больше не претендует на камеру. Order-маркеры рендерятся в 3D-сцене для selected squads. Pie-код остаётся в `ui/pie_menu.go` deprecated (не вызывается из main.go), может быть удалён в Phase 21.

UX-полировка управления — фундамент под Phase 17.7 (Behavior Panel split) и Phase 17.5 (Visual fidelity). Не блокирует ни одну из них, но 17.7 + 17.5 опираются на чистый popup-flow и читаемую 3D-сцену.

Standing rules (stance / RoE / movement profile) сознательно НЕ затрагиваются — их перенос из Inspector в отдельную Behavior Panel — Phase 17.7. Phase 17.6 — только Order kinds + camera + markers + popup infrastructure.

ROADMAP — высокоуровневый трекер. `COMMAND-MODEL.md` §6 — спецификация input-механизмов (обновляется в M17.6.0). `CONTROLS.md` — снимок текущих биндингов (обновляется в M17.6.0). Этот файл — рабочий план фазы.

---

## Что в Phase 17.6 сознательно НЕТ

- **Behavior Panel split** (вынос Movement / Engagement / Behavior из Inspector в отдельный widget) — Phase 17.7.
- **Visual fidelity** (модели, текстуры, дорожные сплайны) — Phase 17.5, после 17.7.
- **`Demolish` / per-window Suppress** — Phase 14.8 (combat extension) или Phase 21. В popup секция «Атака» содержит только Зачистить и Подавлять-footprint в Phase 17.6.
- **`FallBack` / `Retreat` order kind** — Phase 21. Требует «safe direction» path-planner.
- **`DisperseOnLine` / `DefendLine` через RMB-drag** — Phase 19 (multi-squad) или 21.
- **Marker context menu** (insert / delete / drag waypoint) — Phase 21.
- **Mount / Dismount** — Phase 19 (vehicles).
- **Action bar внизу 3D** (быстрые one-shot кнопки) — Phase 18 (UI L4).
- **Order kind в popup на terrain / враге / траншее** — Phase 17.6 popup только для здания. ПКМ-tap на terrain/враге/траншее работает как сейчас (через `resolveTargetIntoOrder`).
- **ClearBuilding параметры** (aggressive / cautious / use grenades / stop after floor) — Phase 21. В 17.6 один Clear-kind без параметров.
- **Reason / Resume condition** как predicate — никогда. Текстовая строка из `TacticalOverride.Reason` — M17.6.10 polish, опционально.
- **Per-floor / per-window cover-aware OccupyBuilding distribution** — Phase 21. В 17.6 round-robin по Floor центрам со spread.
- **Terrain popup** — не делаем. Default MoveTo (RMB-tap), facing-drag (RMB-drag), modifiers (Alt/Ctrl/Shift) покрывают все frequent terrain-кейсы.

---

## Решения, которые лочим до начала кода

**P1. `OrderKindOccupyBuilding` — новый OrderKind, не модификация Garrison.**

Семантика: «зашли и расселись равномерно, без боевого подтекста». Completion = «все живые юниты в ростере находятся внутри Building footprint» (тот же check что у Garrison через `CompletionEveryMemberOnFloor`). Distribution: ростер делится на N этажей округлением вверх (5 юнитов / 2 этажа = 3+2), каждая подгруппа получает MoveTo на Floor.AABB center с spread по NavGrid через FormationSystem.

Не использует ShootingArc / Window / CoverDirection — это отличает Occupy от Garrison. Garrison остаётся как kind «у окон», вызывается из popup «Атакующая позиция».

Альтернатива (отклонена): расширение Garrison флагом `OrderParamOccupyMode{WindowAttach bool}`. Один kind с двумя режимами — двусмысленно при чтении логов / Inspector, флаг не виден в существующем UI без специального чтения. Чистая семантика kind'ов важнее экономии 50 строк OrderResolver кода.

**P2. ПКМ-tap на building → OccupyBuilding (default через hit-test).**

`command.go:resolveTargetIntoOrder` правка: `case HitBuilding: return components.OrderKindOccupyBuilding, hit.Entity` (было `OrderKindGarrison`). Garrison-default остаётся только при `kindOverride != nil` (из popup).

Существующая `rmbPressTargetIsBuilding` ветка в main.go:1434-1452 + 1521-1528 (Phase 16.B.1.b) — snap target к ground-floor центру + force MoveTo — **удаляется**. OccupyBuilding покрывает кейс «зайти в здание» лучше: target идёт на Floor entity напрямую, NavService уже знает что зайти нужно через дверь (TransitionRegistry, Phase 7).

**P3. Camera orbit на MMB вместо RMB.**

`systems/orbit_system.go:48` — `rl.IsMouseButtonDown(rl.MouseButtonRight)` → `rl.MouseButtonMiddle`.

`OrbitInputEnabled` гейт сохраняется (focused = Panel3D или None + не floating-busy), но дополнительные suppressors `!pieMenu.IsActive() && pieMenu.SourcePanel == ui.PanelNone` (main.go:1890-1892) **удаляются** — pie не активна в RMB flow вообще.

MMB в PanelMap (pan карты, main.go:1124-1140) сохраняется через panel-focus гейтинг: `OrbitInputEnabled` false при `focused == PanelMap`, поэтому MMB-drag в map-панели пан'ит карту, не вращает 3D-камеру.

Подводный камень: пользователю нужно привыкать к новому жесту. Если playtest покажет что MMB неудобен — можно вернуться к RMB-orbit, но тогда контекстное меню нужно полностью на RMB-hold > 200ms (без drag confusion). Phase 17.6 фиксирует MMB.

**P4. `ui.ContextMenu` — generic infrastructure, не private для здания.**

Структура (новый файл `ui/context_menu.go`):

```go
type ContextMenu struct {
    Active   bool
    Origin   rl.Vector2  // позиция курсора при открытии
    Sections []MenuSection
    HoveredItem int  // -1 если ни на чём
}

type MenuSection struct {
    Header string         // "Атака" / "Взаимодействие" / ""
    Items  []MenuItem
}

type MenuItem struct {
    Label   string
    Tooltip string         // 1-2 строки описания, показ при hover
    Icon    rl.Texture2D   // 16×16 placeholder, может быть пустой текстурой
    Kind    components.OrderKindCode
    Enabled bool            // grayed когда неприменимо
    OnSelect func()        // callback при click; null = no-op
}
```

Triggers: RMB-hold > 200ms на здании. RMB-tap (release < 200ms без drag) → default OccupyBuilding. RMB-drag > 8 px → facing-drag (без открытия popup).

Popup отрисовывается с offset (+12, +12) от cursor чтобы не накрыть press point. Если popup упирается в правый/нижний край панели — flip к (-, -). Внутри popup: hover-highlight на item under cursor, tooltip отрисовывается **отдельной строкой над popup** (не inline) — Total War / Wargame паттерн.

ESC → Cancel. LMB на item → execute OnSelect + close. LMB вне popup → Cancel.

Reuse'абельность: Phase 17.6 использует ContextMenu только для здания. Phase 18+ может использовать его для marker-context-menu / unit-popup / trench-popup / vehicle-popup без изменений.

**P5. Содержимое building popup (Phase 17.6).**

```
─ Атака ──────────────────────
  ▣ Зачистить и занять
  ⚡ Подавлять огонь
─ Взаимодействие ─────────────
  ◯ Атакующая позиция у окон
  ◌ Закрытая позиция
  ─────────────────────────
  ◇ Занять L0
  ◇ Занять L1
  ◇ Занять L2
```

Иконки — placeholder (один из встроенных glyphs / unicode символов; реальные .png — Phase 17.5). Tooltip пример:

- «Зачистить и занять»: «Войти, уничтожить врагов внутри, удержать»
- «Подавлять огонь»: «Стрелять по зданию для подавления гарнизона»
- «Атакующая позиция у окон»: «Распределиться по окнам с боевыми секторами»
- «Закрытая позиция»: «Зайти тихо, сидеть в Crouch, не стрелять первыми»
- «Занять L*N*»: «Зайти именно на этаж *N* через ближайшую лестницу»

Динамические L-пункты: один MenuItem на каждый Floor entity из `BuildingPlanIndex.Levels[building]`. Если здание 1-этажное — секция «Занять L*N*» не появляется (Атакующая позиция покрывает кейс).

**P6. `OrderKindClearBuilding` — новый kind, composite через OrderChain.**

Семантика: «войти, уничтожить врагов, остаться». Completion: `no hostile units inside footprint AND >= 1 friendly inside`. Реализация — новый arm в `order_resolver_completion.go`, читает `Faction` + `WorldPos` для каждого юнита внутри footprint.

При completion → автоматический IssueOrder OccupyBuilding в queue (через OrderChain.Next). Если completion immediate (никого враждебного не было) — chain выполняется сразу, ощущение «зашли и заняли». Если враги были — отряд сражается внутри, при clearing условии — переходит в OccupyBuilding (расселяется по этажам).

Phase 17.6 baseline без параметров. Phase 21 расширения (aggressive/cautious/grenades/stop-after-floor) — отдельный M-milestone в будущей фазе.

**P7. «Закрытая позиция» — не новый OrderKind, preset на OccupyBuilding.**

OccupyBuilding с `OrderParamMovementProfile{Stance: Crouch, Pace: Walk, Posture: Quiet}` + `OrderParamEngagementOverride{Mode: HoldFire}` (если последний компонент существует; иначе создаём в M17.6.6). Не дублирует OrderKind — «закрытая» это modifier на занятие.

Альтернатива (отклонена): отдельный `OrderKindHidePosition`. Семантически — то же самое что OccupyBuilding + standing rules override, дублирование вредно для spec table.

**P8. «Занять L*N*» — OrderKindMoveTo на Floor entity center.**

Не OccupyBuilding — это **направленное** занятие конкретного уровня. Target = Floor entity, `OrderTarget.Pos = Floor.AABB.Center`, `OrderTarget.Entity = Floor entity`. NavService уже умеет path к Floor через TransitionRegistry (Phase 7).

Ростер целиком идёт на один этаж, без deviation на другие. Если ростер большой / этаж маленький — spread в пределах Floor.AABB через FormationSystem clamp.

Distinction от OccupyBuilding: OccupyBuilding делит ростер по всем этажам, «Занять L*N*» концентрирует на одном.

**P9. 3D order markers — только для selected squads, depth-test off.**

Walk `OrderQueueHead.First` → chain через `OrderChain.Next` для каждого squad в selection. Для каждого Order:

- **Marker primitive**: куб 0.4 m × 0.4 m × 0.4 m в `Order.Target.Pos.ToRenderSpace(CurrentOriginChunk)`, tinted в `squadColor(squad)`.
- **Соединительная линия**: от squad center (для head order) или от предыдущего marker (для chain) до этого marker'а. `rl.DrawLine3D`, alpha 180.
- **Per-kind glyph**: 16×16 текстура на спрайте над marker'ом (через `GetWorldToScreenEx`). Phase 17.6 — буква-placeholder (M, G, O, C, S, …); реальные иконки — Phase 17.5.
- **DefendPosition sector arc**: reuse существующий `drawGhostArc` (Phase 13.6), но в active-color а не ghost-alpha.

`rl.DisableDepthTest()` обёртка вокруг рендера маркеров — всегда видны сквозь стены/здания. Это стандартный RTS-паттерн (Wargame, Combat Mission, Total War).

Active-order маркер — bigger (0.6 m) + glow (доп. wire куб). Queued — smaller (0.4 m) + dimmed.

**P10. Подсветка floor section под cursor.**

Существующий `drawBuildingOutline(building, color)` (main.go:2102-2106) рисует контур по полному `Building.Footprint`. Расширяем:

При hover на здании, если `mouseTargetWorldPos` ray-пересекает Floor entity AABB stack (Floor.AABB.MinY ≤ Y ≤ MaxY), подсвечиваем `Floor.AABB` outline'ом (а не полный footprint). Фолбэк (ray miss / pre-stack) — текущий полный footprint outline.

Ray vs Floor AABB stack: walk Floor entities принадлежащих hovered building (через `buildingPlanIndex.Levels[buildingRoot]`), для каждого проверить ray ∩ AABB. Выбрать самый верхний (ближе к камере) который пересекается. ~30 строк в новой функции `floorUnderRay(ray, building) ecs.Entity`.

**P11. Ghost preview для OccupyBuilding.**

Текущий `ghost.go` имеет branch для Garrison (first-N окон). Новый branch для OccupyBuilding:

- Делим ростер N на M этажей (M = `len(buildingPlanIndex.Levels[building])`).
- На каждом этаже dots в центре Floor.AABB.Center с spread через FormationSystem.resolveSlot (Loose formation, spacing 1.5 m).
- Ghost-цвет — neutral grey alpha 80 (как существующий ghost), не tinted в squad-color (это preview, не active marker).

В popup-active state (RMB-hold открыл меню) — ghost обновляется при hover'е item: если cursor на «Атакующая позиция» → ghost к окнам; если «Закрытая позиция» / «Занять L0» / «OccupyBuilding» (default) → ghost к этажам. Reuse существующего `pieMenu.HoveringKind` паттерна, только из ContextMenu вместо pie.

**P12. RMB-drag = facing-drag всегда (без зависимости от pie/popup state).**

Текущая логика: `pieMenu.HasSelection` ветка в `pie_menu.go:Tick` решает «facing vs orbit» при drag > 8 px. После Phase 17.6 — pie не активна, drag сразу facing если есть selection в Panel3D.

main.go упрощается: 
```go
if rmbDown && cursorMovedPx > 8 && len(selected) > 0 && focused == ui.Panel3D {
    // facing-drag mode active until release
}
```

Без pieMenu посредничества. Logic перенесётся в новый `rmbState` struct (`rmbState.facingDragActive bool`) или прямо в main.go locals. Yaw computation остаётся как сейчас: `atan2(cursor.X - origin.X, -(cursor.Y - origin.Y))`.

OrbitInputEnabled при facing-drag — false (но MMB всё равно работает, потому что MMB и RMB — разные кнопки).

**P13. Pie menu code remains in `ui/pie_menu.go` deprecated.**

Не удаляем — `PieMenu` struct, `drawPieWedge`, `segmentAt`, `pieSegments` могут быть полезны для будущих radial-кейсов (waypoint editing на маркере, weapon-bar circular layout). `ui/pie_menu.go` остаётся в репо без вызывающих сторон. Build не ломается (только unused-warnings от линтера — добавим `_ = pieSegments` или go-build-tag если warnings раздражают).

Альтернатива (отклонена): удалить файл целиком. Если popup для terrain / weapon-bar / marker всё же захочется radial — придётся восстанавливать. Дешевле оставить.

---

## M-milestones

**M17.6.0 — Docs sync.**

Обновить `COMMAND-MODEL.md` §6 (input grammar): RMB-tap = default Order, RMB-hold = popup для здания (вместо pie), RMB-drag = facing-drag (всегда). Удалить упоминание pie menu из §6, перевести его в «Deferred / unused» секцию.

Обновить `CONTROLS.md` §3.1 и §3.3: RMB-hold > 200ms на здании → ContextMenu (вместо PieMenu); §2 → MMB orbit; §13 (свободные клавиши) — Z больше не зарезервирован (когда-то планировали для stance, в Phase 17.6 не используется).

Обновить `CLAUDE.md` секция Controls если упоминается RMB-orbit или pie-menu специфика.

**M17.6.1 — Camera switch RMB → MMB.**

`systems/orbit_system.go:48` — `MouseButtonRight` → `MouseButtonMiddle`. main.go:1890-1892 — удалить pie-related suppressors из OrbitInputEnabled.

**Verify:** MMB-drag в Panel3D вращает камеру, RMB-drag не вращает. MMB-drag в PanelMap pan'ит карту (не вращает 3D). Wheel zoom работает в обеих панелях согласно focus.

**M17.6.2 — `OrderKindOccupyBuilding` + spec + completion arm + default hit-test.**

Новые компоненты/энумы:
- `components.OrderKindOccupyBuilding = OrderKindCode(N)` (следующий свободный)
- Spec entry в `OrderKindSpecs`: Name="Occupy", NeedsEntity=true (Building), InPieMenu=false (pie deprecated), Completion=CompletionEveryMemberOnFloor (reuse Garrison completion arm).
- `command.go:resolveTargetIntoOrder` — `case HitBuilding: return OrderKindOccupyBuilding, hit.Entity` (было Garrison).
- Удалить `rmbPressTargetIsBuilding` ветку в main.go (P2).
- Distribution в `order_resolver_target.go` или новом arm: ростер делится по этажам, каждый юнит получает MoveTo на Floor.AABB.Center.

**Verify:** ПКМ-tap на 2-этажный Office (5-юнитный squad) → squad заходит, 3 юнита на L0, 2 на L1 (или 2+3 — round-robin). Никто не висит снаружи. Inspector показывает `OrderKindOccupyBuilding` в Orders section.

**M17.6.3 — `ui.ContextMenu` infrastructure.**

Новый файл `ui/context_menu.go`: struct definitions (P4), `Begin`, `Tick`, `Draw`, `HitItem`. Generic — не привязан к building.

Self-tests: ручной test scene с ContextMenu открывающимся на ключевом нажатии (например F12) с hardcoded items. Verify: tooltip отрисовывается над popup, hover highlight работает, ESC/click-outside закрывают, LMB на item вызывает OnSelect.

**M17.6.4 — Building popup integration в RMB flow.**

main.go RMB handler reshape:
- RMB-press на building с selection → start press-timer.
- Если в течение 200ms курсор не сдвинулся > 8 px и RMB released → tap path: default OccupyBuilding через `resolveRMBOrder`.
- Если 200ms истекли без drag → open ContextMenu c building items (P5). Pie не открывается.
- Если drag > 8 px до 200ms → facing-drag mode (P12).
- ESC / LMB вне popup / click на item → close popup. Order issue через `resolveRMBOrderWithParams` с `kindOverride` из item.Kind.

`pieMenu.Begin/Tick/Draw` callsite — удалить из main.go (P13). pieMenu state переменные в main.go state struct — удалить.

**Verify:** Tap на здание → squad заходит (OccupyBuilding). Hold > 200ms → popup появляется. Drag > 8 px → ghost rotates с курсором, no popup, no orbit. ESC закрывает popup.

**M17.6.5 — `OrderKindClearBuilding` + chain → OccupyBuilding.**

Новый kind:
- `components.OrderKindClearBuilding = OrderKindCode(N+1)`
- Spec: Name="Clear", NeedsEntity=true, Completion=CompletionClearBuilding (новый arm).
- Completion arm `clearBuildingCompletion(world, order, footprint)`: walks units inside footprint, считает hostile (Faction != PlayerFaction). Returns Completed когда hostile == 0 AND friendly >= 1.
- При Completed → IssueOrder OccupyBuilding на тот же building entity (через `SquadService.IssueOrder` с append=false, без shift). Auto-chain.

**Verify:** Test scene с 2 enemy MotorRifle внутри здания. Player squad с popup → «Зачистить и занять». Squad заходит, уничтожает врагов, после clearing — расселяется по этажам (видно в Orders section: Clear → completed, OccupyBuilding → InProgress).

**M17.6.6 — «Закрытая позиция» preset + «Занять L*N*» через Floor target.**

«Закрытая позиция»:
- Использует OccupyBuilding kind + OrderParamMovementProfile{Stance: Crouch, Pace: Walk, Posture: Quiet}.
- Если `OrderParamEngagementOverride{Mode: HoldFire}` отсутствует как компонент — добавить (новый optional component, читается WeaponSystem при `shouldFire`).
- Popup item «Закрытая позиция» создаёт Order с этими params.

«Занять L*N*»:
- Использует OrderKindMoveTo (не нужен новый kind).
- `OrderTarget.Pos = Floor.AABB.Center`, `OrderTarget.Entity = Floor entity`.
- Popup item создаётся динамически: один per Floor entity.

**Verify:** Click «Закрытая позиция» → squad в Crouch внутри здания, не открывает огонь по врагу снаружи через окно. Click «Занять L1» → squad идёт именно на 2-й этаж (через лестницу), не на 1-й.

**M17.6.7 — Order markers в 3D.**

Новая функция `drawOrderMarkers3D(world, selected, squadMaps, …)` в `render_overlays.go`. Walk OrderQueueHead.First для каждого selected squad → chain. Render:
- Куб per Order (active = big+wire, queued = small+dimmed).
- Линия от squad center / previous marker до текущего.
- Per-kind glyph (placeholder буква) через 2D screen overlay в скиссоре Panel3D.
- DefendPosition arc (reuse `drawGhostArc` в active alpha).
- `rl.DisableDepthTest()` обёртка.

**Verify:** Selected squad с очередью MoveTo → Garrison → DefendPosition → видны 3 marker'а в 3D, соединённые линиями. Видны сквозь стены. При смене selection маркеры перерисовываются для нового squad'а.

**M17.6.8 — Подсветка floor section под cursor.**

Новая функция `floorUnderRay(ray rl.Ray, building ecs.Entity) ecs.Entity` в render_buildings.go или нового файла. Walk Floor entities (через buildingPlanIndex.Levels[building]), ray ∩ AABB test, return топ-most match.

main.go hover update (около строки 1826-1839): если cursor over building AND ray hits Floor → подсвечиваем Floor.AABB outline'ом, не полный footprint. Иначе — полный (текущее поведение).

**Verify:** Hover на верхнюю часть 3-этажного здания → outline вокруг L2. Hover на нижнюю → L0. Hover на здание издалека (ray miss из-за угла камеры) — полный footprint fallback.

**M17.6.9 — Ghost preview для OccupyBuilding и popup-hover ghost swap.**

Новый branch в `ghost.go`: `OrderKindOccupyBuilding` → dots по N этажам. Reuse FormationSystem.resolveSlot с Loose formation в Floor.AABB.Center.

ContextMenu open + hover на item → ghost обновляется per-kind (как сейчас pie HoveringKind работает): на «Атакующая позиция» → ghost к окнам (existing Garrison); на «Закрытая позиция» / default → ghost к этажам; на «Занять L1» → ghost на конкретном Floor.

**Verify:** Hover на «Атакующая позиция» в popup → ghost dots появляются у окон. Switch hover на «Закрытая позиция» → ghost dots перестраиваются по этажам. Switch на «Занять L1» → ghost dots концентрируются на L1.

**M17.6.10 (polish, opt) — Reason / Resume condition строка в Inspector override-row.**

Если время остаётся в фазе. Уже существующий `TacticalOverride.Reason` (string) показывается в Inspector. Добавить:
- Гарантировать что Reason заполнен для всех override источников (SurvivalInstinct, StanceController).
- Текстовая строка «Resume when: …» — формальное описание условий (не predicate, просто display). Хранится либо в `TacticalOverride.ResumeWhen string`, либо вычисляется ad-hoc по типу override.

**Verify:** Squad под огнём → Inspector override-row показывает «Override: Taking cover», «Reason: Suppression 0.74 > threshold 0.45», «Resume when: suppression < 0.20 and no enemy 10s».

---

## Closure criteria

`cmd/tactical_sandbox` или dedicated test scene в main.go.

Сцена: 2-storey Office building + 5 player Recon squad outside + 2 enemy MotorRifle inside (2-й этаж).

Test sequence:

1. **Camera.** MMB-drag вращает камеру, RMB-drag не вращает. MMB в map панели pan'ит. RMB-drag на terrain (без selection) ничего не делает.
2. **Default tap.** ПКМ-tap на здание → squad заходит, распределяется (3 на L0, 2 на L1 или 2+3). Inspector → OrderKindOccupyBuilding.
3. **Popup open.** ПКМ-hold > 200ms на здании → popup появляется рядом с курсором. 6 пунктов в 2 секциях (Атака: 2, Взаимодействие: 4 = Атакующая, Закрытая, Занять L0, Занять L1). Иконки видны. Tooltip при hover.
4. **Clear and Occupy.** Click «Зачистить и занять» → squad заходит, уничтожает 2 enemy MotorRifle, после clearing переходит в OccupyBuilding (видно в Inspector orders chain).
5. **Garrison.** Click «Атакующая позиция у окон» → squad распределяется по окнам, у каждого виден ShootingArc через окно.
6. **Hide.** Click «Закрытая позиция» → squad в Crouch внутри, не стреляет по врагам снаружи через окно (RoE HoldFire active).
7. **Floor pick.** Click «Занять L1» → squad идёт на 2-й этаж через лестницу. Никто не остаётся на L0.
8. **3D markers.** В 3D видны marker'ы текущего Order'а selected squad'а + queued (если есть). Marker'ы видны сквозь стену здания (depth-test off).
9. **Floor outline.** Hover cursor на верхнем этаже → outline вокруг L1, не вокруг всего footprint. На нижнем → L0.
10. **Popup-ghost swap.** Hover на «Атакующая позиция» в popup → ghost dots у окон. Hover на «Закрытая позиция» → ghost dots по этажам.
11. **No regressions.** Trench OccupyTrench работает через RMB-tap. Terrain MoveTo работает. Hostile AttackTarget работает. Facing-drag на terrain работает. Inspector chips (RoE / Movement / Doctrine / Autonomy) работают как раньше.
12. **ESC cancel.** Open popup → ESC → popup закрывается, ни один Order не issued.

Performance: median `order_resolver` + `unit_movement` tick не растёт > 0.5 ms на 50 units.

---

## Зависимости

- **Phase 17 (Tactical AI Wave 2)** — закрыт. MicroPath + Cover 2.0 + StanceController обеспечивают что юниты заходят в здание без bug'ов и распределяются разумно.
- **Phase 14.6 (Garrison completion + no-walkthrough)** — закрыт. `CompletionEveryMemberOnFloor` arm + `NavInBuilding` bit reuse'абельны для OccupyBuilding completion.
- **Phase 15 (Tactical AI / Autonomy / Doctrines)** — закрыт. MovementProfile override через OrderParamMovementProfile уже работает.
- **Phase 13.6 (Ghost preview & facing-drag)** — закрыт. Facing-drag механика переиспользуется без pieMenu посредничества.
- **Phase 11 (Orders foundation)** — закрыт. OrderQueueHead + OrderChain + OrderKindSpecs spec table готовы под новые kinds.

Не блокирует Phase 17.7 (Behavior Panel split) — фаза 17.7 независима от popup-flow. Можно делать параллельно если будет ресурс.

Не блокирует Phase 17.5 (Visual fidelity) — модели/текстуры можно делать поверх любого UI-flow.
