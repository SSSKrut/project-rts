# Phase 11 — рабочий план

Orders foundation: `Order` как отдельная ECS-сущность с lifecycle. 5 типов в MVP — `MoveTo`, `Garrison`, `OccupyTrench`, `DefendPosition`, `Patrol`. Hit-test resolver (ПКМ контекстно выбирает тип в зависимости от того, во что попал курсор). Order markers на 2D-карте. Inspector показывает текущий приказ и очередь. Multi-squad RMB → N распределённых Order'ов (фикс ISSUES #3). Pie menu при ПКМ-hold. Refactor `MacroPath` — становится derived state от current Order, не primary store. LOD fix для длинных путей (ISSUES #4).

**Что в Phase 11 сознательно НЕТ.** Vector orders (RMB-drag для ширины фронта / facing'а multi-squad) — Phase 19. Полноценная auto-distribution для multi-squad (полукруг охвата, фланговые vs frontal) — Phase 19. Условные триггеры на Order'ах (`on_contact → push tactical_response`) — Phase 15. Per-kind глубокая AI-реализация (как именно Garrison распределяет по окнам, как Defend выбирает сектор) — Phase 15. Stamina / Pace / Posture как параметры приказа — Phase 13. RoE как override приказа — Phase 13. Action masking от RadioNetwork — Phase 20. Build-приказы (Trench / Sandbag / Mines) — Phase 18. Mount / Disembark / Follow / Escort — Phase 17. AttackMove / Sneak / SuppressFire / Retreat как Order types — будут добавлены по мере появления combat (Phase 14) и движения (Phase 13). Здесь в MVP только 5 базовых.

ROADMAP — высокоуровневый трекер. Этот файл — рабочий, обновляется по ходу выполнения.

---

## Решения, которые лочим до начала кода

**P1. Order — отдельная ECS-сущность.** Залочено в GAMEDESIGN §3. Структура:

```go
// Базовые компоненты на Order-entity (все обязательные):
type Order struct{}                                  // marker (filter-target)
type OrderKind struct{ Code OrderKindCode }
type OrderState struct{ Code OrderStateCode }
type OrderOwner struct{ Squad ecs.Entity }
type OrderTarget struct {
    Pos    WorldPos       // для kind'ов с координатой
    Entity ecs.Entity     // для kind'ов с целевой сущностью (Building, Trench, ...)
}
type OrderIssued struct{ Time float32 }              // session-time выдачи
type OrderProgress struct{ Value float32 }           // 0..1, для long-running

// Опциональные per-kind параметры (компоненты, добавляются только если нужно):
type OrderParamFacing struct{ YawRad float32 }       // для DefendPosition
type OrderParamPatrol struct{ Loop bool }            // для Patrol

// Связь в цепочку:
type OrderChain struct{ Next ecs.Entity }            // 0 = конец цепочки

// На Squad-entity (поле очереди приказов):
type OrderQueueHead struct{ First ecs.Entity }       // 0 = idle
```

`OrderKindCode`: `OrderMoveTo`, `OrderGarrison`, `OrderOccupyTrench`, `OrderDefendPosition`, `OrderPatrol` (5 значений в MVP).
`OrderStateCode`: `OrderIssued`, `OrderInProgress`, `OrderBlocked`, `OrderCompleted`, `OrderCancelled`, `OrderFailed`.

**P2. `OrderQueueHead.First` — primary order state на Squad'е. `MacroPath` остаётся, но становится derived/cached.**

До Phase 11: `Squad.MacroPath.HasGoal/Goal` — primary state, `SquadMacroPathSystem` пишет туда. После Phase 11: `OrderQueueHead.First` — primary; SquadMacroPathSystem читает текущий Order'а, при Kind ∈ {MoveTo, Garrison, OccupyTrench, DefendPosition, Patrol} планирует A* до `OrderTarget.Pos` (или resolved-position для entity-target'ов) и **кэширует** в `MacroPath.Waypoints`. Поля `HasGoal/Goal` уходят (или становятся redundant copy для backward-compat — решаем в M11.1).

FormationSystem остаётся как был — читает `MacroPath.Waypoints[Head]` для centerTarget. Не знает о Order'ах напрямую. Это сохраняет Phase 9-инвариант «UnitMovementSystem не меняется».

**P3. Order lifecycle с явными переходами:**

```
Issued ─── (resolver/system дочитал) ──→ InProgress
InProgress ─── (target достигнут или kind=Complete-on-arrival) ──→ Completed
InProgress ─── (нет пути / target dead / timeout) ──→ Blocked
InProgress ─── (игрок отменил) ──→ Cancelled
Blocked ─── (retry удался) ──→ InProgress
Blocked ─── (giveup) ──→ Failed
```

Завершённые приказы (`Completed/Cancelled/Failed`) живут один-два тика для рендера progress'а потом удаляются `OrderCleanupSystem`. Head переключается на `OrderChain.Next` при Completed/Cancelled (если есть Next), иначе `OrderQueueHead.First = 0`.

`OrderState.Code = OrderIssued` для свежевыданных. `OrderResolverSystem` каждый тик берёт орденов с этим состоянием и переводит в `InProgress` (после первой обработки SquadMacroPathSystem'ом). Это позволяет различать «приказ только что пришёл — нужно re-plan A*» от «приказ давно идёт».

**P4. OrderResolverSystem — центральный диспетчер.**

Каждый тик:
1. Filter `Squad + OrderQueueHead` — берёт текущие head'ы.
2. Если head валиден и `State == Completed/Cancelled/Failed` → продвигает head на `Chain.Next`; если Next нет → `First = 0`.
3. Если head валиден и `State == InProgress` → ничего, ждёт per-kind executor'а.
4. Если head валиден и `State == Issued` → переводит в `InProgress`. Это сигнал SquadMacroPathSystem'у на немедленный re-plan (через сброс `MacroPath.ReplanAt = 0`).
5. Per-kind completion-проверка:
   - `MoveTo`/`Garrison`/`OccupyTrench`: центр squad'а в `arrivalRadius` от target → Completed.
   - `DefendPosition`: никогда не Completed автоматически (стоит до Cancelled).
   - `Patrol`: достиг последнего waypoint'а и `!Loop` → Completed; если `Loop` → переход на первый waypoint снова (через chain replay).

**P5. SquadMacroPathSystem становится Order-aware.**

Изменения:
- Filter теперь `Squad + CommandRoster + MacroPath + FormationData + OrderQueueHead` (добавился последний).
- Per-squad: если `OrderQueueHead.First == 0` — squad idle, FormationSystem ничего не делает (как сейчас при `HasGoal=false`).
- Иначе: читает `OrderKind` + `OrderTarget` из head order. Резолвит target в WorldPos:
  - `OrderMoveTo` / `DefendPosition` / `Patrol`: `OrderTarget.Pos` напрямую.
  - `OrderGarrison`: footprint center of `OrderTarget.Entity` (Building).
  - `OrderOccupyTrench`: nearest point on trench polyline of `OrderTarget.Entity` (Trench).
- Дальше существующая A*-логика как раньше.

FormationSystem **не меняется** — продолжает читать `MacroPath.Waypoints[Head]` как centerTarget.

**P6. Hit-test resolver — превращение клика в Order.**

Новая функция в `command.go`:
```go
func resolveTargetIntoOrder(target rl.Vector3, hit HitTestResult, modifiers OrderModifiers) (kind OrderKindCode, entityTarget ecs.Entity)
```

`HitTestResult` — результат проверки «что было под курсором»:
- `HitBuilding{Entity}` — в footprint здания
- `HitTrench{Entity}` — на полилинии окопа
- `HitTerrain` — пусто

Mapping:
- `HitBuilding` → `OrderGarrison`, `entityTarget = Building`
- `HitTrench` → `OrderOccupyTrench`, `entityTarget = Trench`
- `HitTerrain` → `OrderMoveTo`, `entityTarget = 0`
- Pie menu override игнорирует HitTest (явный выбор игрока).
- Modifier'ы: Shift → append-to-queue (см. P9), Alt/Ctrl — резерв для Phase 13 (AttackMove / Sneak).

Hit-test реализация:
- Building: проверка точки в `AABB2D Footprint` каждого Building entity. O(N), N=10-20 buildings — копейки.
- Trench: distance-to-polyline для каждой `Trench` line, threshold 2-3 м. O(M lines × K points).

**P7. Order markers на 2D-карте.**

Для каждой Squad-сущности с активным head'ом:
- Линия от squad center к first waypoint OR `OrderTarget.Pos` (если path не построен).
- Иконка типа приказа над target'ом: square=Garrison, triangle-down=OccupyTrench, diamond=DefendPosition, dot=MoveTo, circle-with-arrow=Patrol.
- Цвет: squad palette (existing).
- Опционально (если время): остальные waypoints в `MacroPath.Waypoints` соединены тонкой пунктирной линией.

Реализация — в `ui/map_render.go::drawOrderMarkers`, рисуется поверх squad-markers.

**P8. Inspector показывает order info.**

Для single-squad selection:
- Текущий Order: kind name + target (Pos или Entity name), State, Progress.
- Queue (если есть Chain.Next): «Next: kind name → ...» — список длиной 1-3 (показываем head + до 2 следующих).
- Если idle: «No order».

Для single-unit selection (member of squad): «Squad order: …» с тем же display.

Добавится в `ui/inspector.go`.

**P9. Multi-squad RMB → распределённые Order'ы (фикс ISSUES #3).**

Новый helper `groupSelectionByOwner` в `input.go`:
```go
type SelectionGroups struct {
    SquadsToOrder []ecs.Entity      // уникальные squad'ы, чьи члены в selection
    Soloists      []ecs.Entity      // юниты без squad'а
}
func groupSelectionByOwner(selected []ecs.Entity, squadMemberMap *ecs.Map[SquadMember]) SelectionGroups
```

Новый `resolveRMBOrder`:
```go
groups := groupSelectionByOwner(selected, squadMemberMap)
for _, squad := range groups.SquadsToOrder {
    issueOrder(squad, kind, target, entityTarget, shiftHeld)  // P10 ниже
}
for _, e := range groups.Soloists {
    // existing per-unit MoveTo behaviour для одиночек
    ...
}
```

**Никаких squadService.Leave** — squad-членов не вырывает из ростера. Это закрывает ISSUES #3.

В Phase 11 все squad'ы получают **один и тот же target** (никакой auto-distribution). Distributing по полукругу / spread'у — Phase 19. Пока многоотрядный приказ = все целятся в одну точку и пытаются туда дойти; если они мешают друг другу — это видно, и Phase 19 это починит.

**P10. Order spawn / cancel API на SquadService.**

Расширение `SquadService`:
```go
// IssueOrder создаёт Order-сущность, вешает на squad как новый head либо
// аппендит в конец цепочки если append=true. Если append=false и у squad'а
// уже есть head — текущая цепочка отменяется (Cascade Cancel). Возвращает ID
// нового Order'а.
func (s *SquadService) IssueOrder(squad ecs.Entity, kind components.OrderKindCode,
    target components.WorldPos, entityTarget ecs.Entity, append bool, params OrderParams) ecs.Entity

// CancelAllOrders отменяет head + всю цепочку. Используется когда player жмёт
// H (Stop) на squad'е.
func (s *SquadService) CancelAllOrders(squad ecs.Entity)
```

`OrderParams` — структура с опциональными per-kind полями (Facing, Loop, etc.).

Существующий `OrderMoveTo` остаётся как wrapper над `IssueOrder` (для backward-compat и хоткеев), но внутри строит Order-сущность. `Stop` остаётся как wrapper над `CancelAllOrders`.

**P11. LOD fix для длинных путей (ISSUES #4).**

`UnitMovementSystem.LODPolicy()`:
```go
return core.LODPolicy{
    ActiveEvery:   0,
    RelevantEvery: 250 * time.Millisecond,
    DormantEvery:  500 * time.Millisecond,  // было LODDisabled
}
```

Это значит юниты в Dormant-tier (>120 м от anchor) обновляются 2 раза в секунду. Достаточно чтобы Order дошёл до выполнения; не дорого даже на большом числе entity. На сцене Phase 11 (12 юнитов) — мизер; при росте до 100+ юнитов добавим hash-bucket distribution (GAMEDESIGN cross-cut).

**Возможный побочный эффект**: junit на Dormant tier'е с long path делает большие шаги (500 мс × 5 м/с = 2.5 м за step). Это может привести к «прохождению сквозь» тонкие препятствия (заборы, прозрачные стены). Phase 11 не решает — реалистично для Dormant tier'а, фикс через interpolation на финальном подходе — Phase 17+ при появлении технических деталей.

**P12. Pie menu при ПКМ-hold.**

UI-механизм:
- ПКМ-press → запустить таймер `holdStart = time.Now()`.
- Если ПКМ ещё нажата через 200 мс → активировать pie menu mode.
- В pie mode рисовать колесо вокруг `holdStart`-cursor'а с 5 сегментами (по числу OrderKindCode в MVP).
- Cursor отъезжает от центра → выбран ближайший сегмент.
- ПКМ-release в pie mode → commit выбора через resolveRMBOrder с kind override.
- ПКМ-release в pie mode при cursor в центре (< inner radius) → cancel.

В pie mode hit-test resolver игнорируется — приоритет у explicit выбора. Available kinds могут быть disabled (например, Garrison сегмент серый если рядом нет building'а). MVP: все сегменты всегда доступны, кидают приказ с возможным `OrderState = Blocked` если target невалиден.

Реализация — `ui/pie_menu.go`. Рисуется поверх любой панели (3D или Map) — pie menu это overlay уровня всего экрана.

---

## Семь мильстоунов

### M11.1 — Order entity types + Resolver scaffold + MacroPath refactor

**Цель.** Order как ECS-сущность существует. OrderQueueHead на Squad'е. OrderResolverSystem проматывает lifecycle. SquadMacroPathSystem читает текущий Order вместо `MacroPath.HasGoal/Goal`. На сцене ничего не изменилось геймплейно — игрок всё ещё даёт MoveTo, squad идёт, но под капотом — Order-сущность.

**Делаем:**

- `components/order.go`: все базовые компоненты (P1) + `OrderKindCode` / `OrderStateCode` enum.
- `components/squad.go`: добавить `OrderQueueHead`. `MacroPath.HasGoal/Goal/ReplanAt/LastPlanned` остаются (но больше не primary state — заполняются SquadMacroPathSystem'ом из текущего Order'а).
- `systems/order_resolver.go`: `OrderResolverSystem` — lifecycle transitions, head advancement, completion checks.
- `systems/squad_service.go`: `IssueOrder` / `CancelAllOrders`. `OrderMoveTo` → wrapper. `Stop` → wrapper.
- `systems/squad_macro_path.go`: filter добавляет `OrderQueueHead`. Логика «есть ли куда идти» меняется с `mp.HasGoal` на `head.First != 0 && kind in {MoveTo, ...}`. Target резолвится из Order.
- `main.go`: Phase 9 startup-spawn squads больше не вызывает `OrderMoveTo` (Phase 9 не вызывал, проверка); существующий ПКМ-handler через `resolveRMBOrder` теперь идёт через IssueOrder.

**Проверяем.** На сцене 3 squad'а, ПКМ даёт MoveTo. Inspector показывает «Order: MoveTo @ <pos>, InProgress». Squad доходит до target → Inspector «No order», Order-сущность исчезает через 1-2 тика. `world.Stats()` показывает Order-сущности в счёте entity (всего ~3-4 одновременно).

### M11.2 — MoveTo + DefendPosition + Inspector + map markers

**Цель.** Два простейших Order-типа полностью работают. На карте и в Inspector'е видно тип приказа, цель, прогресс. DefendPosition обозначается ромбом на карте, не Completed'ится сам.

**Делаем:**

- `systems/order_resolver.go`: implementation для `OrderMoveTo` (arrival check), `OrderDefendPosition` (no completion, optional Facing).
- `ui/map_render.go::drawOrderMarkers`: рисуем линию + иконку (dot для MoveTo, diamond для DefendPosition) с squad-color.
- `ui/inspector.go::drawOrderSection`: имя kind'а, target Pos, State, Progress.
- Hotkey draft: `D` для DefendPosition (выделил squad → нажал D в режиме «выбрать точку» → ЛКМ ставит). Опционально — простой вариант через pie menu в M11.6. В M11.2 — Phase 11-light: только `OrderMoveTo` через ПКМ; DefendPosition только через test-харнес (debug-spawn в main.go при старте — даём одному squad'у DefendPosition).

**Проверяем.** ПКМ на 3D → линия на карте от squad'а к target'у, точечка-иконка. Достижение target'а → Inspector обновляется на «No order», иконка пропадает. DefendPosition (debug-spawn): squad доходит до точки, останавливается, иконка-ромб остаётся пока не Cancel'нули.

### M11.3 — Garrison + OccupyTrench + Hit-test resolver

**Цель.** ПКМ на building → Garrison; ПКМ на trench → OccupyTrench. Иконки на карте (square для Garrison, triangle-down для OccupyTrench). Garrison считается Completed когда центр squad'а в footprint'е здания; OccupyTrench — когда центр на trench-polyline.

**Делаем:**

- `command.go`: добавить `HitTestResult` + `resolveTargetIntoOrder` + интеграция в `resolveRMBOrder`. Hit-test для Building через `buildingFilter` итерацию по AABB2D. Hit-test для Trench через `trenches.Lines` и distance-to-polyline.
- `systems/order_resolver.go`: completion check для Garrison (центр в Footprint), OccupyTrench (центр в trench-radius). Per-kind target resolution: Garrison → footprint center, OccupyTrench → nearest polyline point.
- `ui/map_render.go`: добавить square / triangle-down icons.
- `ui/inspector.go`: показать entity target по имени (хеш Building или Trench).

**Проверяем.** Выделил squad, ПКМ на здание → squad идёт к зданию, иконка-квадрат на карте. Достижение → Completed. ПКМ на trench → иконка-треугольник, тот же flow. ПКМ на пустую точку — по-прежнему MoveTo.

### M11.4 — Patrol + Shift+RMB append + queue display

**Цель.** Patrol с циклом по точкам через цепочку `OrderChain.Next`. Shift+RMB апендит Order в очередь вместо замены текущего. Inspector показывает up to next 2 заказа в очереди.

**Делаем:**

- `systems/order_resolver.go`: `OrderPatrol` — после достижения target Completed (или повторного Issued если Loop). Loop реализуется через `OrderResolverSystem`: при Completed Patrol-приказа с Loop, head не очищается, target пере-Issued (или новая Order-сущность создаётся как копия).
- `command.go::resolveRMBOrder`: shiftHeld → `IssueOrder(append=true)`.
- `systems/squad_service.go::IssueOrder`: append-mode — пройти по цепочке от head'а через `OrderChain.Next` до конца, привязать новый Order как Next последнего.
- `ui/inspector.go`: дисплей queue.

**Проверяем.** Shift+RMB на трёх точках → squad идёт A→B→C, Inspector показывает «MoveTo @ A, then MoveTo @ B, then MoveTo @ C». При loop-Patrol (debug-spawn) — squad цикл бегает между точками.

### M11.5 — Multi-squad RMB → distributed orders (фикс ISSUES #3)

**Цель.** Marquee-select членов 2+ squad'ов + RMB → каждый squad получает свой Order, никто не вырывается из ростера. Soloists в том же selection получают per-unit MoveTo (Phase 7 поведение).

**Делаем:**

- `input.go::groupSelectionByOwner` — новая функция.
- `command.go::resolveRMBOrder`: новая логика по P9. Игнорирует homogeneous-проверку (она была compromise из Phase 9); теперь каждый squad — самостоятельный recipient.
- H hotkey (Stop) аналогично — итерирует SquadsToOrder и вызывает CancelAllOrders на каждом, плюс per-unit Stop для soloists.

**Проверяем.** Marquee всех 12 → ПКМ на цели → все 3 squad'а идут целиком в формациях к одной точке. Inspector multi-select показывает «3 squads, orders pending». До фикса: расформировывались в одиночек. После: целые squad'ы.

### M11.6 — Pie menu (RMB-hold)

**Цель.** Зажатый ПКМ через 200 мс открывает pie menu с 5 сегментами (MoveTo / Garrison / OccupyTrench / DefendPosition / Patrol). Cursor выбирает сегмент, release commit'ит.

**Делаем:**

- `ui/pie_menu.go`: state-машина (Idle / Active / Selecting), draw-функция.
- `main.go` input loop: интеграция в существующий ПКМ-handler. Press → запоминаем cursor + время. Hold > 200 мс → активируем pie menu. Release → если был активен — commit; иначе — обычный resolveRMBOrder (tap-режим).
- Pie menu запоминает target Pos с момента press (чтобы курсор мог уезжать в сторону сегмента не теряя target'а).

**Проверяем.** ПКМ-tap = старый flow. ПКМ-hold на пустой точке → pie menu появляется → выбрал Defend → squad идёт в эту точку как DefendPosition. ПКМ-hold с курсором в центре → release → cancel, никакого приказа.

### M11.7 — LOD fix для длинных путей + Phase polish

**Цель.** Squad с приказом «иди за 200 м» доходит независимо от того, где anchor. ISSUES.md #4 закрыт. Прочие мелкие polish-итерации (типы иконок, padding в Inspector'е).

**Делаем:**

- `systems/unit_movement.go`: `LODPolicy.DormantEvery = 500 * time.Millisecond`.
- Verify через профайлер: tick-overhead не вырос значимо. На 12 юнитах разница неуловимая; будущий рост покрываем hash-bucket distribution когда понадобится.
- Polish: проверка что иконки orders на карте видны при всех zoom-level'ах (минимальный pixel-size icon'а, scaling). Inspector layout — fit без overflow. Pie menu — сегменты equally distributed, текст-метки читаются.

**Проверяем.** Squad с приказом MoveTo на 250 м. WASD anchor подальше (>120 м от squad'а) — squad продолжает двигаться (медленнее, но идёт). Order Inspector показывает прогресс. Squad достигает target → Completed. Это и есть закрытие ISSUES #4.

---

## Что считаем «закрытием Phase 11»

- `Order` ECS-сущность с lifecycle (Issued/InProgress/Blocked/Completed/Cancelled/Failed). 5 типов: MoveTo / Garrison / OccupyTrench / DefendPosition / Patrol.
- `OrderQueueHead.First` — primary order state на Squad'е. `MacroPath.HasGoal/Goal` — derived/cached.
- `OrderResolverSystem` + расширенная `SquadMacroPathSystem` обрабатывают Order'ы.
- ПКМ-tap → hit-test resolver выбирает kind (MoveTo / Garrison / OccupyTrench).
- ПКМ-hold (>200 мс) → pie menu, явный выбор kind'а.
- Shift+ПКМ append в очередь через `OrderChain.Next`.
- Multi-squad selection RMB → каждый squad получает свой Order (без расформирования). Soloists — per-unit MoveTo.
- Order markers на 2D-карте (линия + per-kind иконка).
- Inspector показывает текущий Order + next 2 в очереди.
- `H` (Stop) отменяет всю цепочку приказов squad'а.
- ISSUES #3 закрыт (multi-squad не расформировывается).
- ISSUES #4 закрыт (UnitMovementSystem Dormant-tier @ 500 мс).
- ISSUES #1 и #2 — остаются в ISSUES.md, не блокируют закрытие Phase 11.

Pipeline: `... → ground_stick → unit_movement → vision → order_resolver → squad_macro_path → formation → lod → ...`.

После этого — обновление ROADMAP, Phase 11 → ✅, переход к Phase 12 (Unit roles).

---

## Заметки на полях

- **`MacroPath` остаётся, не выпиливается.** Логически это «кэш A*-пути для текущего Order'а». Поле `MacroPath.Goal` дублирует `OrderTarget.Pos` для удобства SquadMacroPathSystem'а (читать одно поле вместо chain'а Order → Target). `MacroPath.HasGoal` дублирует `OrderQueueHead.First != 0 && currentOrderKind in {moving kinds}`. Это маленький redundant copy, но он держит SquadMacroPathSystem узким — не нужно знать про OrderKind enum'ы.

- **`OrderState.Code = Issued` важен как сигнал на immediate replan.** Когда `IssueOrder` создаёт новую сущность с состоянием Issued, SquadMacroPathSystem видит «свежий приказ» через OrderResolverSystem'овский сброс `MacroPath.ReplanAt = 0`. Это решает проблему «нажал ПКМ — squad не сразу пошёл, ждёт следующий 1-секундный replan-tick».

- **OrderResolverSystem — самостоятельная, не часть SquadMacroPathSystem'а.** Lifecycle transitions (Issued → InProgress, head advancement, cleanup завершённых сущностей) — отдельная responsibility. SquadMacroPathSystem делает A*-плэн. FormationSystem пишет ActionQueue. Разделение даёт легче добавление новых Order types в Phase 12+ без правки трёх систем.

- **Per-kind completion checks в OrderResolverSystem, не в SquadMacroPathSystem.** Это значит OrderResolverSystem должен уметь считать `SquadCenter` сам (нужен posMap + rosterMap handle). Дёшево, эта же логика в FormationSystem / SquadMacroPathSystem уже.

- **Garrison completion как «центр в footprint'е» — упрощение MVP.** Реалистично — после того как squad дошёл до здания, надо ещё **войти**, занять окна (Phase 15 window distribution). В Phase 11 — просто «достиг footprint'а» = Completed. Squad стоит у входа, формация натянута на footprint. Window distribution делают AI Phase 15, когда у нас есть SurvivalInstinct + CoverEvaluation. Сейчас просто визуальный indicator «squad дошёл до здания».

- **OccupyTrench как «центр на polyline в radius'е 2 м» — то же упрощение.** Реалистично распределить вдоль длины окопа — Phase 15. Сейчас просто «squad подошёл к окопу» = Completed.

- **DefendPosition Facing.** В P1 описан как опциональный `OrderParamFacing`. В Phase 11 он не используется (FormationSystem не читает Facing напрямую — Forward всё ещё derive'ится от макро-пути). В Phase 13 при появлении RoE / sectors — Facing будет читаться как «куда смотрят бойцы», задаст RoE.SectorRestriction. На уровне Phase 11 — поле есть, никто не читает (как RadioNetwork в Phase 9).

- **Patrol Loop — простой вариант: повторное Issued при Completed.** Если флаг `Loop=true` на последнем приказе patrol-цепочки, OrderResolverSystem не очищает head, а пере-выдаёт первый Order цепочки (или новый Order с теми же params). Это даёт цикл без рекурсивных Chain.Next. Альтернатива — циклическая цепочка (Tail.Next = Head). Я за повторное Issued — проще обрабатывать abort.

- **Order'ы сохраняются на диск?** В Phase 11 — нет. Save/Load для Order'ов — Phase 25 (Polish). При перезапуске squad стартует idle.

- **OrderCleanupSystem нужен?** В отдельной системе — нет. OrderResolverSystem делает `world.RemoveEntity(orderEnt)` при advancing head'а с Completed/Cancelled. Это эквивалент cleanup'у в один проход.

- **Pie menu локальный, не глобальный state.** Active-state живёт в локальной переменной `pieMenuState` в main.go (как `marqueeActive`, `mapPanning`). После M11.6 pie menu — это просто ещё один input mode, не отдельный singleton resource. Если когда-нибудь понадобится несколько pie menu одновременно (Phase 22 UI L4 polish), refactor'нём в `*ui.PieMenu`.

- **Multi-squad один target в MVP — visual issue?** Все squad'ы целятся в одну точку — будут «толпиться» по приближении. Это видно, но не критично. Phase 19 (Multi-squad coordination) добавит auto-distribution (полукруг охвата / spread). До Phase 19 — игрок может выделять squad'ы по-одному и давать отдельные точки.

- **ISSUES #1 (squad-маркеры jitter) фикс — Phase 21 UI expansion или раньше.** Реализация: в `ui/map_render.go` хранить `lastSquadPositions map[ecs.Entity]rl.Vector2`, lerp от last к current с factor ~0.15 каждый кадр. ~10 строк кода, можно сделать прямо в M11.7 polish если останется время.

- **ISSUES #2 (Tab лаг) фикс — Phase 21 / 22.** Realloc RT при swap — корректное место это L3/L4 framework (Phase 22) где layout-changes становятся first-class событием. До тех пор живём с лагом, при frequent Tab'ах помогает workaround: предзагружать обе RT при старте (Field-size + Command-size), просто переключать active.

- **Phase 12 (Roles) разблокируется этим.** Roles будут влиять на Order'ы (Engineer может выполнять Build-приказ, Medic — Heal). Сами Order types Build/Heal — Phase 18 / 25, но gate «у squad'а должен быть Engineer для Build-приказа» появится в Phase 12 P-decision через `Order` API (check `unitsInSquad.HasRole(Engineer)` перед IssueOrder).
