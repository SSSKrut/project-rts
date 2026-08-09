# Phase 18 - UI L4 + Plan timeline + Undo/Redo (рабочий план)

После Phase 13.5 PanelManager умеет два пресета (Field / Command), drag splitter'ов (Main / Right), save/load `layout.json`. После Phase 14/15/17 у нас работают reactive units + order graph + RoE; Inspector умеет inline order timeline (3 строки head+queued). После Phase 16-17.5 здания и юниты выглядят прилично.

Phase 18 закрывает три самостоятельные боли:

1. **Layout L4** - произвольная топология панелей (tree-of-splits, tabs, floating, presets), не фиксированная 2×2-сетка.
2. **Plan timeline panel** - полноценный git-tree вид всех squad orders во времени (не inline-3-строчник Inspector'а), открывается снизу как Blender timeline.
3. **Undo/Redo** plan edits.

**Цель.** В конце фазы:
- Игрок видит compact toolbar сверху (T+mm:ss, pause, speed) и timeline снизу (squad rows + order blocks с прогрессом + время-gap'ы). Может тащить splitter timeline'а вверх/вниз, schwap'ать пресеты Field/Command, splittить любую панель пополам, drag'ать вкладку между slot'ами, undock'нуть во floating window.
- Ctrl+Z откатывает последнюю plan-edit (IssueOrder / CancelOrder / DragWaypoint), Ctrl+Y возвращает. AI-driven completion / failure не undoable (это симуляция, не player edit).

ROADMAP §18 - исходник. COMMAND-MODEL.md §3 (Orders) - producer'ы для timeline'а уже есть. layout_persistence.go - база для presets.

---

## Структура фазы

Семь tracks. **Tracks 18.0 и 18.A критичны для видимого MVP** ("compact toolbar сверху, timeline снизу") - они закроют explicit request юзера и дают полезный UX даже без последующих tracks. 18.C-G - чистый refactor + advanced features, можно растянуть на несколько коммитов.

- **18.0** Layout reshape - PanelTopBar (32 px сверху) + PanelTimeline (~180 px снизу), pre-existing PanelTime удаляется. Layout-функции `layoutField/Command` пересобираются на 4 строки grid'а.
- **18.A** Plan timeline rendering - squad rows + order blocks с прогресс-баром + state-color, time-gaps между orders preserved. Reads существующие `OrderQueueHead + OrderChain + OrderProgress + OrderState` без новых компонентов на squad (history - см. 18.A.3).
- **18.B** Timeline interactions - click block = camera fly-to + Inspector highlight; horizontal scroll wheel; hover tooltip.
- **18.C** PanelManager tree-of-splits refactor - recursive `LayoutNode = Leaf{PanelID} | Split{Orient, Children, Ratio}`. Удаляет hard-coded splitGrid + два SplitterID; добавляет рекурсивный hit-test.
- **18.D** Tabs внутри slot - `Leaf` хранит `[]PanelID`, не один; tab-bar над content rect; drag tab → relocate.
- **18.E** Floating windows - PanelID может жить вне tree как `FloatingPanel{Bounds, ID}`; drag tab вне dock zones = undock.
- **18.F** Layout presets - extend `layout.json` до versioned schema with named layouts.
- **18.G** Undo/Redo - `PlanEditHistory` resource + `PlanEdit` interface с `Inverse()`. SquadService.IssueOrder/CancelAllOrders/Push waypoint pushает entry; Ctrl+Z вызывает Inverse.

Порядок: 18.0 → 18.A → 18.B → (18.C → 18.D → 18.E → 18.F) → 18.G. Tracks в скобках - один связный refactor (split-tree без tabs/floating - half-done state), их лучше делать одной волной. 18.G технически независим, но его UX лучше работает поверх 18.B (clicking undone order's block в timeline = full feedback loop).

---

## Track 18.0 - Layout reshape

### Решения, которые лочим

**0-P1. Top toolbar - отдельная панель `PanelTopBar`, не часть chrome.**

Альтернативы:
1. **(*) Отдельная панель `PanelTopBar` поверх существующего PanelManager grid.** Recompute резервирует 32 px сверху, остаток отдаёт грид'у. Time controls рендерятся в его ContentRect (или без chrome — см. 0-P3). Преимущество: PanelManager + saveLayout уже умеют работать с одним лишним rect'ом, минимум изменений.
2. **Меню-bar в chrome.go.** Глобальный strip, не панель. Минус: ломает паттерн "всё в layout", из chrome не получится таскать кнопки в др. panel slot (для 18.D-E).

Выбираем (1). Имя `PanelTopBar` (не `PanelMenu` - в дальнейшем туда же повесим menu-bar, статус AI, alerts).

**0-P2. Bottom timeline - переиспользуем PanelTime ID, переименовываем в `PanelTimeline`.**

PanelTime в текущем коде - 64 px bottom strip с pause/speed. Все его читатели (DrawTimePanel, panel iteration в main.go) уйдут. Имя ID меняется для ясности; константа `timeBarHeight` тоже уезжает.

**0-P3. Top bar - без DrawChrome, без title.**

64 px chrome (1 px border + 20 px titleBar + 1 px) сожрёт половину 32 px toolbar'а. Render идёт прямо в Panel.Bounds; DrawChrome'а для PanelTopBar не вызываем (loop в main.go skip'ит этот ID). Timeline получает обычный chrome (Title="Timeline").

**0-P4. Bottom panel height = 180 px по умолчанию, mutable через splitter.**

Пять squad-row'ов высотой ~30 px + 20 px header bar = 170 px. Округляем до 180. Стандартный inspector мин ~100 px тоже соблюдается. Splitter между central row и Timeline (горизонтальный) - см. 0-P5.

**0-P5. Один новый горизонтальный splitter - `SplitterTimeline`.**

Между средним рядом (3D + Inspector + Map) и timeline'ом. Drag меняет новый ratio `TimelineRatio float32` (0..1, доля Timeline от высоты под top bar'ом). Старый SplitterRight (внутри side column) остаётся. Старый отсутствующий "bottom timebar splitter" - не существовал, height был константой.

После 18.C tree-of-splits эти SplitterID энумы выкидываются, hit-test - рекурсивный. На 18.0 - один новый ID, минимальное изменение.

### Milestones

**M18.0.1.** Расширить `PanelID`: добавить `PanelTopBar`, `PanelTimeline`. Удалить `PanelTime`.

**M18.0.2.** В `ui/layout.go`: переписать `splitGrid` под четыре строки:
- top: PanelTopBar, height = `topBarHeight = 32`
- middle big: Panel3D / PanelMap (rest of width - rightCol)
- middle side: PanelInspect (top) / PanelMap or Panel3D (bottom)
- bottom: PanelTimeline, height = `defaultTimelineHeight * TimelineRatio`-driven

Удалить `timeBarHeight const`, добавить `topBarHeight = 32`, `defaultTimelineRatio = 0.20`.

**M18.0.3.** PanelManager: новый `TimelineRatio float32`, NewPanelManager seed = 0.20. Recompute учитывает.

**M18.0.4.** SplitterID: добавить `SplitterTimeline`. SplitterAt: учесть горизонтальную полосу между central и Timeline (Y = screenH - timelineH ± grabRadius). BeginDrag/UpdateDrag/EndDrag: ветка для TimelineRatio.

**M18.0.5.** Перенести DrawTimePanel в новый `ui/topbar.go`:
- Не зовём DrawChrome.
- Layout: T+mm:ss (left, 14px) → pause/play icon → speed label (1x/2x/4x/8x/PAUSED) → hotkey hint (right, dim).
- Без отдельного "icon" - compact text "▶ 1x" / "⏸ PAUSED" + clock.

**M18.0.6.** Расширить layout.json: добавить `TimelineRatio` поле, bump version → 2. loadLayout - читает с дефолтом, saveLayout - пишет.

**M18.0.7.** В main.go:
- panel iteration loop `[]PanelID{Panel3D, PanelMap, PanelInspect, PanelTime}` → `[Panel3D, PanelMap, PanelInspect, PanelTimeline]` (PanelTopBar в loop'е без chrome - отдельная ветка).
- DrawChrome iterator skip'ит PanelTopBar.
- `ui.DrawTimePanel(...)` → `ui.DrawTopBar(...)` с тем же TimeDisplay.
- Заглушка `ui.DrawTimelinePanel(panelTimeline, ...)` пока пустой content rect - перейдём к ней в 18.A.

**M18.0.8.** Visual smoke test: запустить, проверить:
- Toolbar 32 px сверху, clock T+ + pause icon + speed читаемы.
- 3D/Inspector/Map занимают середину, размеры адекватные.
- Timeline panel - пустая, видна снизу с title "Timeline".
- Splitters: Main (vertical) + Right (horizontal в side col) + Timeline (horizontal между middle/bottom).
- Tab toggle Field/Command работает.
- saveLayout пишет TimelineRatio после drag'а splitter'а.

### Closure

После M18.0.8 - старый PanelTime ушёл, новая топология видна. Phase 18.A может начинать рисовать timeline content.

---

## Track 18.A - Plan timeline rendering

### Решения, которые лочим

**A-P1. Источник данных - живой order graph + опциональный history.**

Сейчас orders живут от Issued до Completed/Failed/Cancelled; resolver их despawn'ит (point cleanup). Для timeline нужны:
- **Текущие orders.** Все Squad с `OrderQueueHead.First` + walk OrderChain.Next. Достаточно для head + queued; УЖЕ работает в inspector_orders.
- **История (опциональная).** Phase 18.A.0 ride a без истории - timeline показывает только "сейчас + очередь". Phase 18.G undo/redo даст operational history (PlanEditHistory), её можно проецировать на timeline. Полную history (completed/failed records) — отложим на 18.A.3 если нужно.

Lockи: 18.A MVP = live orders only. История - 18.A.3 / 18.G.

**A-P2. Squad row layout.**

```
+--------------------------------------------------------+
| Header: T+00:00 . . . T+01:00 . . . T+02:00 . . .      | <- time axis
+--------------------------------------------------------+
| Recon-1  [MoveTo 80% ][Garrison wait]                  | <- head + queued
| Recon-2  [DefendPos 30%]                               |
| Assault  [AttackTarget 100%]<gap><[Patrol queued]>     |
+--------------------------------------------------------+
```

- Y: 24 px header + 30 px per squad. Vertical scroll если squads > N.
- X: pixels-per-second axis. По умолчанию 1 sec = 8 px → 1 min = 480 px. Wheel zoom между [2..40] px/s.
- "Now" line = vertical bright cyan в текущем X. Header tick'и каждую 10s short / 60s long.
- Order block: x = IssuedAt → x + estimated_duration; height = row - 4 px padding; inner filled bar = progress.
- Цвет block'а - per OrderKind (палитра из existing inspector_orders).
- "Gap" между orders - blank space, не renders'я.

**A-P3. Estimated duration на order'е - евристика по kind + distance.**

Нет точного duration на самом order'е (resolver не считает). Heuristic:
- MoveTo: `distance / squad.AvgSpeed` (AvgSpeed = 5 m/s placeholder).
- DefendPosition / Garrison: пока нет duration → fixed 30s placeholder, потом дольше "active".
- AttackTarget / SuppressFire: 30s (Phase 14 spec cap для SuppressFire).
- Patrol: TBD - chain length × MoveTo estimate.

Это **дисплейная** оценка, не симуляционная. Если real duration больше - block extends past estimated, progress bar fills slower. Если меньше - block ends на progress=1 и следующий начинает.

**A-P4. Squad имя - из SquadIdentity, цвет - из squad palette.**

Inspector_squad уже умеет читать `SquadIdentity{Name}` + `SquadColor`. Timeline reuses тот же palette helper.

### Milestones

**M18.A.1.** Pure-data renderer `ui/timeline.go`:
- `TimelineData` struct - snapshot, который main.go строит из ECS: `[]TimelineSquadRow{Name, Color, Orders []TimelineOrderBlock{Kind, State, StartT, EndT, Progress}}`.
- `DrawTimelinePanel(panel, font, data, viewStartT, pixelsPerSec, nowT)`.

**M18.A.2.** Заполнение TimelineData в main.go (после input phase, до render):
- Walk all Squad entities (Squad + SquadIdentity + OrderQueueHead).
- Для каждого: walk chain, build []TimelineOrderBlock с estimated_duration.
- Sort по SquadID для stable order.

**M18.A.3.** (опционально) Минимальная history: при completion order'а resolver не уничтожает entity сразу, а добавляет `OrderArchived{CompletedAt}` маркер; despawn через 60s. Timeline показывает archived orders блёкло, перечёркнуто. Может уехать в Phase 19/20 - не критично для MVP.

**M18.A.4.** Тестируем визуал: один Recon squad с цепочкой MoveTo → DefendPosition (Shift+RMB). Видим два block'а, прогресс первого, gap до второго, progress fill в realtime.

### Closure

После M18.A.4 - timeline читается, юзер видит планы всех squad'ов в одном кадре. 18.B добавит interactivity.

---

## Track 18.B - Timeline interactions

### Решения, которые лочим

**B-P1. Hit-test на TimelineOrderBlock.**

При фокусе PanelTimeline, click на block:
- LMB-click: camera fly-to OrderTarget.Pos (анимированный или snap - см. B-P2) + Inspector переключается на squad, скроллится на этот order.
- Hover: tooltip с kind, state, target, progress%, IssuedAt T+mm:ss.
- RMB on block: контекст меню - Cancel / Insert before / Insert after (insert требует Phase 18.G plan-edit инфра, на 18.B оставляем заглушки "TODO").

**B-P2. Camera fly-to - мгновенный snap для 18.B MVP.**

Smooth fly-to требует CameraSystem extension (target Pos + duration + easing). 18.B - просто `anchorPos = target.Pos`. Smooth fly-to - 18.B.2 если будет время, иначе 18.B.4 mark'аем deferred.

**B-P3. Horizontal scroll - mouse wheel внутри panel.**

Wheel up/down → pan timeline X-axis. Shift+wheel → zoom (pixels-per-second). По дефолту now-line в правой трети экрана; pan может двигать её в любое место.

### Milestones

**M18.B.1.** Hit-test: `TimelineHitTest(panel, viewState, cursor) → (squadEnt, orderEnt, ok)`. Зовётся при LMB в PanelTimeline focused.

**M18.B.2.** LMB on block → input layer ставит selection = squad + anchorPos = target.

**M18.B.3.** Hover tooltip - небольшой rect под cursor с 4 строками.

**M18.B.4.** Horizontal wheel pan + Shift+wheel zoom. Save TimelineViewState (offsetT, pixelsPerSec) в PanelManager.

**M18.B.5.** RMB context menu на block - stub Cancel/Insert (18.G допиливает Insert).

### Closure

После M18.B.5 timeline стал operational tool, не только indicator.

---

## Track 18.C - PanelManager tree-of-splits refactor ✅

### Решения, зафиксированные

**C-P1. Recursive LayoutNode (ui/layout_tree.go).**

```go
type LayoutNode struct {
    Kind     NodeKind        // Leaf | Split
    Panel    PanelID         // valid когда Leaf
    Title    string          // отображаемый заголовок Leaf
    Bounds   rl.Rectangle    // заполняется Recompute
    Orient   SplitOrient     // valid когда Split: Vertical / Horizontal
    Ratio    float32         // valid когда Split, 0..1 = доля первого child
    Children [2]*LayoutNode  // valid когда Split
    Parent   *LayoutNode     // обратная ссылка для merge / sibling
}
```

`PanelManager.Workspace *LayoutNode` заменил пятиэлементный `Panels []Panel`. TopBar остался отдельным фиксированным Panel'ом сверху, не в дереве. Recompute выдает Bounds через `node.Compute(rect)` рекурсивно.

**C-P2. Splitters = указатели на Split-узлы.**

`type SplitterID = *LayoutNode`. SplitterAt walks `WalkSplits`, проверяет в grab-radius (6 px) от divider-линии (внутренней грани первого child). UpdateDrag клампит ratio к panelMinW/H обоих children. SplitterNone = nil.

**C-P3. TogglePreset = swap содержимого, а не пересборка дерева.**

Tab свапает `Panel`+`Title` у Leaf(3D) ↔ Leaf(Map) через `SwapPanels`. User-edited layout (corner-splits, merge'и) сохраняется.

**C-P4. Corner-drag в bottom-right углу каждого Leaf'а (Blender-style).**

12×12 px hit-зона с тремя диагональными штрихами как handle. Press LMB → BeginCornerDrag. Direction решается по большему |dx|/|dy|:
- горизонтальный drag → SplitVertical (left/right children)
- вертикальный drag → SplitHorizontal (top/bottom children)

Drag должен превысить `cornerCommitPx=8` чтобы commit'ить. На CommitCornerDrag: `SplitLeaf(leaf, orient, ratio, originalSide)` оборачивает leaf в новый Split, второй side получает duplicate (тот же PanelID, тот же Title — пользователь сам поменяет через chevron).

**C-P5. Layout v3 — сериализация дерева.**

`layout.json` v3: `{version, layout_preset, tree: {kind, panel|orient, ratio, children}}`. Старые v1/v2 файлы инвалидируются → defaults применяются. Дерево пишется на каждом EndDrag / corner-commit / chevron-action которые меняют layout.

### Milestones (закрыты)

M18.C.1. LayoutNode + helpers (`Compute`, `WalkLeaves`, `WalkSplits`, `FindLeaf`, `LeafAt`, `SplitLeaf`, `MergeIntoSibling`, `SwapPanels`).
M18.C.2. PanelManager rewrite на дерево, API `Get(id)`/`Recompute`/`FocusedAt`/`ScrollByID`/`SplitterAt`/`BeginDrag`/UpdateDrag/EndDrag/AbortDrag сохранён.
M18.C.3. Corner-drag (ui/corner.go) — handle drawing + hit-test + commit.
M18.C.4. Layout persistence v3 (layout_persistence.go).
M18.C.5. Wire main.go: cursor-priority chain (drag → corner-drag → splitter → chevron → corner-grab → default), `chromeBusy()` closure гасит LMB у content-handler'ов.

### Closure

Tree visible через chrome (border + title + chevron); splitter drag двигает Ratio; corner-drag создаёт новый Split с preview-линией; layout.json v3 переживает рестарт.

---

## Track 18.D - Per-leaf widget switch (без tabs) ✅

### Решения, зафиксированные

**D-P1. Chevron-button в title bar.**

`▾` глиф 22×24 px в правом краю title bar'а каждого workspace leaf (ui/chrome.go::ChevronRect + drawChevron). Click → opens popup menu.

**D-P2. Popup menu (ui/menu.go).**

`ChevronMenu` state — Open, Leaf, Anchor, Items. Items список = `WorkspacePanelKinds` (4D / Map / Inspector / Timeline) как Switch-items + Close pane. Текущий widget помечен `• ` и disabled. Close pane disabled когда leaf == root (нельзя merge с несуществующим sibling).

**D-P3. Switch = swap, не дублирование.**

Если target widget уже в дереве — `SwapPanels(thisLeaf, otherLeaf)`. Если не в дереве — просто `leaf.Panel = target`. Гарантия: каждый PanelID живёт в дереве максимум один раз. Это требование от scene3DRT / map camera (single instance).

**D-P4. Close pane = merge с sibling.**

`MergeIntoSibling(leaf)` поднимает sibling на место parent'а, отвязывая leaf от дерева. Если parent был корнем — sibling становится новым корнем через `SetWorkspace`. Phase 18.D не делает табы — для multi-widget-per-slot см. backlog 18.D.tabs.

### Milestones (закрыты)

M18.D.1. ChevronRect + drawChevron в chrome.go.
M18.D.2. ChevronMenu в ui/menu.go (Open/Close/HitItem/Draw).
M18.D.3. main.go::handleMenuItem (Switch via SwapPanels, Close via MergeIntoSibling).
M18.D.4. Cursor priority + lmbConsumed-style gate (`chromeBusy()` closure).

### Closure

Click chevron → menu выезжает, выбор swap'ает leaves либо merge'ит pane. Layout пишется в layout.json после каждого изменения. Tab по-прежнему swap'ает 3D ↔ Map content.

---

## Track 18.D.tabs (отложено) - Tabs внутри leaf

Один Leaf с `Tabs []PanelID`. Tab bar 20 px над content rect (либо вытесняет existing title bar). Click → активный PanelID меняется, content от active рисуется. Drag tab → relocate в др. Leaf или undock (18.E).

Milestones TBD - сначала смотрим запрос пользователя; пока chevron-switch покрывает основной use case.

---

## Track 18.E - Floating panels ✅

### Решения, зафиксированные

**E-P1. Reusable in-game floating window (не OS-level).**

`ui/floating.go` - `FloatingPanel{ID, Title, Bounds, MinW, MinH, Render FloatingRenderFn, OnClose func()}` + `FloatingState` manager. Render-callback получает `(content, cursor, font, lmbPress) bool` (returning true = close). Любой UI который не вписывается в workspace tree (formation editor, asset picker, dialogs) живёт здесь.

**E-P2. UX контракт.**

- Title-bar (24 px): title-text + X-button справа.
- Drag за title → move, clamp к экрану (минимум 40 px видимости).
- Bottom-right grip (14 px, diagonal-stripe glyph) → **resize**. MinW/MinH из FloatingPanel, max — screen size.
- LMB на любую часть → raise to top of Z (slice last = topmost).
- ESC → close topmost.
- X-кнопка → close + fire OnClose.
- HitTest top-down; `IsBusy(cursor)` true при hover ИЛИ active drag/resize.

**E-P3. Input приоритет.**

`floating.HandleInput` зовётся **первой** в input phase. Возвращает `floatingConsumed` для гейтинга lmbDown. `overFloating = floating.HitTest(cursor) != nil` блокирует workspace-chrome LMB когда cursor над флоатером. `floating.IsBusy(cursor)` входит в `chromeBusy()` → content-handlers (selection / marquee / inspector clicks / topbar) тоже молчат под флоатером.

**E-P4. Render order.**

`floating.DrawAll` рисуется ПОСЛЕ chevron menu, ДО pie menu. Каждая панель — drop-shadow + bg + border + title-bar + X-button + resize-grip; content rendered внутри `BeginScissorMode` content rect.

### Closure

Reusable `ui/floating.go` для произвольных popup-window'ов; используется formation editor'ом. Resize / move / raise / close работают; chromeBusy интегрирован.

---

## Track 18.G - Dock/Float interop ✅

Возникло из user feedback после 18.F: "сделать панель построения dockable + остальные панели — floatable". Расширение чевронного меню + общий рендерер для floating/workspace flavours.

### Решения

**G-P1. PanelFormation как первоклассный workspace widget.**

Добавлен `ui.PanelFormation` в `WorkspacePanelKinds` + `WidgetTitle`. Тот же `FormationEditor`-инстанс рендерит и в workspace leaf (`DrawPanel`), и в floating panel (`Render`). Singleton живёт в main.go, чтобы zoom/kind/CustomSlots сохранялись при re-dock.

**G-P2. SelectionFn callback в FormationEditorCtx.**

`SelectionFn func() ecs.Entity` — необязательный hook. Когда задан, editor каждый кадр rebind'ит `e.Squad` к текущему общему squad'у selection'а (через `groupSelected`). Возврат zero entity = "оставь как есть" (no flicker). Когда squad умирает / снимается selection — editor рисует placeholder "Select a squad" вместо self-close (только в SelectionFn-mode).

**G-P3. Float pane через ChevronMenu.**

`MenuItemFloat` в `ui/menu.go` (label "Float pane", между switch-block и Close pane через separator). Disabled для root leaf и для `Panel3D` (RT привязан к Panel3D bounds — отложили separate-RT до Phase 19).

`handleMenuItem` принимает дополнительный callback `floatSpawn func(id, title, bounds)`. Сначала вырезает leaf через `MergeIntoSibling` (как Close pane), потом зовёт callback. main.go-`floatSpawn` открывает `FloatingPanel{ID: "float:"+id, Bounds: leaf.Bounds, Render: dispatch}` — bounds совпадают с pre-tear-out местом, чтобы окно появилось "на месте".

**G-P4. ContentToPanel adapter.**

`ui.ContentToPanel(content rl.Rectangle, title string, id PanelID) Panel` — inverse `ContentRect`: даёт synthetic Panel такой что `ContentRect(synthetic) == content`. Позволяет переиспользовать workspace draw'ы (`DrawInspector`, `DrawMap`, `DrawTimelinePanel`, `FormationEditor.DrawPanel`) внутри floater без рефактора их сигнатур. Floater сам рисует chrome (title-bar + border + close + resize), widget рисует только content.

**G-P5. renderFloatingWidget dispatch.**

main.go-closure switch'ит по PanelID и собирает per-widget context (mapCtx / inspectorCtx / etc.) каждый кадр. Все вспомогательные данные (`selected`, `hovered`, `mapCam`, ...) захватываются по reference из enclosing scope — никаких глобалов.

**G-P6. Input shield на floating panels.**

`chromeBusy()` теперь блокирует:
- map pan/zoom (`focused == PanelMap && !chromeBusy()`);
- orbit input (`OrbitInputEnabled` AND'ится с `!floating.IsBusy(cursor)`);
- RMB orders (`!floating.IsBusy(cursor)`).

Wheel внутри floater'а попадает только в его widget (formation editor zoom, etc.); ни map zoom, ни inspector scroll не реагируют.

**G-P7. Tunable formation editor zoom.**

`FormationEditorCtx` теперь имеет `MinPxPerM` / `MaxPxPerM` / `SnapMeters` опциональные поля. Built-in defaults: 2 / 200 px/m (расширили нижнюю границу с 8 → 2 чтобы wide vehicle / loose формации fit'нулись), snap 0.5 m. Ring step ladder расширили до 25 / 50 m для глубокого zoom-out.

### Milestones

M18.G.1. `PanelFormation` + `WorkspacePanelKinds`/`WidgetTitle` + `FormationEditor.DrawPanel` adapter.
M18.G.2. `SelectionFn` + drawEmpty placeholder в Render.
M18.G.3. `MenuItemFloat` + chevron меню rendering / disable rules.
M18.G.4. `ContentToPanel` helper + `renderFloatingWidget` dispatch + `floatSpawn` closure.
M18.G.5. `handleMenuItem` принимает floatSpawn callback.
M18.G.6. Input shield: `!chromeBusy()` на map zoom + `!floating.IsBusy(cursor)` на orbit + RMB.
M18.G.7. Editor zoom params (`MinPxPerM`/`MaxPxPerM`/`SnapMeters`) + расширенный ring-step ladder.

### Closure

Workspace formation panel доступен через chevron → Formation (или в стартовой раскладке заменой existing leaf). `E` всё ещё toggle'ит floating editor. Любой нон-3D widget превращается в floater через chevron → Float pane (leaf сворачивается в sibling, окно появляется в его bounds). Все floater'ы блокируют клики/wheel под собой. Editor zoom теперь доходит до 2 px/m (≈ 60-метровый радиус half-canvas) для размещения растянутых формаций.

### Backlog (next pass)

- **Panel3D как floater**: завести отдельный RT поверх default'ного, переключаемый по `Bounds`. Откладывается до P19 (vehicle dummies сильнее нагрузят 3D + перформанс ревью).
- **Float panel chrome busy-cursor**: middle-mouse pan / inspector scroll внутри float'а не работают (gate'ы привязаны к workspace `focused`). После 18.G floating-only режим работы Map / Inspector — read-only view.
- **Layout persistence для floating panels**: сейчас закрыл окно через X → пропало. Нужно сохранять `[]FloatingState` в layout.json (Phase 18.G.1 backlog).
- **Re-dock**: drag floater title в workspace leaf → leaf становится Split с widget. Сейчас только manual через chevron.

---

## Track 18.F - Formation editor + presets ✅

### Решения, зафиксированные

**F-P1. Новые компоненты.**

`components/squad.go`:
- `FormationOrientationMode` enum (`OrientMovement` default | `OrientNorth`).
- `FormationOrientation{Mode}` - optional component; absence = OrientMovement.
- `FormationCustomSlots{Slots [SquadRosterSize]rl.Vector2}` - per-slot offsets в squad-local frame (X=right of forward, Y=along forward, метры). Когда есть — FormationSystem использует вместо kind-based.
- `FormationPreset{Name, Count, Slots}` + `FormationPresets{List}` - resource для сохранённых раскладок, общий для всех squad'ов.

**F-P2. FormationSystem интеграция.**

В `processSquad`:
- OrientNorth → forward override на `(0, 0, 1)`.
- CustomSlots != nil → `customSlotWorld(slot, forward)` вместо `FormationOffset(kind, ...)`.

`customSlotWorld` проецирует local-frame в world-XZ через right/forward basis (тот же что FormationOffset).

**F-P3. Editor UX.**

`ui/formation_editor.go`. Открывается F-hotkey'ем на выбранном homogeneous squad'е в плавающее окно (`floating-editor` ID, 360×400 default, resize'абельно). Layout:

- **Header (32 px)**: `[Formation] [Kind: Loose ▾] [Movement | North]`.
- **Canvas**: концентрические кольца (адаптивный step 0.5/1/2/5/10 м под текущий zoom), кросс-оси, компас-стрелка сверху (label "N" в North-mode, "MARCH" в Movement-mode).
- **Member dots**: цвет = role color, slot-index внутри. Slot 0 (commander) — квадрат вместо круга. **Drag** любого dot мутирует CustomSlots (snap 0.5 m, clamp к viewport-радиусу). Slot 0 залочен для infantry; разблокируется для mixed squad'ов (vehicles, Phase 19+) — placeholder `allowsCommanderDrag` сейчас всегда false.

**F-P4. Zoom.**

Mouse wheel внутри canvas → scale `e.PxPerM` (per-editor field), factor 1.15 / wheel-tick, clamp [8, 120]. На первом frame seed'ится так чтобы 6m fit в canvas. Ring step выбирается через `chooseRingStep(maxMeters)` — всегда 5-9 видимых rings.

**F-P5. Kind/preset dropdown.**

Click на "Kind: X" — popup ниже кнопки (flip вверх если не помещается):
- **Predefined**: Line / Column / Wedge / Loose. Apply → `fd.Type = k`, `fd.Spacing = formationSpacingDefault(k)`, удалить CustomSlots.
- **Presets**: список сохранённых FormationPreset'ов. Apply → copy `p.Slots` в CustomSlots squad'а.
- **Save current as preset**: snapshot текущих effective slots (CustomSlots если есть, иначе kind-based) → append к `Presets.List` с auto-name "Preset N".

Label кнопки = "Custom" когда CustomSlots present, иначе имя kind'а.

**F-P6. Auto-formation на CreateFromUnits (T-key).**

В main.go T-hotkey:
- `containsVehicle(selected, world)` (placeholder = false до Phase 19).
- All-infantry → `FormationLoose` (свободное построение).
- Mixed → `applyPreservedSlots` snapshot'ит pre-merge world-positions относительно commander'а в CustomSlots + ставит `OrientNorth` (формация не крутится при движении, юниты сохраняют относительные позиции).

### Milestones (закрыты)

M18.F.1. Components + resource (`FormationOrientation`, `FormationCustomSlots`, `FormationPreset`, `FormationPresets`).
M18.F.2. FormationSystem honor orientation + custom slots.
M18.F.3. Editor виджет: header + canvas + dots + drag.
M18.F.4. Zoom + adaptive ring step.
M18.F.5. Slot 0 lock (infantry-only) + `allowsCommanderDrag` placeholder.
M18.F.6. Kind/preset dropdown menu + Save current button.
M18.F.7. main.go: F-hotkey open/close, T-hotkey auto-formation.
M18.F.8. Floating panel resize (track 18.E) integrated.

### Closure

F открывает editor для homogeneous squad'а; resize окна + zoom canvas работают; toggle Movement/North мгновенно меняет behavior в поле; drag dot пишет CustomSlots; dropdown переключает между kinds и сохранёнными presets; "Save current" появляется в dropdown'е других squad'ов.

### Backlog (next pass)

- **Vehicle detection** для `containsVehicle` / `allowsCommanderDrag` (ждёт Phase 19).
- **Preset persistence** в файл (сейчас live только в текущей сессии).
- **Preset rename / delete** в editor (сейчас только auto-name).
- **Squad selector** в editor (сейчас editor залочен на squad открытия — для другого squad'а нужно F снова).

---

## Track 18.E - Floating windows

`FloatingPanel{ID, Bounds, ZOrder}`. Список таких не в LayoutNode tree, рисуются last. Drag header → move. Двойной click → re-dock в ближайший Leaf. Phase 25 OS undock - отдельный issue.

Milestones TBD.

---

## Track 18.F - Layout presets

`layout.json` v3: `presets map[string]LayoutNode + active string`. UI dropdown в TopBar. Save current → named preset.

Milestones TBD.

---

## Track 18.G - Undo/Redo plan edits

### Решения, которые лочим

**G-P1. Resource `PlanEditHistory`.**

```go
type PlanEditHistory struct {
    Stack    []PlanEdit  // applied edits, growing
    Cursor   int         // [0..len(Stack)] - everything < Cursor is "live"
    MaxDepth int         // ring, default 128
}

type PlanEdit interface {
    Apply(world *ecs.World, squadSvc *SquadService) error
    Inverse() PlanEdit
    Describe() string
}
```

Конкретные impl'ы: `IssueOrderEdit`, `CancelOrderEdit`, `InsertOrderEdit`, `DragWaypointEdit`, `DoctrineChangeEdit`, ...

**G-P2. SquadService - сервис, не системы, пушит edit'ы.**

`SquadService.IssueOrder(...)` теперь зовёт `history.Push(IssueOrderEdit{...})` который внутри сам зовёт application logic. Old direct callers миграются единообразно.

**G-P3. AI-driven mutations - не undoable.**

Resolver completion / SurvivalInstinct overrides / Stance autonomy НЕ пушат в history. История - только player intent.

### Milestones

TBD после 18.0-B. Гранулярность M18.G.1 = define + push for `IssueOrderEdit` only → проверка цикл'а Ctrl+Z+Y → расширение до остальных edit kinds.

---

## Closure всей фазы

Closure-критерий: запустить игру, провести 5-минутный run:
- Toolbar сверху виден и читаем.
- Timeline снизу показывает 4 squad'а с цепочками orders, прогресс в realtime.
- Click block на timeline → camera прыгает к цели, inspector подсвечивается.
- Tab swap presets Field/Command сохраняется в layout.json.
- (после 18.C+) drag splitter любого узла - panel resize'ся.
- (после 18.D+) drag tab между slot'ами.
- (после 18.G) Ctrl+Z откатывает Issue/Cancel; Ctrl+Y возвращает.

ROADMAP §18 закрыт. Mark phase as done в ROADMAP, переместить PHASE-18.md в `.claude/old/`.
