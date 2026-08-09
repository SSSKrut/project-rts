# Phase 13.5 — рабочий план

UI L2 polish: scrollable содержимое Inspector + resizable splitter'ы между панелями + мини-persistence сплит-пропорций. Минимально-необходимый шаг от L1 к L3, bumped перед Phase 13.6 (Ghost preview) и Phase 14 (Combat) после того как Phase 12 (roles) + Phase 13 (quick-bars) переполнили Inspector. Цель — устранить текущий overflow без полной L4-infrastructure (split / tabs / undock / dock zones — Phase 22).

**Что в Phase 13.5 сознательно НЕТ.** Новые панели (Orders queue / Formation / Equipment / Event log) — Phase 21 L3. Blender-style split (ПКМ на угол → split на половинки) — Phase 22 L4-split. Tabs внутри панели — Phase 22 L4-split. Dock zones при drag + visual hints — Phase 22 L4-polish. Undock в floating window — Phase 22 L4-polish. Multi-monitor support — Phase 25. Layout presets save/load (несколько именованных раскладок) — Phase 22 L4-polish. Phase 13.5 сохраняет только текущую сплит-пропорцию, не пресеты. Scrollbar в Panel3D / PanelMap — не нужен (viewport panning purpose-built). Scrollbar в PanelTime — тоже не нужен (одна строка фиксированной высоты). Horizontal scroll — не нужен (контент любой панели вертикально-укладывающийся). Hover tooltips / drag thumbnails — Phase 21. Collapsible sections внутри Inspector — Phase 21.

ROADMAP — высокоуровневый трекер. GAMEDESIGN §9 — спецификация L1→L4 шкалы. Этот файл — рабочий план фазы.

---

## Решения, которые лочим до начала кода

**P1. Layout ratios становятся mutable struct fields на PanelManager.**

Текущие `rightColRatio` и `inspectorRatio` из `ui/layout.go` — package-level const'ы. Phase 13.5 переносит их на PanelManager как ratio-fields:

```go
type PanelManager struct {
    Panels  []Panel
    Layout  LayoutPreset
    focused PanelID
    // Phase 13.5 — mutable split ratios.
    RightColRatio   float32 // 0..1, side column's share of width (default 0.25)
    InspectorRatio  float32 // 0..1, inspector's share of side column height (default 0.4)
}
```

`layoutField` / `layoutCommand` / `splitGrid` принимают ratios как arg'и, не читают глобальные const'ы. Defaults остаются 0.25 / 0.4 — visual baseline unchanged.

Phase 22 расширит эту структуру до split-tree (несколько вложенных сплитов с произвольной топологией), но Phase 13.5 keeps it flat: один H-split + один V-split.

**P2. ScrollState per-panel — хранится на PanelManager как массив параллельный Panels.**

```go
type ScrollState struct {
    OffsetY       float32 // current scroll position (px from top)
    ContentHeight float32 // measured content height — set by panel content at end of draw
}

type PanelManager struct {
    // ...
    Scroll [4]ScrollState // parallel to Panels by index
}
```

Massив (не map) — четыре панели зафиксированы, индекс совпадает с `Panels[i]`. `m.ScrollOf(id)` / `m.ScrollByID(id)` — accessor helpers. Phase 13.5 only Inspector consumes; 3D / Map / Time leave `ContentHeight == 0` → scroll never активируется. Phase 21 включит другие при необходимости.

**P3. Scrollbar — overlay на правый край content rect, рисуется в `ui.chrome.go` поверх содержимого.**

Не часть Inspector рендера, а универсальный helper `DrawScrollbar(panel, scroll)`:
- Тонкий track (8 px wide) на right edge of content rect.
- Thumb size = `visibleHeight / max(visibleHeight, ContentHeight)`.
- Thumb pos = `OffsetY / max(1, ContentHeight - visibleHeight) * (trackHeight - thumbHeight)`.
- Скрыт когда `ContentHeight <= visibleHeight` (нет переполнения).

Click + drag на thumb — Phase 13.5 включает (wheel-scroll работает всегда, thumb-drag — для precision). Click on track outside thumb = page-jump (move by visibleHeight).

**P4. Wheel-scroll: focused-panel + cursor-over-content gating.**

В main.go input loop:
1. `wheel := rl.GetMouseWheelMove()` once per frame.
2. Если `wheel != 0` AND focused panel имеет `ContentHeight > visibleHeight`:
   - `scroll.OffsetY -= wheel * wheelScrollSpeed` (negative because positive wheel = scroll up = decrease offset).
   - Clamp `[0, ContentHeight - visibleHeight]`.

`wheelScrollSpeed = 30` px / wheel-tick (3 typical rows of Inspector at fontSize=14). Tunable константа.

Существующие consumers of `rl.GetMouseWheelMove()` (MapCamera zoom) — оставляем приоритет: если focused == PanelMap → wheel goes к MapCamera (existing behavior); если focused == PanelInspect → wheel goes к scroll.

**P5. Splitter detection: 2 splitters в Phase 13.5 (H-split + V-split).**

```go
type SplitterID uint8
const (
    SplitterNone SplitterID = iota
    SplitterMain      // vertical bar between big slot и right column (H-split)
    SplitterRight     // horizontal bar в right column между Inspector и side view (V-split)
)

func (m *PanelManager) SplitterAt(cursor rl.Vector2) SplitterID
```

Splitter hit-zone = 6 px wide (= grab radius — slightly wider than visual для usability). Visual splitter = 1 px line (current panel border line — Phase 13.5 не вводит explicit splitter visual, использует существующую границу панели).

Time-bar splitter (между Time strip и контентом выше) — Phase 13.5 НЕ делает: Time bar остаётся fixed 64 px tall. Если позже понадобится — добавим SplitterTime.

**P6. Drag mechanism: state-машина в PanelManager.**

```go
type PanelManager struct {
    // ...
    dragging       SplitterID
    dragStartXY    rl.Vector2
    dragStartRatio float32
}

func (m *PanelManager) BeginDrag(splitter SplitterID, cursor rl.Vector2)
func (m *PanelManager) UpdateDrag(cursor rl.Vector2, screenW, screenH int32) bool // returns true if dragging
func (m *PanelManager) EndDrag() (changed bool) // returns true if ratio mutated
```

main.go input loop:
1. Hover: detect splitter under cursor → SetMouseCursor(ResizeEW для Main / ResizeNS для Right).
2. LMB-press on splitter → BeginDrag.
3. LMB-down + dragging != None → UpdateDrag (recompute ratio from cursor delta).
4. LMB-release + dragging != None → EndDrag, returns true → trigger persistence save.

Cursor change uses `rl.SetMouseCursor(rl.MouseCursorResizeEW / ResizeNS / Default)`.

**P7. Min-size constraints applied during drag.**

```go
const (
    panelMinW int32 = 180 // Inspector / 3D / Map минимальная ширина
    panelMinH int32 = 100 // panel минимальная высота
)
```

`UpdateDrag` рассчитывает new ratio из cursor pos, потом clamp'ит так чтобы оба panel'а (с одной и с другой стороны splitter'а) удовлетворяли min size в текущем layout preset.

Phase 13.5 — single constraint pair (per splitter). Phase 22 split-tree introduces per-node minSize attributes.

**P8. Layout persistence: `save/layout.json` через encoding/json + atomic write.**

```go
// save/layout.json schema
{
  "version": 1,
  "right_col_ratio": 0.30,
  "inspector_ratio": 0.45
}
```

Path = `./save/layout.json` (parent dir уже gitignored через save/.gitignore).

Save: на `EndDrag` returning changed=true → atomic write (.tmp + rename) — mirror pattern для terrain chunks (`systems/persistence.go`). main.go owns the call (`saveLayout(panelMgr)`) — UI package не импортирует filesystem APIs.

Load: на startup, после `NewPanelManager`, ДО первого `Recompute`. main.go вызывает `loadLayout(panelMgr)`. Если файл отсутствует / parse error → silently fall back to defaults (no panic).

Phase 13.5 — single-layout persistence. Phase 22 расширит до multiple named presets.

**P9. Inspector becomes scrollable — рефактор `DrawInspector`.**

Current Inspector рисует с `y := contentRect.Y + padY` и пишет в `BeginScissorMode(contentRect)`. Phase 13.5 расширение:

1. Pull scroll state for PanelInspect from PanelManager.
2. Initial `y := contentRect.Y + padY - scroll.OffsetY` (subtract offset).
3. После всего рендера: `scroll.ContentHeight = y_final - (contentRect.Y - scroll.OffsetY)` — total used height. Это и есть «измерение» контента, которое затем feed'ит scrollbar.
4. Scissor on contentRect (existing — no change).
5. Outside scissor: `DrawScrollbar(panel, scroll)` рисует track + thumb если ContentHeight > visibleHeight.

InspectorCtx получает scroll handle (pointer) чтобы можно было записать ContentHeight по факту.

**P10. Splitter visual feedback — минимальный.**

Phase 13.5 не вводит explicit splitter graphics. Hover-cursor change — единственный visual hint. Phase 21 / 22 могут добавить highlighted splitter line при hover + drag-time visual feedback.

Это сохраняет Phase 13.5 sharp в scope. Если плейтест покажет что splitter'ы visually невыразительные — добавим 1-px highlighted line поверх существующей panel border на M13.5.6.

**P11. ScrollState reset на preset toggle (Tab).**

При смене Field ↔ Command layout, Inspector остаётся в той же позиции (его роль не меняется). Reset scroll НЕ нужен.

При resize окна (screenW / screenH change) — scroll preserves OffsetY если still в range; clamped иначе.

Defensive: на load layout.json invalid → reset both ratios to defaults (0.25 / 0.4).

**P12. Layout file location matches save/ pattern.**

`save/` уже gitignored. layout.json living там — no new gitignore needed. main.go saveLayout / loadLayout — short functions, no separate package.

`systems/persistence.go` уже имеет `SaveDir = "./save/world-default"` для chunks. Layout живёт one level above: `./save/layout.json` (не в `world-default/`) — это user-level setting, не world-specific. Phase 22 при multi-layout-presets возможно move layouts inside `save/layouts/`.

---

## Шесть мильстоунов

### M13.5.1 — Layout ratios → mutable struct fields

**Цель.** `rightColRatio` / `inspectorRatio` уходят из package consts в struct fields `PanelManager.RightColRatio` / `PanelManager.InspectorRatio`. `layoutField` / `layoutCommand` / `splitGrid` принимают ratios как параметры. No visual change — defaults 0.25 / 0.4 preserved. Compiles and runs identical pixel-for-pixel.

**Делаем:**
- `ui/panel.go`: добавить `RightColRatio float32`, `InspectorRatio float32` к PanelManager struct.
- `ui/panel.go::NewPanelManager`: init defaults 0.25 / 0.4.
- `ui/layout.go`: удалить `const rightColRatio` / `const inspectorRatio`. `layoutField` / `layoutCommand` принимают `(screenW, screenH, rightCol, inspector float32)`. `splitGrid` подобным образом.
- `ui/panel.go::Recompute`: передаёт `m.RightColRatio, m.InspectorRatio` в layout-функции.
- Build: `go build ./... && go vet ./...` clean. Smoke test: запустить, видеть тот же layout.

**Проверяем.** Pixel-perfect identity со старой версией (3D 75% / Inspector 25% × 40%). `Ctrl+P` snapshot panel counts — unchanged.

### M13.5.2 — Splitter detection + drag mechanism

**Цель.** PanelManager имеет API для определения splitter под курсором + state-машины drag. Cursor меняется на ResizeEW/ResizeNS при hover. LMB-drag меняет ratios live; release фиксирует. Min-size constraints применены. Без persistence (M13.5.5 займётся).

**Делаем:**
- `ui/panel.go`: `SplitterID` enum (None/Main/Right). `dragging SplitterID` + `dragStartXY rl.Vector2` + `dragStartRatio float32` поля.
- `ui/panel.go::SplitterAt(cursor) SplitterID`: hit-test проверяет — 6 px radius вокруг H-line (X = panelMain.right) и V-line (Y = panelInspector.bottom) в right column.
- `ui/panel.go::BeginDrag`, `UpdateDrag`, `EndDrag`: state-машина.
- `UpdateDrag`: рассчитывает new ratio из delta cursor vs dragStart; clamp по min-size (panel left of splitter ≥ panelMinW, right of splitter ≥ panelMinW, similar для V-split heights); mutates `RightColRatio` / `InspectorRatio`; вызывает Recompute internally.
- main.go input loop: после `focused := panelMgr.FocusedAt(cursor)`, перед всеми другими input handlers:
  - `splitter := panelMgr.SplitterAt(cursor)`
  - hover cursor: `if splitter == SplitterMain { rl.SetMouseCursor(rl.MouseCursorResizeEW) }` etc.
  - LMB press + splitter != None → BeginDrag, consume the LMB-press (mark flag to skip selection/marquee).
  - while dragging: UpdateDrag(cursor); skip selection / focused-panel logic.
  - LMB release: EndDrag → keep "changed" flag for M13.5.5 persistence trigger.
- main.go: track `splitterDragging bool` flag — guards selection / marquee code so dragging doesn't trigger a selection click.

**Проверяем.** Hover over splitter — курсор меняется. Drag the vertical splitter — Inspector / Map / 3D panels resize live. Drag horizontal splitter в right column — Inspector vs Map ratio меняется. Min-size: попытка сжать Inspector до 0 — ratio clamp'ится на panelMinW=180. Tab preset toggle во время dragging — ratio preserved.

### M13.5.3 — ScrollState infra + scrollable Inspector

**Цель.** PanelManager имеет `Scroll [4]ScrollState` параллельный Panels. Inspector рендер модифицирован: subtract OffsetY на entry, write measured ContentHeight на exit. Scrollbar overlay в правой части Inspector если переполнение. No input yet (M13.5.4 wheel). Initial OffsetY=0 — Inspector видится identically as before.

**Делаем:**
- `ui/panel.go`: `ScrollState{OffsetY, ContentHeight float32}` + `Scroll [4]ScrollState` field on PanelManager. `ScrollByID(id) *ScrollState` accessor.
- `ui/chrome.go::DrawScrollbar(panel, scroll)`: 8 px track на правом краю content rect; thumb sized + positioned per P3 formula; render только если ContentHeight > visibleHeight. Track color `inspectorTextDim`-ish, thumb color slightly brighter.
- `ui/inspector.go::InspectorCtx`: добавить `Scroll *ScrollState` field.
- `ui/inspector.go::DrawInspector`: 
  - `startY := contentRect.Y + padY - ctx.Scroll.OffsetY`
  - Pass `startY` через describeSelection helpers.
  - В конце: `ctx.Scroll.ContentHeight = endY - startY + ctx.Scroll.OffsetY` (total used height).
  - After EndScissorMode: `DrawScrollbar(panel, ctx.Scroll)`.
- main.go: pull `panelMgr.ScrollByID(PanelInspect)` and pass through InspectorCtx.

**Проверяем.** Inspector contents render identically when OffsetY=0. Debug: set scroll.OffsetY=50 manually — Inspector content shifts up 50 px, scrollbar appears on right edge with thumb at 1/4 down (if contentHeight ~200). Scrolled-off content clipped by scissor (already happens, no change).

### M13.5.4 — Wheel-scroll input + thumb drag

**Цель.** Wheel-scroll над Inspector меняет OffsetY. Clamped to valid range. Thumb-drag тоже работает (LMB-down на thumb → vertical drag меняет OffsetY proportionally). MapCamera wheel zoom preserved (not stolen by Inspector scroll).

**Делаем:**
- main.go input loop:
  - `wheel := rl.GetMouseWheelMove()` (single read).
  - If `focused == ui.PanelInspect && wheel != 0`:
    - `scroll := panelMgr.ScrollByID(ui.PanelInspect)`
    - `scroll.OffsetY -= wheel * wheelScrollSpeed` (wheelScrollSpeed = 30)
    - Clamp `[0, max(0, scroll.ContentHeight - visibleHeight)]`
    - Mark wheel "consumed" — MapCamera path checks consumption flag.
  - MapCamera path: only consumes wheel if focused == PanelMap AND wheel not yet consumed.
- `ui/chrome.go::ScrollbarHit(panel, scroll, cursor) bool`: returns true if cursor is on the thumb. Used by main.go for drag-detection.
- main.go: track `scrollbarDragging bool` flag + `scrollDragStartOffset float32`. LMB-press on thumb → start drag; LMB-down → update OffsetY proportionally to cursor delta * (ContentHeight / trackHeight); LMB-release → end drag.

**Проверяем.** Scroll wheel over Inspector — content moves up/down smoothly. Scroll over Map — Map camera zooms (no Inspector scroll). Click + drag thumb — Inspector scrolls proportionally. Reach top — wheel-up no-op. Reach bottom — wheel-down no-op.

### M13.5.5 — Layout persistence (save/layout.json)

**Цель.** Splitter drags persist to disk. On startup, ratios load from disk if file exists. Atomic write avoids corruption on crash. Layout file format versioned.

**Делаем:**
- `main.go` или новый `ui/layout_persistence.go`: 
  - `type layoutFile struct { Version uint16; RightColRatio float32; InspectorRatio float32 }` (JSON struct).
  - `saveLayout(panelMgr *ui.PanelManager) error`: marshal + atomic write to `./save/layout.json` (`.tmp` + os.Rename). Create `./save/` if missing.
  - `loadLayout(panelMgr *ui.PanelManager) error`: read file; if missing → return nil (defaults stay). If parse error / unsupported version → log warn + return nil. Else apply ratios via direct field write + `panelMgr.Recompute()`.
- main.go startup: `loadLayout(panelMgr)` right after `NewPanelManager()`, before first `Recompute`.
- main.go splitter logic: after EndDrag returns changed=true → call `saveLayout(panelMgr)`. Log error but don't crash (e.g. disk full).
- `defer saveLayout(panelMgr)` на shutdown (covers case where user resized but didn't trigger release — paranoid backup).

**Проверяем.** Drag the main splitter → close game → relaunch — layout восстановилось. Delete save/layout.json → relaunch — defaults (0.25 / 0.4) used. Corrupt save/layout.json (write random bytes) → relaunch — log warning + defaults used, no crash.

### M13.5.6 — Closure: defaults rebalance, doc updates, archive

**Цель.** Pass through subjective playtest of resized layouts to confirm UI behaves naturally. Subtle tweaks if needed (min-size, scroll speed, splitter grab radius). ROADMAP / GAMEDESIGN updated, PHASE-13.5.md archived, Phase 13.6 (Ghost preview) became next active.

**Делаем:**
- Playtest: try shrinking Inspector to min — Inspector contents still legible at min width. Try maxing Inspector to half-screen — 3D / Map still usable. Tune defaults if mid-point feels off.
- Tune `wheelScrollSpeed` (currently 30 px) — should feel "one wheel-click = ~3 rows". Adjust if too slow / fast.
- Tune `panelMinW` (currently 180 px) — Inspector at 180 may clip role chips. If yes — raise to 200 or 220.
- ROADMAP.md: Phase 13.5 → ✅; активная фаза становится Phase 13.6.
- PHASE-13.5.md → archive `.claude/old/PHASE-13.5.md`.
- Memory если есть новые patterns — update reference_design_docs.md if needed (probably not — layout persistence is standard).

**Проверяем.** Combat scenario (or whatever current test scene) feels useable across multiple panel proportions. Switch from 25%-Inspector to 35%-Inspector — quick-bars layout intelligently (chips may wrap to 2 rows naturally since drawChip widths are computed from content width).

---

## Что считаем «закрытием Phase 13.5»

- Layout ratios на PanelManager как mutable fields (RightColRatio / InspectorRatio).
- `Splitter` detection API в PanelManager (SplitterAt, BeginDrag, UpdateDrag, EndDrag).
- Hover курсора над splitter показывает resize cursor (ResizeEW / ResizeNS).
- LMB-drag splitter'а меняет ratio live с min-size constraint enforcement (panelMinW=180, panelMinH=100).
- ScrollState инфраструктура (`Scroll [4]ScrollState` parallel to Panels).
- Inspector рендерится с scroll offset; вертикальный scrollbar появляется при переполнении.
- Wheel-scroll над Inspector меняет OffsetY (wheelScrollSpeed=30 px / tick).
- Click-drag thumb scrollbar'а работает.
- MapCamera wheel zoom preserved (wheel routing по focused panel).
- Layout сохраняется в `save/layout.json` (atomic write) на каждом EndDrag.
- На startup loadLayout восстанавливает ratios; defaults применяются если файл missing / corrupt.
- На shutdown defer saveLayout как fail-safe.
- Pipeline systems и component'ы — без изменений (Phase 13.5 — чисто UI).

После этого — обновление ROADMAP, Phase 13.5 → ✅, переход к **Phase 13.6** (Ghost preview & facing-drag).

---

## Заметки на полях

- **Не вводим explicit splitter visual.** Phase 13.5 использует существующую 1-px panel border как splitter line. Hover-cursor change — единственный hint. Если playtest показывает что splitter'ы invisible — добавим 1-px highlighted line поверх border при hover в M13.5.6 patch.

- **Drag-resize должна disable'нуть selection / marquee.** Current main.go input loop: LMB-press начинает selection или marquee. Phase 13.5 нужно gate: `if splitterDragging { skip selection input }`. Аналогично pieMenu / camera-orbit — full guard list уточняется при имплементации M13.5.2.

- **Scroll vs camera-orbit.** Camera-orbit использует RMB-drag, не LMB-drag — не конфликтует со scroll-thumb drag. RMB-drag над Inspector — не имеет смысла (нет 3D scene'а), безопасно ignore.

- **`ScrollState.ContentHeight` is measured at frame N, used for clamping at frame N+1.** Технически off-by-one: если content height растёт за один frame (selection changes from single-unit to single-squad with longer roster), scrollbar thumb может быть на пиксель неправильным первый frame. На практике imperceptible. Fix — Phase 21 если станет заметно.

- **Atomic write через `.tmp + os.Rename`.** Standard pattern уже used by terrain persistence. Layout file = small (< 200 bytes), однократная запись быстрая. Не оптимизируем — save'им на каждом EndDrag (не debounced). Если playtest показывает write-stutter — debounce'м.

- **Layout file version field.** `Version uint16 = 1` for Phase 13.5. Future phases bump version и add fields. Old version → log warning + use defaults (forward-compat). Newer version в same major series → try parse + ignore unknown fields (через json.Unmarshal behavior by default).

- **Wheel-scroll routing.** Простой подход: focused-panel determines who gets the wheel. MapCamera уже использует focused == PanelMap gate (existing). Phase 13.5 добавляет focused == PanelInspect gate для scroll. Никакой priority queue не нужен — focused — single owner of wheel input.

- **Scroll bar visual.** Tiny — 8 px wide track, thumb ≥ 30 px tall (для grab). Color subdued: track ~`inspectorTextDim` 40% alpha, thumb ~`inspectorTextDim` 80% alpha, brighter on hover. Phase 21 polish может add hover highlight + thumb dragging color change.

- **Inspector ContentHeight computation.** Currently DrawInspector — switch statement по selection kind. Каждая branch updates y monotonically. Measured height = `final_y - (contentRect.Y + padY - scroll.OffsetY)`. Subtract `-scroll.OffsetY` since startY already had that offset. Простая arithmetic в DrawInspector прямо.

- **Persist on EndDrag, not on every UpdateDrag.** Saving JSON 60×/sec during drag = wasteful. EndDrag (LMB release) = exactly one save per drag — good cadence. Phase 22 при multiple-preset switching может revisit.

- **Phase 13.5 не трогает Phase 13 standing rules / quick-bars contents.** Inspector layout unchanged in terms of which sections show — Movement / Engagement / Behavior всё ещё в одном scroll'е. Future Phase 21 может wrap them в collapsible-sections, но Phase 13.5 — only scaffolding, не contents.

- **Save directory creation.** `./save/` already created by terrain persistence (Phase 2). Layout persistence reuses; defensive `os.MkdirAll("save", 0755)` в saveLayout — covers fresh checkout.

- **Map / Time / 3D panels — currently не scrollable.** Map имеет own pan/zoom; Time имеет fixed row; 3D имеет camera. Phase 13.5 sets up ScrollState for all four for uniformity, но только Inspector активно uses scroll.OffsetY. Others leave it at 0 (no harm).

- **Splitter grab radius vs visual width.** Visual border = 1 px; hit-zone = 6 px на каждую сторону splitter line. UX rule: grab area should be larger than visual line для usability (Fitts's law). 6 px is generous but not too greedy — won't interfere with content clicks 7 px from border.

---

## Открытые вопросы (требуют решения по ходу M13.5.x)

1. **Should other panels (3D / Map) also support resize-shrink below min?** Probably не нужно — 3D / Map хорошо себя feel'ят wide. Phase 13.5 enforces panelMinW=180 везде. Если playtest показывает что Map нужно sub-180 для side-by-side view — расширим в Phase 21.

2. **Visual splitter highlight on hover — yes/no in Phase 13.5?** Default: no (cursor change enough). If M13.5.6 playtest shows splitter feel invisible — add 1-px highlight in M13.5.6 patch. Decision deferred.

3. **Should scroll persist across sessions?** Не нужно — scroll is "transient" UI state (где игрок остановился). Layout proportions persist; scroll position resets to 0 on game restart. Same pattern как text editor: window size persists, scroll position обычно нет.

4. **What if loadLayout deserializes ratio outside [0..1]?** Clamp before applying. Защита от ручного editing файла. Same for invalid splitter constraints (e.g. ratio = 0.99 would make Inspector larger than min-size — re-clamped по min-size logic в Recompute).

5. **Tab preset toggle + dragging — what wins?** Tab takes priority — Tab during drag aborts drag (calls EndDrag without "changed" flag, ratios revert). Edge case unlikely in playtest. Decision: implement abort behavior in M13.5.2.

6. **Multi-monitor.** Phase 13.5 не handles multi-monitor (no floating windows). Window itself can be moved between monitors by user's window manager; ratios are screen-relative so scale correctly. ✓.

7. **Window resize event.** Existing main.go listens for window resize и calls panelMgr.Recompute. Phase 13.5 adds no new resize logic — ratios applied at Recompute time, scroll clamp re-applied automatically на next frame.
