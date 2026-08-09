# Phase 13.6 — рабочий план

Ghost preview & facing-drag: Total War-style визуализация намерения перед выдачей приказа. ПКМ-hover (без нажатия) → ghost-юниты появляются у курсора в текущей формации squad'а, лицом в сторону движения от squad center. ПКМ-hold + drag → drag-вектор задаёт facing-yaw (для DefendPosition / arrived-facing для MoveTo) — ghost rotate'ит вместе с курсором. На release вектор фиксируется как `OrderParamFacing` на спавнящемся Order'е. Formation-aware ghost placement: на здании → Garrison-распределение по окнам; на trench polyline → ghost-точки вдоль polyline; на земле → стандартная формация Squad.FormationData.

UX-полировка input'а — финальный шаг перед Phase 14 Combat. Не блокирует Combat; не вводит новых system'ов / component'ов кроме minimal extension OrderParamFacing reading в UnitMovementSystem (для arrived-facing при MoveTo).

**Что в Phase 13.6 сознательно НЕТ.** Multi-squad ghosts (несколько ghost-групп на одном курсоре) — Phase 19 (Multi-squad coordination). Map view ghosts (2D-проекция ghost'ов) — Phase 21 UI expansion. Реальное Garrison cover-slot distribution (greedy ranking по `dot(window.CoverDir, threatDir)`) — Phase 15 SurvivalInstinct / Phase 14 cover evaluation. Phase 13.6 показывает greedy-first-N окон без threat-awareness. DefendPosition sector enforcement (юниты обязаны держать оружие в указанном sector'е) — Phase 14 / Phase 15 weapon system. Phase 13.6 — только visual hint sector arc'ом; реальный sector-based RoE — Phase 14 EngagementRules.SectorYaw/SectorHalfDot. Per-unit ghost color по роли — Phase 21 polish (уже есть `RoleColor` palette из Phase 12, но в Phase 13.6 ghost'ы neutral grey alpha для visual-noise reduction). Animated ghost transition (smooth rotate / lerp) — Phase 25 polish. Patrol ghost (line of waypoints) — Phase 21. Vector orders для multi-squad spread — Phase 19. Per-Unit ghost при single-unit selection (без squad) — Phase 21; Phase 13.6 — squad-only.

ROADMAP — высокоуровневый трекер. COMMAND-MODEL.md §6 — спецификация UX-механизмов input'а. Этот файл — рабочий план фазы.

---

## Решения, которые лочим до начала кода

**P1. Ghost preview always-on при cursor over Panel3D + selected squads exist.**

Не требует modifier'а / зажатой клавиши. Default behavior: если `len(selected) > 0` AND cursor over Panel3D AND кому-то из selected соответствует Squad — ghost рисуется continuously (per-frame). Reduce visual noise через low alpha (см. P3).

Альтернатива (отложена): требовать Alt-hold для preview. Менее intuitive, требует игроку запоминать modifier. Phase 13.6 — always-on; если playtest показывает что raw'ит — Phase 21 добавит toggle.

**P2. Ghost рендерится только в Panel3D (не в Map) в Phase 13.6.**

3D — основной surface для placement intent. Map ghost (2D circle при ПКМ-hover на карте) — Phase 21 UI expansion. Фокусирует Phase 13.6 на том слое где formation visualization наиболее ценен.

При cursor over PanelMap текущий Phase 11 поведение (RMB → resolveRMBOrder без preview) сохраняется неизменным.

**P3. Ghost visual: low-alpha cube + neutral color.**

Body cube alpha = 80 (~31% opacity), color = `rl.Color{R: 200, G: 200, B: 220, A: 80}` (cool grey-white). Без role tinting / cap cube — ghost'ы visually subdued чтобы реальные юниты оставались dominant. Border-line + 1px wires alpha=120 — outline для visibility.

Sector arc (DefendPosition): alpha=60 fill polygon, alpha=180 outline lines. Color matches squad color (existing `squadColor(squad.ID())` palette из Phase 12) для distinction между squad'ами.

**P4. Drag threshold для facing-drag = 8 px (existing PieMenu.DragCancelThreshold).**

Reuse existing constant. Below 8 px: no facing intent — pie menu может open'нуться (>200ms hold) или tap (release) триггерит default order.

8+ px drag: facing-drag mode активна. Cursor delta normalized → facing yaw. PieMenu suppressed (не открывается даже после 200ms). Camera orbit suppressed для этой RMB session (см. P5).

**P5. RMB-drag conflict resolution: facing-drag wins при selection, camera-orbit при no selection.**

Currently OrbitSystem ловит RMB-down безусловно (gated по OrbitInputEnabled global flag). Phase 13.6 — главное изменение: при `len(selected) > 0` AND ПКМ-press в Panel3D, OrbitInputEnabled = false для этой RMB session (until release). Это отдаёт RMB-drag в facing input.

Без selection — current behavior (RMB-drag = camera orbit). Так player может крутить камеру когда никто не выбран.

main.go state: `rmbOwnedByFacing bool` — set on press (selection != empty + Panel3D), cleared on release. OrbitSystem.Update проверяет этот флаг через OrbitInputEnabled.

**P6. Facing computation: yaw от squad center к cursor (hover) ИЛИ от press origin к cursor (drag).**

Hover (без press): facing = yaw of `(cursor - squadCenter)` projected to ground. Squad смотрит «в сторону цели». MovementProfile-aware: если PathStyle = CoverSeek, facing showing approach direction ещё хороший hint.

Drag (RMB-press + cursor moved >8 px): facing = yaw of `(cursor - pressOrigin)` — игрок rotate'ит формацию вокруг target point указывая «откуда подход». Это semantic difference: hover = «куда идёт», drag = «как ориентируется по прибытии».

При release-as-commit: drag-derived facing записывается в `OrderParamFacing` на новом Order'е.

**P7. `OrderParamFacing` extended: применяется при arrival для MoveTo (новое), не только DefendPosition (existing).**

Phase 11 P-decision: OrderParamFacing.YawRad применялся только DefendPosition (юниты разворачиваются после прибытия в указанном направлении). Phase 13.6 расширение: UnitMovementSystem при completion ActionMoveTo (last waypoint reached) — если параметр Order'а имеет OrderParamFacing → set Unit.Motion.Yaw к facing yaw.

Реализация: при попадании в OrderResolverSystem completion path (Order → Completed), check OrderParamFacing component; если есть — pass yaw в FormationSystem (или прямо в Unit'ы через ActionStance pattern). Для Phase 13.6 simple: на Order completion, walk Squad ростер, set Motion.Yaw для всех. Stops feeling jarring через будущий Phase 25 smooth interpolation.

**P8. Per-kind ghost placement: 4 случая, simple version.**

| Order kind | Ghost placement |
|---|---|
| MoveTo (default — cursor over terrain) | `formationOffset(kind, slot, spacing, facing)` от cursor — стандартная формация |
| Garrison (cursor over building) | Distribute units to first-N building windows (greedy by index) |
| OccupyTrench (cursor over trench polyline) | Equal-spaced points along polyline (length / (count+1) interval) |
| DefendPosition (Alt+RMB или pie menu choice) | Formation как MoveTo + arc-sector indicator (default 90° wide) |

Garrison real cover-slot distribution (Phase 15) — first-N greedy достаточно для preview hint.

OccupyTrench polyline traversal: `accumulate(length(P[i]→P[i+1]))`, place units at `total_length × (slotIdx / (count-1))` waypoint.

DefendPosition arc: 2 lines из cursor center в направлениях `facing ± 45°` длиной 8 m + thin polygon fill между ними. Gives «sector vision» visual.

**P9. Multi-squad ghost: Phase 13.6 только для homogeneous selection.**

Если selection — один squad → ghost как обычно. Если selection — несколько squad'ов → Phase 13.6 показывает только first squad's ghost (homogeneous fallback). Phase 19 (Multi-squad coordination) добавит per-squad spread visualization (multiple ghost groups arranged по фронту).

Если selection — soloists (юниты без squad) → Phase 13.6 НЕ показывает ghost (fallback на текущее Phase 11 behavior). Phase 21 может добавить per-Unit ghost для soloists.

**P10. `FormationOffset` exported — для use из main.go ghost computation.**

Currently `formationOffset` (lowercase) — package-private в `systems`. Export как `FormationOffset(kind, slot, spacing, forward)`. main.go (или новый `ghost.go`) использует для ghost slot placement.

Alternative: добавить helper `FormationData.ResolveSlots(center, facing) []WorldPos` на компоненте. Это вариант для Phase 21 — Phase 13.6 simpler (export existing function as-is).

**P11. Ghost render performance budget — 8 ghosts × 1 squad = 8 cubes/frame.**

Single-select case (1 squad, 4-8 units) — trivially cheap (8 DrawCubeV + 8 wires). На 64 ghost'ах (8 squads × 8 units) при multi-select — также trivial для современного GPU. Frustum culling не нужен (ghost'ы near cursor — by definition в виду камеры).

Если performance замедлится в playtest (dell сценарий с 30+ юнитами) — distance-based culling ghost'ов > 100 m от camera. Phase 13.6 не оптимизирует preemptively.

**P12. Drag mode не reset'ит pieMenu state — drag complete'ит facing вместо open'нуть pie.**

При drag > 8 px PieMenu.Tick currently возвращает `ReleasedAsDrag` и обнуляет SourcePanel — drag goes к OrbitSystem. Phase 13.6 интервенция:
- Если selection != empty AND drag detected → не reset pieMenu, keep tracking drag direction для facing computation.
- На release: если drag > threshold → commit facing; иначе fall through к existing tap/hold paths.

Реализация: новый PieMenu state `inFacingDrag bool` set при `dragDetected && hasSelection`. Tick возвращает `ReleasedAsFacingDrag` outcome instead of `ReleasedAsDrag`. main.go обрабатывает: spawn Order with OrderParamFacing.

---

## Шесть мильстоунов

### M13.6.1 — Export `FormationOffset` + ghost render helpers

**Цель.** `formationOffset` exported as `FormationOffset(kind, slot, spacing, forward) (float32, float32)` from systems package. New file `ghost.go` (или helpers в `render_world.go`) с `drawGhostUnit(pos, stance, color)` и `drawGhostArc(center, facingYaw, halfAngleRad, length, color)`. На сцене ничего не меняется — helpers готовы к использованию M13.6.2.

**Делаем:**
- `systems/formation.go`: переименовать `formationOffset` → `FormationOffset` (export). Update all call sites внутри FormationSystem.
- `render_world.go` (или новый `render_ghost.go`): `drawGhostUnit(pos rl.Vector3, stance components.StanceCode, alpha uint8)` — placeholder cube без cap, low alpha. `drawGhostArc(center rl.Vector3, facingYaw, halfAngleRad, length float32, color rl.Color)` — 2 outline lines + filled triangle fan.

**Проверяем.** `go build ./... && go vet ./...` chисто. FormationSystem поведение unchanged (rename без semantic change). Smoke test: визуально неотличимо от Phase 13.5.

### M13.6.2 — RMB-hover ghost preview (default MoveTo)

**Цель.** При cursor over Panel3D AND selection contains a squad — ghost'ы отображаются у курсора в текущей формации, facing toward cursor от squad center. Continuous render каждый кадр. ПКМ ещё ничего не делает (что было — то и работает); это visual-only step.

**Делаем:**
- main.go в render pass (после composite 3D RT, перед label overlay): новая функция `drawSelectionGhost(selected, cursor, panel3DContent, ...)`.
- drawSelectionGhost:
  - Resolve selection → primary squad (homogeneous path; first squad если multi). If нет squad'а → no-op.
  - Compute squad center via existing `SquadCenter(world, roster, posMap)` helper.
  - Compute cursor target via `mouseTargetWorldPos` (existing).
  - Compute facing yaw = atan2(cursor.X - center.X, cursor.Z - center.Z).
  - For each slot in roster: `offX, offZ := FormationOffset(kind, slot, spacing, forward)`; ghostPos := cursorTarget + (offX, 0, offZ); `drawGhostUnit(ghostPos.ToRenderSpace(...), squad.Stance, alpha=80)`.
  - Skip rendering if cursor not in Panel3D.

**Проверяем.** Select 1 squad, hover cursor над terrain в Panel3D → 4-8 grey ghost cubes появляются у cursor'а. Перемещай cursor → ghost'ы следуют. Hover на Inspector → ghost'ы исчезают.

### M13.6.3 — Per-kind ghost placement (Garrison / OccupyTrench / DefendPosition)

**Цель.** `drawSelectionGhost` smart: hit-test cursor через existing `HitTester`, по результату показывает per-kind placement. Garrison → ghost'ы на первых N окнах здания. OccupyTrench → ghost'ы вдоль polyline. DefendPosition (default — без модификатора selection — пока не активен; будет в M13.6.4 через Alt+RMB drag) → standard formation.

**Делаем:**
- main.go: внутри drawSelectionGhost — `hit := hitTester.HitTest(cursorTarget)`.
- Switch по `hit.Kind`:
  - `HitBuilding`: walk Building's walls; collect first N walls с `OpeningKind == OpeningWindow`; place ghosts at window center positions (use existing wall `Yaw` + `Length` math для window center coord).
  - `HitTrench`: Read TrenchRoot.Index → TrenchNetwork.Lines[Index].Points. Compute total polyline length; place ghost at `total × ((i+1) / (count+1))` arc-length point.
  - `HitTerrain`: standard formation (M13.6.2 path).
- Helper `pointOnPolyline(points []WorldPos, t float32) WorldPos` — interpolation along polyline at param `t ∈ [0,1]`.

**Проверяем.** Select squad. Hover на trench → ghost-юниты появляются вдоль окопа. Hover на building → ghost-юниты в N первых окнах здания. Hover на terrain → standard formation. Trench polyline placement visually equal-spaced.

### M13.6.4 — Facing-drag mechanism + OrderParamFacing на MoveTo

**Цель.** ПКМ-press в Panel3D с selected squad → camera orbit suppressed для этой RMB session. ПКМ-down + drag > 8 px → drag mode active, ghost rotate'ится под facing-yaw из drag direction. ПКМ-release с drag → Order spawn'ится с OrderParamFacing (yaw = drag direction). UnitMovementSystem на arrival ActionMoveTo применяет yaw.

**Делаем:**
- `ui/pie_menu.go::PieMenu`: добавить bool field `InFacingDrag`, bool field `HasSelection` (set'ится в Begin). PieTickResult: добавить `ReleasedAsFacingDrag bool` + `FacingYaw float32`.
- PieMenu.Begin: новый аргумент `hasSelection bool`. Save в struct field.
- PieMenu.Tick: если drag > threshold AND HasSelection → set `InFacingDrag = true`; не reset SourcePanel (как сейчас); продолжать tracking. На release с InFacingDrag = true → return ReleasedAsFacingDrag + computed FacingYaw (atan2 cursor-origin delta in screen-space, mapped через camera projection). Если без HasSelection — current behavior (ReleasedAsDrag, cancel).
- main.go: pass `len(selected) > 0` в pieMenu.Begin. Handle ReleasedAsFacingDrag — call resolveRMBOrder with new OrderParams.HasFacing=true / FacingYawRad=facing.
- main.go: when InFacingDrag → ghost rendering uses facing from drag (cursor-origin delta) instead of cursor-from-center hover heuristic.
- main.go: gate OrbitSystem RMB. Add bool `rmbOwnedByFacing` in main.go: true while pieMenu InFacingDrag is set. OrbitSystem reads via global `systems.OrbitInputEnabled = !rmbOwnedByFacing`.
- `systems/order_resolver.go`: при `OrderKindMoveTo` completion (current logic — OrderState → Completed, advance chain), check OrderParamFacing component; if present, walk owner squad's roster и set Motion.Yaw для каждого юнита. Phase 13.6 — instant snap; Phase 25 may smooth.

**Проверяем.** Select 1 squad. ПКМ-press на terrain, drag cursor 50 px вправо → ghost формация rotate'ится так что squad смотрит вправо. Release → Order issued. Squad reaches goal → юниты повернуты лицом вправо (Motion.Yaw set'нут). Без selection — RMB-drag по-прежнему orbit'ит camera.

### M13.6.5 — Sector arc для DefendPosition + pie-menu wiring

**Цель.** Default ПКМ-tap всегда даёт MoveTo (или Garrison/OccupyTrench по hit-test). DefendPosition доступен через RMB-hold > 200ms → pie-menu commit. При hover в pie-mode hovering DefendPosition segment → ghost рисуется + sector arc indicator появляется ниже.

**Делаем:**
- PieMenu state: добавить optional `HoveringKind components.OrderKindCode` field (set по `segmentAt(cursor)` while Active). main.go ghost render reads this — если pie active AND hoveringKind == DefendPosition → render arc-sector indicator + standard formation.
- main.go drawSelectionGhost extended: case OrderKindDefendPosition — рисуем formation + drawGhostArc(cursorTarget, facing, π/4 (90° total), 8.0, squadColor).
- На pie commit с DefendPosition: spawn Order with OrderKind=OrderKindDefendPosition + OrderParamFacing (facing same as ghost rendering).

**Проверяем.** Select squad. ПКМ-hold (без drag) → pie menu open. Mouse'ом над "Defend" segment → arc-sector indicator поверх ghost'а. Release с cursor on Defend → Order issued, squad идёт на cursor target и settles facing in sector direction.

### M13.6.6 — Closure: tune ghost alpha / threshold, archive, ROADMAP

**Цель.** Subjective playtest: ghost-feel приятный, не визуально шумный. Tune alpha (start 80, may shift к 60 если too prominent / 100 если too faint). Tune drag threshold если 8 px feels too sensitive. ROADMAP update Phase 13.6 → ✅, archive.

**Делаем:**
- Playtest single-squad MoveTo with facing-drag — feels intuitive?
- Playtest Garrison hover → ghost in windows. If first-N greedy looks weird (ghosts in random window order), tune ranking heuristic (maybe order by `wall.Pos.X + wall.Pos.Z` для consistent visual placement).
- Playtest OccupyTrench — ghost spacing too tight / too loose? Tune interval formula if needed.
- ROADMAP.md: Phase 13.6 → ✅; активная фаза становится Phase 14 (Combat core).
- PHASE-13.6.md → archive `.claude/old/PHASE-13.6.md`.

**Проверяем.** Combat scenario (or whatever current test scene) feels naturally readable. Ghost preview — accurate hint, не distraction. Map view (still no ghost — per P2) — unchanged.

---

## Что считаем «закрытием Phase 13.6»

- `FormationOffset` exported из systems package; usable из main.go / ghost render.
- `drawGhostUnit` + `drawGhostArc` helpers в render_world.go (или new render_ghost.go).
- `drawSelectionGhost(selected, cursor, ...)` — main.go render pass функция, draws ghost-формацию каждый кадр когда selection contains squad AND cursor over Panel3D.
- Per-kind ghost placement: HitBuilding → first-N окон greedy, HitTrench → equal-spaced polyline, HitTerrain → standard formation, DefendPosition (pie menu hover) → formation + arc sector.
- PieMenu расширен: `InFacingDrag`, `HasSelection`, `HoveringKind` поля + `ReleasedAsFacingDrag` outcome + `FacingYaw` поле.
- main.go: при facing-drag — OrbitSystem suppressed (`rmbOwnedByFacing`); при release — Order spawn с `OrderParamFacing.YawRad = drag-derived yaw`.
- UnitMovementSystem на arrival ActionMoveTo (last action pop): если parent Order имеет OrderParamFacing, apply yaw на all squad units' Motion.Yaw.
- OrderResolverSystem extended: при `OrderKindMoveTo` completion checks OrderParamFacing — applies arrived-facing yaw.
- Test scene: visually verifiable — single squad, RMB-press-drag on terrain → ghost rotates → release → squad arrives + faces drag direction.
- Pipeline: без новых system'ов; formation + ghost render — новый render-pass функция в main.go.

После этого — обновление ROADMAP, Phase 13.6 → ✅, переход к **Phase 14** (Combat core).

---

## Заметки на полях

- **Ghost не использует squad-color tinting в Phase 13.6.** Cool grey-white neutral. Cause: ghost — это «потенциальное намерение», не «команда». Squad-color tinting на ghost'ах может путать с уже-issued orders (которые на map markers рисуются squad-color'ом). Phase 21 может добавить subtle squad-color-tint если playtest показывает что multi-select ghost confusing без color hint.

- **Single-cube ghost vs full body+cap.** Phase 12 unit visual = body cube + role cap cube. Ghost — single cube без cap. Reasoning: cap уже differentiates real units; на ghost'ах добавляет visual noise. Если M13.6.6 playtest show игрок не понимает «это ghost vs real» — добавим cap.

- **Camera orbit conflict — P5 решение.** Если пользователь хочет orbit'ить камеру при выделенном squad'е — Shift+RMB (модификатор). Phase 13.6 не делает этой опции; orbit без selection — единственный путь. Phase 21 может добавить explicit modifier toggle. Workaround в Phase 13.6: deselect (LMB-tap empty terrain) → orbit → reselect.

- **OrderParamFacing на MoveTo — semantic difference vs DefendPosition.**
  - DefendPosition: facing = «куда смотрит overwatch sector» — постоянно держится в режиме defend.
  - MoveTo (arrived-facing): facing = «куда смотрит squad сразу после прибытия». После того как стало в позицию — formation поворачивается. Дальнейшие orders (другой MoveTo) могут изменить.

  Same field, разный semantic — реализуется per-OrderKind в OrderResolverSystem completion path.

- **Facing-drag yaw conversion: screen-space delta to world yaw.** При cursor-press cursor.X в screen coords; cursor delta (dx, dy) — screen-space. Need: world-space yaw vector from press origin to current cursor. Approach: project both cursor positions через `mouseTargetWorldPos` to terrain → compute yaw between them. This is what gives «point cursor → yaw rotates towards point» feel.

  Approximation для performance: только cursor direction relative to centerOfPress на screen — `atan2(dx, -dy)` — works because camera looks down at ground primarily. Phase 13.6 — start with screen-space approximation; if camera angles produce wrong feel, switch to world-space projection.

- **Garrison ghost — first-N greedy placement.** Real Garrison Order (Phase 11) at runtime distributes units to windows by `dot(window.CoverDir, threatDir)`. Phase 13.6 ghost не имеет threatDir info (нет combat ещё) — fallback на index ordering (windows как они лежат в filter query). Может выглядеть случайно — Phase 21 добавит deterministic stable ordering (e.g. sort by `wall.Pos.X+Z`).

- **OccupyTrench polyline — equal arc-length placement.** Compute total = sum of segment lengths. For unit i (0..count-1), place at `total × i/(count-1)`. Walk segments accumulating distance; находим segment containing target distance; lerp within segment to exact point. Standard polyline arc-length parameterization.

- **DefendPosition arc — visual hint, не enforcement.** Phase 13.6 рисует 90° wedge (45° each side of facing). Phase 14 EngagementRules.SectorYaw / SectorHalfDot fields (currently zero scaffold) become live readers — sector enforcement via WeaponSystem. Phase 13.6 ghost preview matches default 90° wide; player can't change in Phase 13.6 (no per-Order sector slider).

- **Ghost обновляется per-frame, не throttled.** Continuous tracking cursor через `mouseTargetWorldPos` (raycast) — 1 raycast per frame. Cheap. Phase 14 может intro spatial hash that further reduces — но Phase 13.6 не требует.

- **Multi-squad fallback в Phase 13.6.** Если 2+ squad'ов selected, ghost shows только first squad's формация. UI hint — small text "(multi-squad: showing leader)" below ghost? Probably overkill for Phase 13.6; document как known limitation, Phase 19 (Multi-squad coordination) implements proper per-squad spread.

- **`OrbitInputEnabled` global flag.** Currently set/cleared by main.go. Phase 13.6 sets it = `!rmbOwnedByFacing` per frame. Single owner of mutation chain. Cleaner pattern (Phase 22 polish): introduce `core.InputContext` resource with explicit flags per consumer (orbit / pieMenu / facingDrag / scrollDrag) — сейчас слишком много global flags.

- **Hover ghost рисуется даже когда Inspector open / focus on Inspector?** P1 says always-on when cursor over Panel3D. So when cursor moves into Inspector panel — ghost disappears (cursor not over Panel3D). When player navigates Inspector chips, ghost не distract'ит. Cursor returns to 3D — ghost появляется. Желаемое UX behavior.

- **Stance ghost — какой stance показывать?** Squad's effective MovementProfile.Stance (Phase 13). Если Stance = Prone, ghost cubes flat (height = 0.4m). Player видит «squad arrived in prone stance». Реализуется через existing `unitStanceHeight(stance)` reuse.

- **OrderParamFacing на arrived MoveTo — risk: Order completion already pops chain.** OrderResolverSystem already advances `OrderQueueHead.First → Chain.Next` on completion. Need to apply facing BEFORE the entity'ы are detached. Phase 13.6: read facing in completion handler before advancing head.

- **Pie menu segment label hint — Phase 13.6 может add facing arrow on Defend segment label.** Currently labels "Move / Garrison / Trench / Defend / Patrol". Defend segment label could show small ↑ arrow indicating facing intent. Polish; not blocker.

---

## Открытые вопросы (требуют решения по ходу M13.6.x)

1. **Arc width для DefendPosition default** — 90° (45° each side of facing) или 120° (60°)? Combat Mission uses ~120°, Sea Power uses ~90°. Default чисто estetic — finalize в M13.6.5 playtest.

2. **Ghost shows when pieMenu is active or hidden during pie?** Pie menu opens around cursor → ghost'ы ниже рядом могут conflict visually. Recommendation: ghost dimmed (alpha → 40) when pieMenu.Active. Hovering DefendPosition segment — ghost back to full alpha + arc.

3. **What happens for soloists in selection?** P9: not shown. Edge case: selection mixed (squad + soloist) — Phase 13.6 shows squad ghost, ignores soloist. Soloist gets per-Unit MoveTo to cursor (existing Phase 7 fallback). User probably won't notice — soloist case rare in actual play.

4. **OrderResolverSystem reading OrderParamFacing in MoveTo completion — performance impact?** Per-Order completion = once per Order. Trivial. No impact. Confirmed.

5. **Cursor jitter near edges / off-screen.** mouseTargetWorldPos may return invalid WorldPos when cursor off-terrain (sky raycast misses). Ghost should hide when no valid target. Defensive check `if !targetOK { skip ghost render }`.

6. **What's "facing yaw" when cursor is exactly at squad center (zero distance)?** atan2(0, 0) = 0 (defined behavior). Ghost facing = forward axis. Acceptable degenerate case — rare since squad center и cursor only collide momentarily.

7. **Test scene addition — нужно ли?** Phase 13.6 — pure UX. Existing test scene (3 player squads + 1 enemy from Phase 14 если applied) демонстрирует. No new scene work needed.

8. **Map view ghosts — really defer?** Counter-argument: показывать squad's планируемый position на карте интуитивно, especially при command preset. Но requires map-projection of formation (4-8 dot'ы вокруг cursor in 2D), additional code path. Phase 21 подходящее место когда Map UI получает другие polish'и. Defer ratified.
