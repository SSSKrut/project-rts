# Controls inventory

Снимок всех текущих способов ввода и кликабельных зон в проекте — на состоянии 2026-05-22 (Phase 17 закрыт, перед перепланированием контроля). Рабочий справочник для пересмотра схемы управления; не дизайн-doc, не план.

Источники: `main.go` (вход), `input.go`, `command.go`, `ui/*`, `systems/orbit_system.go`. Phase-теги указаны где были введены.

---

## 1. Глобальные хоткеи (работают при любом focused-панели)

| Клавиша | Действие | Где обработка |
|---|---|---|
| `Space` | Pause / Resume (toggle `app.TimeScale`) | main.go:1054 |
| `+` / `=` / Numpad `+` | Speed up: 1× → 2× → 4× → 8× | main.go:1065 |
| `-` / Numpad `-` | Speed down (минимум 1×, пауза только через Space) | main.go:1069 |
| `Tab` | Cycle workspace preset (Field / Command) | main.go:921 |
| `P` | Toggle expanded profiler HUD | main.go:1782 |
| `Ctrl+P` | Print profiler snapshot to stdout + trace mark | main.go:1782 |
| `M` (build `-tags trace`) | Emit `{"event":"mark"}` в JSONL трейс | main.go (trace build) |
| `Esc` | Close active floating panel / cancel chrome state | main.go:1041 |
| `Shift` (modifier) | RMB-append / LMB-marquee union / WASD-sprint | main.go:913 |
| `Ctrl` (modifier) | Bind digit slots / Ctrl+RMB Sneak / Ctrl+P snapshot | main.go:914 |
| `Alt` (modifier) | Alt+RMB AttackMove | main.go:915 |

---

## 2. Якорь и камера

| Ввод | Действие | Контекст |
|---|---|---|
| `W` / `A` / `S` / `D` (hold) | Двигать anchor по поверхности | main.go:1086-1095 |
| `Shift` + WASD | Anchor sprint (бег) | main.go (anchor speed mul) |
| **MMB drag** | **Orbit камеры** (Yaw + Pitch) — Phase 17.6 (was RMB) | `systems/orbit_system.go:48` |
| Wheel | Zoom камеры (radius) | `systems/orbit_system.go:70` |
| RMB tap (без selection) | Anchor pathing — pathfind anchor к target'у | command.go (legacy fallback) |

**Гейтинг orbit (`OrbitInputEnabled`):** только когда focused = `Panel3D` или `PanelNone`, и курсор не над floating-панелью. Phase 17.6: pie-menu suppressors удалены (pie больше не в RMB flow, конкуренции с MMB camera нет).

---

## 3. Panel3D — основная 3D-сцена

### 3.1 Мышь

| Ввод | Действие |
|---|---|
| LMB click | Select unit (closest within 1 m XZ) |
| `Shift`+LMB click | Toggle unit в selection |
| LMB click on terrain (без `Shift`) | Clear selection |
| LMB click на building (без других hit'ов) | Set `selectedBuilding` (sticky подсветка) |
| LMB drag | Marquee box |
| `Shift`+LMB drag | Marquee union с текущей selection |
| RMB tap | Order на cursor-target (hit-test resolver) |
| RMB hold > 200 ms на здании | **Context menu** (popup, Phase 17.6) с секциями Attack / Interact |
| RMB hold > 200 ms (не здание) | Ничего (default tap committed on release) |
| RMB drag > 8 px (с selection) | **Facing-drag** — yaw для arrived-facing, всегда работает |
| RMB drag > 8 px (без selection) | Ничего (orbit теперь на MMB) |
| RMB hover (без нажатия) с selection | Ghost preview расстановки в формации |
| `Shift`+RMB | Append order to chain (вместо replace) |
| `Shift`+RMB на subset одного squad'а | Place `IndividualPosition` на каждый выбранный (Phase 15) |
| `Ctrl`+RMB | Force Sneak preset (Stealth profile override) |
| `Alt`+RMB | Force AttackMove flag |
| Double-RMB (≤ 300 ms) | MoveTo + Sprint preset |
| **MMB drag** | Camera orbit (Phase 17.6, was RMB) |

### 3.2 Hit-test resolver (RMB-tap → OrderKind)

`command.go:resolveTargetIntoOrder` маппит:
- `HitBuilding` → **`OrderKindOccupyBuilding`** (Phase 17.6 default — было Garrison)
- `HitTrench` → `OrderKindOccupyTrench` (entity = trench root)
- `HitUnit` (enemy в радиусе 1.5 m) → `OrderKindAttackTarget`
- `HitTerrain` → `OrderKindMoveTo`

`OccupyBuilding` ведёт squad внутрь здания через дверь и распределяет по этажам через NavGrid spread. Garrison ("атакующая позиция у окон") доступен только через popup. Snap-к-floor-center для cursor-над-крышей здания сохранён (main.go RMB press handler).

### 3.3 Building popup (RMB-hold > 200 ms на здании, Phase 17.6)

Прямоугольный контекстный popup `ui.ContextMenu` рядом с курсором. Закрывается через:
- LMB на пункт → commit
- RMB release с hover на пункт → commit (single-gesture)
- ESC / LMB вне popup → cancel

Содержимое (`command.go:buildBuildingPopupSections`):

**Attack:**
- **Clear and occupy** → `OrderKindClearBuilding` (auto-chain в OccupyBuilding на complete)
- **Suppress fire** — disabled (Phase 14.8 per-window работа)

**Interact:**
- **Attacking position at windows** → `OrderKindGarrison` (ghost у окон)
- **Hidden position** → `OrderKindOccupyBuilding` + Stealth preset + HoldFire override
- **Occupy L0..LN** (динамические per Level entity) → `OrderKindMoveTo` на Level.AABB.Center, NavService путь через лестницу

Каждый пункт = icon (placeholder rune) + label + tooltip над popup'ом при hover. Ghost preview переключается per-hover: window-attach для Garrison, floor-spread для Occupy/Clear/Hidden, single-floor для Occupy L*N*.

**Pie menu (legacy)** — код `ui/pie_menu.go` остаётся в репо deprecated (не вызывается из main.go), может быть удалён в Phase 21 или переиспользован для будущих radial-кейсов.

### 3.4 Hold-клавиши: debug-оверлеи (Panel3D-only)

| Клавиша (hold) | Overlay |
|---|---|
| `G` | RoadGraph wireframe + map-debug-layer (рендерит roads + rivers + buildings на 2D-карте) |
| `N` | NavGrid (per-chunk surface) |
| `C` | CoverMap (cover bitmap per cell) |
| `V` | Cover-slot positions + facing arrows |
| `F` | FloorNavGrid (interior level nav) |
| `Y` | Vision pairs (seer → last-seen target lines) |
| `K` | Draw all squads connections (вместо только-selected) |
| `J` | TransitionEdge lines (Surface↔Level зелёные, Level↔Level жёлтые) |

### 3.5 Прочие Panel3D хоткеи

| Клавиша | Действие |
|---|---|
| `H` | Stop order для selected (отменяет всю цепочку squad'ов + clear actions у соло-юнитов) |
| `T` (≥ 2 selected) | Form Squad (infantry → FormationLoose, mixed → FormationCustomSlots snapshot) |
| `U` (selected) | Disband: каждый member вышибается из squad'а через `SquadService.Leave` |
| `E` | Toggle Formation Editor floating panel |
| `F1` / `F2` / `F3` / `F4` (homogeneous squad) | Switch FormationKind: Line / Column / Wedge / Loose |
| `[` / `]` (homogeneous squad) | Cycle MovementPreset prev/next (6 presets) |
| `'` (Apostrophe) (homogeneous squad) | Toggle Posture Standard ↔ Quiet |
| `X` | Drop 4×2 m crater at anchor (debug/persistence test) |
| `B` | Toggle `BuildingViewMode.InteriorOpen` для всех зданий |
| `PageUp` / `PageDown` | Cycle `CurrentLevel` всех зданий вверх/вниз |
| `1`..`5` | Recall bind slot (selection ring) |
| `Ctrl+1`..`Ctrl+5` | Bind current selection в slot |

**Stance hotkeys (Z/X/C)** — отложены до Phase 21 из-за коллизий: `X` = crater, `C` = cover overlay (см. memory `feedback_hotkey_collisions`).

---

## 4. PanelMap — 2D-карта

| Ввод | Действие |
|---|---|
| MMB drag | Pan карты |
| Wheel | Zoom (pivot на cursor) |
| LMB click | Pick squad marker (12 px радиус) → select roster |
| RMB tap | Тот же `resolveRMBOrder` (через map-coord flow), target = `MapPanelToWorld(cursor)` |
| RMB hold | Pie menu (тот же что в Panel3D) |
| `Shift`+RMB | Append order |
| Modifiers Ctrl/Alt/Double-RMB | Те же — Sneak/AttackMove/Sprint |

**Маркеры на карте:** order chain рендерится линиями + per-kind icons (`drawOrderMarkers` в `ui/map_render.go`). Hover-эффекта по маркерам нет. **Context menu на marker (Phase 21)** не реализован.

---

## 5. Inspector — кликабельные зоны

LMB-press внутри focused inspector триггерит:

### 5.1 Заголовок / селекция
- Roster row click → select unit (single-squad view)

### 5.2 Doctrine chip row (Phase 15)
- Один из 5+ доктринных preset'ов (Patrol / Assault / Stealth / Defense / …) — пишет сквозь Movement+Engagement+Behavior

### 5.3 Autonomy chip row (Phase 15)
- 4 уровня: Strict / Cautious / Adaptive / Survival — пишет в BehaviorRules с dirty-bits

### 5.4 Movement section
- 6 preset chips: Default / Cautious / Rush / Sprint / Stealth / ProneCrawl
- Pace dropdown: Walk / Run / Sprint
- Stance dropdown: Stand / Crouch / Prone
- Posture dropdown: Standard / Quiet
- PathStyle dropdown: Direct / RoadPrefer / RoadAvoid / CoverSeek
- Stamina avg bar — read-only

### 5.5 Engagement section
- 3-button RoE Mode: Hold / Return / Free
- 4 target toggles: Inf / Arm / Air / Struct

### 5.6 Behavior section
- 4 чекбокса: Auto-reposition / Auto-stance / Hold until ordered / Allow return fire
- Suppression threshold: `[-]` / `[+]` cycle

### 5.7 Override section
- Read-only: текущий TacticalOverride и Reason (если есть)

### 5.8 Orders section
- Текущий head order + до 2 queued — read-only (clickable marker context menu это Phase 21)
- Progress bar по `OrderProgress.Value` (для Garrison: inside/alive fraction)
- AttackMove pill индикатор

### 5.9 Скролл
- Вертикальный скролл колесом
- Drag scrollbar мышью (Phase 13.5)

---

## 6. Formation editor

Контекст: открывается через `E` как floating panel, либо через chevron menu в любом workspace-leaf.

| Ввод | Действие |
|---|---|
| LMB drag на unit dot | Drag slot offset (snap-to-grid) — slot 0 locked для infantry-only |
| Wheel внутри панели | Zoom range (2–200 px/m) |
| LMB click на "Movement" / "North" toggle | Rotate-with-march vs compass-locked |
| LMB click на "Kind ▾" dropdown | Switch FormationKind или Apply/Save preset |

**Follows selection:** при смене squad'а в 3D, эдитор пере-биндится автоматически.

---

## 7. Timeline panel

| Ввод | Действие |
|---|---|
| LMB click на order block | (Hit zone есть, но handler сейчас неактивен / placeholder) |
| RMB на timeline (вне order block) | Открывает context (`main.go:1267` ветка — placeholder) |
| Tooltip on hover (`DrawTimelineTooltip`) | Показывает kind / state / progress |

Полное редактирование маркеров (insert/delete/drag waypoint) — отложено до Phase 21.

---

## 8. Top bar (chromeless toolbar)

| Кнопка / зона | Действие |
|---|---|
| `<<` button | Speed down (то же что `-`) |
| `▶ / ⏸` button | Play / Pause toggle (то же что `Space`) |
| `>>` button | Speed up |
| Left: `T+mm:ss` clock | Read-only |
| Right: hotkey hint string | Read-only |

`TopBarHitTest` в `ui/topbar.go:106` возвращает hit-kind, main.go routes LMB clicks.

---

## 9. Building widget (chip strip)

При hover/select здания вверху над крышей рендерится chip-strip:

| Chip kind | Действие |
|---|---|
| `ChipKindLevel` (L0 / L1 / L2 …) | Click → switch `BuildingViewMode.CurrentLevel` |
| `ChipKindWallMode` | Click → switch wall rendering mode (full / cutaway / hide) |
| `ChipKindInside` | Click → toggle `BuildingViewMode.InteriorOpen` |

Hovered building outline = жёлтый, selected = голубой (main.go:2102-2107).

---

## 10. Floating panels

| Ввод | Действие |
|---|---|
| LMB title drag | Move floating panel |
| LMB drag любой из 4 edges / 4 corners | Resize (cursor swaps NS/EW/NWSE/NESW) |
| LMB на `X` button | Close floating |
| `Esc` (с active floating) | Close topmost |
| LMB на chevron (рядом с `X`) | Open switch-content menu — swap widget |
| LMB anywhere в floater | Raise to top |
| Любой ввод под cursor на floater | Suppress всё под ним (wheel/pan/RMB orders/3D orbit) |

Зарегистрированные floating: `formation-editor` (через `E`), может также Inspector/Map/Timeline через Float-pane action.

---

## 11. Workspace chrome (PanelManager)

### 11.1 Chevron menu (правый край title bar каждой панели)

LMB на chevron → меню:
- **Switch widget**: Field / Map / Inspector / Timeline / Formation
- **Float pane**: Detach widget в floating panel (leaf merges into sibling; Panel3D currently не floatable)
- **Close pane**: Same merge без spawn floating

### 11.2 Splitter

LMB drag на splitter (между панелями) → изменение split-пропорции. Persisted (L2 polish, Phase 13.5).

### 11.3 Panel focus

`panelMgr.FocusedAt(cursor)` — каждый кадр определяет focused panel; routing для wheel/RMB/keyboard зависит от focus.

---

## 12. Modifier matrix (резюме)

| Modifier | LMB | RMB | Wheel |
|---|---|---|---|
| (none) | Select unit / Click chip / etc | Order на target | Camera zoom (3D) / Map zoom (map) |
| `Shift` | Marquee union / toggle unit | Append order / IndividualPosition on subset | — |
| `Ctrl` | (свободен в 3D) / Bind slot 1..5 | Sneak preset override | — |
| `Alt` | (свободен) | AttackMove flag | — |
| Double-RMB | — | Sprint preset override | — |

---

## 13. Что **зарезервировано / не используется**

Эти клавиши свободны и могут быть переназначены:

- `Q`, `R`, `Z`, `I`, `L`, `O` — не используются нигде в Panel3D
- `Z`/`X`/`C` — в плане Phase 21 как Stance hotkeys (X/C сейчас заняты under debug/crater, требуют переноса)
- Function keys `F5`..`F12` — свободны
- Numpad — кроме `+` / `-` свободен
- Backtick `` ` ``, `\`, `;`, `,`, `.`, `/` — свободны

---

## 14. Что **планировалось, но ещё не сделано**

Для контекста при перепланировании — это уже было в COMMAND-MODEL.md / ROADMAP, но пока не реализовано:

- **Marker context menu** (RMB-tap на existing Order marker) → insert/delete/edit waypoint — Phase 21
- **Weapon-bar в Inspector** для per-weapon targeting — Phase 14 plan, скаффолд не доделан
- **Map ping** (события на 2D-карте) — Phase 21
- **Event log panel** — Phase 21
- **Auto-pause matrix** в options — Phase 21
- **Order tree visualization** (git-tree projection в Inspector) — Phase 21
- **Drag waypoint** — Phase 21
- **Undo/Redo для плана** — Phase 18 (UI L4 + timeline + undo)
- **Salvo / coordinated fire** через weapon-bar group select — отложено
- **Order naming** (player labels для сложных операций) — open question в UI.md

---

## 15. Architecturally-locked обработчики

Что **нельзя двигать без больших рефакторов**, потому что они интегрированы в систему:

- **`OrderKindSpecs[]` spec-table** определяет, какие kind'ы появляются в pie menu, какой default-арм completion, нужен ли entity-target. Любой новый kind = строка в этой таблице.
- **`OrbitInputEnabled` per-frame gate** в main.go — единственное место, где RMB-orbit может быть отключён.
- **`PanelManager.FocusedAt`** — единая точка определения focused panel; routing wheel/RMB/keyboard зависит от неё.
- **`SquadService.IssueOrder`** — все RMB orders проходят через него (replace/append), сразу с params (MovementOverride / AttackMove / Facing).
- **`pieMenu.Begin` → `Tick` → result switch** в main.go:1418-1559 — единственный flow для RMB-decoding (tap vs hold vs facing-drag vs orbit-drag).
