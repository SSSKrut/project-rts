# Phase 10 — рабочий план

Interface foundation: переход от fullscreen 3D + HUD к multi-panel UI. 4 базовые панели (3D-сцена / 2D-карта / Inspector / Time controls), фиксированный L1-layout с Tab-переключаемыми пресетами Field/Command, mouse routing через PanelManager, общий resolver приказов для 3D и карты, time scale с pause. Карта — 2D-абстракция через иконки и pre-baked greyscale underlay, без 3D-render.

**Что в Phase 10 сознательно НЕТ.** Movable / resizable / splittable панели (L3 → Phase 22). Order markers на карте, pie menu, vector orders — это Phase 11 (без типизированных Order-сущностей рисовать нечего). Vision/FoW на карте — Phase 15. Build toolbar и quick-bars — Phase 21. Roads/rivers/buildings как отдельные стилизованные слои карты — опционально в M10.6, но не обязательно для MVP («лишь бы что-то отображалось» — see GAMEDESIGN §9 dynamic-layer-минимум: squads + selection + position-anchor). Tooltips, hover popups, hotkey rebinding — Phase 21+. Layout persistence в save/ — Phase 22 L4-polish.

ROADMAP — высокоуровневый трекер. Этот файл — рабочий, обновляется по ходу выполнения.

---

## Решения, которые лочим до начала кода

**P1. Panel как struct с bounds + content callback, layout вычисляется контейнером.**

```go
type PanelID string

const (
    Panel3D       PanelID = "3d"
    PanelMap      PanelID = "map"
    PanelInspect  PanelID = "inspector"
    PanelTime     PanelID = "time"
)

type Panel struct {
    ID     PanelID
    Bounds rl.Rectangle   // в screen coords, вычисляется LayoutManager'ом
    Title  string
}

type PanelManager struct {
    Panels  []Panel        // упорядочены: первая = «нижняя» (rendered first); Z-order для focus
    Layout  LayoutPreset   // Field / Command
    focused PanelID        // под cursor'ом в этом кадре
}

type LayoutPreset uint8
const (
    PresetField   LayoutPreset = iota  // 3D большой (60%), Map маленькая (25%)
    PresetCommand                      // Map большая (60%), 3D маленькая (25%)
)

func (m *PanelManager) Recompute(screenW, screenH int32)         // пересчитать bounds от layout + screen size
func (m *PanelManager) FocusedAt(cursor rl.Vector2) PanelID      // top-Z panel containing cursor; "" если ни одна
func (m *PanelManager) Get(id PanelID) Panel                     // lookup by ID; для рендера / input routing
```

Layout — **функция от screen size и LayoutPreset'а**. Никаких absolute coords в кодe. Это даёт корректное поведение при resize окна и упрощает миграцию на L3 (когда splitter'ы появятся, layout формула становится сложнее, но интерфейс PanelManager не меняется).

**P2. Render-to-texture только для 3D-сцены. Map и Inspector — direct 2D draw через scissor.**

3D-панель — `rl.RenderTexture2D` правильного размера, перерисовываемый каждый кадр в RT, потом `DrawTextureRec` в panel-rect. Map и Inspector — `rl.BeginScissorMode(panel.Bounds)` + 2D draw calls + `EndScissorMode`. Time controls — то же самое.

**Развилка**: RT-размер = panel-size, переаллоцируется при изменении layout. Альтернатива — RT всегда screen-size, копируется sub-region'ом. Первое экономит память (3D-RT ~30% экрана = меньше пикселей), второе экономит CPU на переаллокациях. На стабильном layout'е MVP'а разница пренебрежимая; берём первое (panel-size RT), переаллокация только при смене preset'а или resize окна.

**P3. Mouse routing — focusedPanel определяется per-frame по cursor.pos, input handlers gate'ятся по `panelMgr.IsFocused(id)`.**

Каждый кадр перед input-блоком main.go:
```go
focused := panelMgr.FocusedAt(rl.GetMousePosition())
```

Дальше:

- `pickUnitFromMouse` / marquee / RMB-order — работают только когда `focused == Panel3D`. Координаты курсора пересчитываются в panel-local (`cursor - panel.Bounds.Origin`).
- WASD-anchor — работает когда `focused == Panel3D` или ни одна панель не focused. Когда focused == PanelMap — WASD панорамирует карту (через MapCamera, см. P5).
- LMB / RMB на карте — работают когда `focused == PanelMap` через свой resolver.
- Все hotkeys (Tab, H, T, U, F1-F4, Ctrl+N, N) работают всегда независимо от focus.

Раздельные клавиатурные input'ы по фокусу делать НЕ будем — это перегиб.

**P4. 3D-сцена: rl.BeginTextureMode(rt) → BeginMode3D(currentCamera) → все 3D-draws → EndMode3D → EndTextureMode → DrawTextureRec(rt, panel.Bounds).**

Это базовый паттерн raylib. Текущий код в main.go (между `rl.BeginMode3D(systems.CurrentCamera)` и `rl.EndMode3D()`) переносится **внутрь** `BeginTextureMode`. Размер RT и projection matrix матчатся с panel.Bounds.Width/Height — это даёт правильный aspect ratio.

**Важно для raycast'а:** `rl.GetMouseRay(cursor, cam)` использует **screen-size** для projection. Когда 3D-сцена — это RT внутри панели, cursor должен быть **panel-local**, а camera matrix построен от panel-size. raylib`s GetMouseRay этого сам не делает. Решение: пересчитать вручную, либо использовать `rl.GetScreenToWorldRayEx(cursor, cam, width, height)` (raylib 5.0+ функция — нужно проверить наличие в raylib-go).

**P5. Map camera = struct с center WorldPos + zoom (пикселей на метр) + pan-state. Никакой 3D-камеры на карте.**

```go
type MapCamera struct {
    Center   components.WorldPos    // что в центре панели
    Zoom     float32                // пикселей на метр; default 0.5 (= 2 м/пиксель)
    MinZoom  float32                // напр. 0.1 (= 10 м/пиксель)
    MaxZoom  float32                // напр. 4.0 (= 0.25 м/пиксель)
}

// World ↔ screen conversion (внутри map-панели):
func mapWorldToPanel(wp components.WorldPos, cam MapCamera, panelBounds rl.Rectangle) rl.Vector2
func mapPanelToWorld(panelPos rl.Vector2, cam MapCamera, panelBounds rl.Rectangle) components.WorldPos
```

Pan: MMB-drag (или WASD когда focused) сдвигает Center. Zoom: mouse wheel умножает Zoom на 1.15^scroll (continuous). Wheel cursor-relative: точка под cursor'ом остаётся под cursor'ом после zoom'а (стандартный CAD-pattern).

**P6. Map underlay — pre-baked greyscale hill-shade, генерируется при старте.**

```go
type MapUnderlay struct {
    Texture rl.Texture2D       // greyscale R8 или RGB8
    OriginWorld rl.Vector3     // world-coord левого верхнего угла underlay в render-space
    ScaleM      float32        // м/пиксель в исходных данных
}
```

Генерация при `main.go` startup'е: проходим по `2 км × 2 км` square вокруг origin chunk'а (placeholder size; в Phase 24 заменится на DEM-load), sample'им `GroundHeight(x, z)` каждые 4 м, считаем hill-shade (классическая формула: lighting от +Y с косинусом наклона), кодируем в uint8 greyscale. Размер: 2000/4 = 500 px → 500×500 = 250 KB. Дёшево.

Render на карте: одна `DrawTexturePro(underlay.Texture, srcRect, dstRect)` с правильным масштабированием через MapCamera. raylib делает bilinear filtering автоматически — выглядит чисто на любом zoom'е.

**Открытая развилка**: что делать когда anchor уехал за пределы pre-baked area? Для MVP — ничего; underlay просто заканчивается, дальше карта пустая (greyscale → fallback цвет терраина). Для Phase 24 — streaming tile'ов или re-bake. Решение откладываем.

**P7. Time scale через app.TimeScale, умножается в dt при Tick. TimeScale=0 = pause, input работает.**

```go
// В core/app.go:
type App struct {
    // ... existing fields
    TimeScale float32   // 0 = paused, 1 = normal, 2/4/8 = compressed
}

func (app *App) Tick(deltaReal time.Duration) {
    delta := time.Duration(float64(deltaReal) * float64(app.TimeScale))
    // existing Tick logic with `delta` instead of input
    // app.elapsed += delta (НЕ deltaReal — паузная игра не должна тикать elapsed)
}
```

**Развилка**: чем считать elapsed — реальным или scaled? Profiler / Trace используют elapsed для timestamps; они должны отражать реальное время (для perf-анализа). Scheduler-LOD-интервалы используют elapsed для определения «пора ли запускать систему» — должны жить в scaled-time (паузная игра не должна продолжать запускать SquadMacroPathSystem каждую игровую секунду).

Решение: `app.elapsed` — scaled (игровой elapsed), profiler берёт `time.Now()` для своих замеров. Это дешёвый split, всё уже работает в этом духе (profiler делает time.Since, не elapsed).

Hotkeys: `Space` toggle (TimeScale 0 ↔ предыдущая ненулевая), `+`/`-` cycle через 1→2→4→8→1. Display над time-panel: `PAUSED` / `1×` / `4×` etc.

**P8. Selection и hover — глобальные переменные в main.go, читаются всеми панелями. Без ECS-компонентов `Selected{}` / `Hovered{}`.**

```go
// В main loop scope:
var selected []ecs.Entity     // существует с Phase 7
var hovered ecs.Entity        // новое в Phase 10; 0 = ничего не hover'ится
```

Mouse hover в 3D-панели обновляет `hovered` через тот же raycast что pickUnitFromMouse, но без клика. Map-панель обновляет `hovered` через scan ближайшей squad-маркер к cursor'у. Inspector рисует подсветку на роwе ростера если `hovered == roster member`.

**P9. Layout presets Field / Command — два фиксированных layout'а, Tab toggle через мгновенный swap, без анимации.**

Layout Field:
```
+-----------------------+-------+
|                       | Insp  |
|        3D (60%)       |  15%  |
|                       |       |
+-----------------------+-------+
|        3D cont        | Map   |
|                       |  25%  |
+-----------------------+-------+
|         Time controls (full)  |
+-------------------------------+
```

Layout Command: 3D ↔ Map swap (Map становится основной).

Реализация: `LayoutPreset` → функция, возвращающая `map[PanelID]rl.Rectangle` от screen size. Tab переключает `panelMgr.Layout`, `Recompute()` обновляет bounds, RT 3D-сцены переаллоцируется на новый размер.

**P10. Order resolver — общий код, вызывается из 3D-RMB-handler'а И из Map-RMB-handler'а.**

```go
func resolveRMBOrder(selected []ecs.Entity, target components.WorldPos, ...)
    {
    // Существующая логика: groupSelected → homogeneous squad? OrderMoveTo : per-unit.
    // Phase 11 расширит этим resolver'ом hit-test'ом сущностей под target'ом
    // (Building → Garrison, Trench → Occupy, etc.). Phase 10 — только MoveTo.
}
```

3D-RMB:
```go
if panelMgr.IsFocused(Panel3D) && rl.IsMouseButtonPressed(rl.MouseButtonRight) {
    cursor := rl.GetMousePosition().Sub(panel3D.Bounds.Origin)  // panel-local
    if target, ok := mouseTargetWorldPos(cursor, cam, ...); ok {
        resolveRMBOrder(selected, target, ...)
    }
}
```

Map-RMB:
```go
if panelMgr.IsFocused(PanelMap) && rl.IsMouseButtonPressed(rl.MouseButtonRight) {
    cursor := rl.GetMousePosition().Sub(panelMap.Bounds.Origin)
    target := mapPanelToWorld(cursor, mapCam, panelMap.Bounds)
    resolveRMBOrder(selected, target, ...)
}
```

Идентичность 3D ↔ карта обеспечена тем, что обе ветки сводятся к одному resolver-вызову с одним и тем же `target WorldPos`.

---

## Семь мильстоунов

### M10.1 — PanelManager core + L1 layout + 4 stub-панели + Tab toggle

**Цель.** На сцене 4 разделённых прямоугольника-панели, каждый с заголовком в верхнем углу, без содержимого (просто цветной background для визуальной проверки). Tab переключает Field ↔ Command layout. Окно ресайзится — layout пересчитывается.

**Делаем:**

- `ui/panel.go`: `Panel`, `PanelManager`, `PanelID`, `LayoutPreset`. `Recompute(screenW, screenH)`. `FocusedAt(cursor)`. `Get(id)`.
- `ui/layout.go`: функции `layoutField(w, h) map[PanelID]Rectangle`, `layoutCommand(w, h) map[PanelID]Rectangle`. Каждая возвращает 4 ректа (3d / map / inspector / time).
- В main.go: `panelMgr := ui.NewPanelManager(); panelMgr.Recompute(screenWidth, screenHeight)`. Tab-handler: toggle preset + recompute.
- В render loop main.go: `rl.DrawRectangleRec(p.Bounds, debugColor)` + `drawHUDText(p.Title, ...)` для каждой панели.

**Проверяем.** Окно показывает 4 цветных прямоугольника с заголовками. Tab меняет местами 3D и Map. Ресайз окна (если raylib позволит — `rl.SetWindowState(rl.FlagWindowResizable)`) — layout остаётся пропорциональным.

### M10.2 — 3D-сцена в RenderTexture2D

**Цель.** Существующая 3D-сцена (terrain, units, buildings, props, overlays) полностью рисуется внутри RT, RT композитится в 3D-panel-rect. Aspect ratio корректный, всё видно как раньше, но в подгрузить-меньшем окне.

**Делаем:**

- `ui/scene3d.go` или прямо в main.go: создание `rl.LoadRenderTexture(width, height)` при старте + при смене layout.
- Перенос существующего 3D-блока (`rl.BeginMode3D ... rl.EndMode3D`) внутрь `rl.BeginTextureMode(rt) ... rl.EndTextureMode`.
- После EndTextureMode — `rl.DrawTextureRec(rt.Texture, srcRect, dstPos, rl.White)` с правильным flip Y (raylib RT текстуры перевёрнуты по умолчанию — флипаем через `srcRect.Height = -rt.Texture.Height`).
- Камера projection matrix остаётся тем же (perspective), но aspect ratio считается от RT size: `cam.Projection = rl.CameraPerspective` уже даёт это автоматом — raylib derives aspect from current viewport size, которое внутри BeginTextureMode равно RT size.

**Развилка.** При смене layout RT нужно переаллоцировать: `rl.UnloadRenderTexture(rt)` + `rl.LoadRenderTexture(newW, newH)`. Делаем это лениво — после `panelMgr.Recompute`, проверяем размер 3D-panel'а, переаллоцируем если изменился.

**Проверяем.** 3D-сцена видна в своей панели. WASD двигает anchor (с focusedPanel-gate, см. M10.3). Все overlays (G/N/C/V/F/Y/K) работают и рисуются внутри 3D-RT.

### M10.3 — Mouse routing: focusedPanel-gating всех 3D-инпутов

**Цель.** Все 3D-specific input'ы (LMB select / drag / RMB order / WASD-anchor) активны только когда `focused == Panel3D`. Cursor coords пересчитаны в panel-local перед raycast'ом. WASD — особый случай: активен в 3D-panel'е или когда нет focused-panel'а (cursor вне всех панелей).

**Делаем:**

- В main.go начало кадра: `focused := panelMgr.FocusedAt(rl.GetMousePosition())`.
- Каждый input-блок (LMB-press / LMB-release / RMB / WASD / H / X / T / U / F1-F4) обёрнут в:
  ```go
  if focused == ui.Panel3D || focused == "" /* для глобальных */ {
      // existing logic
  }
  ```
- `pickUnitFromMouse` / `collectUnitsInRect` / `mouseTargetWorldPos` принимают `cursor rl.Vector2` параметром (cursor — panel-local), не читают `rl.GetMousePosition()` напрямую.
- Marquee start/end coords тоже panel-local.

**Развилка.** Что если cursor над панелью Inspector (выше 3D-сцены), и игрок жмёт LMB? Существующий код пытается раскастать в 3D-сцену → промах. С gate'ом — игнорируем. Тест-кейс: должно быть тихо, без false-select.

**Проверяем.** LMB/RMB в 3D-панели — работают. LMB/RMB над inspector — игнорируются. WASD-anchor работает в 3D-панели и в «нигде». Если cursor над map-panel'ом, WASD пока ничего не делает (map-pan через WASD — в M10.7, пока MMB-drag только).

### M10.4 — Inspector content

**Цель.** Inspector-панель показывает информацию о selected:

- Пусто → «No selection» + list of all squads с краткой статистикой (count, doctrine — TBD когда добавим, пока просто count).
- 1 Unit → имя (entity ID хэш), role placeholder (Phase 12 даст роли — пока «Rifleman» дефолт), stance, motion (speed + yaw deg), suppression level, parent squad link, equipment summary.
- 1 Squad → имя (hash), member count, formation kind + spacing, current MacroPath state (Idle / Moving (waypoint i/N) / On goal), список members (по строке: slot + entity ID + stance).
- Multi-select → counts (units, squads).

**Делаем:**

- `ui/inspector.go`: функция `drawInspector(panel ui.Panel, selected []ecs.Entity, hovered ecs.Entity, world *ecs.World, posMap *ecs.Map[components.WorldPos], rosterMap *ecs.Map[components.CommandRoster], ...)`. Внутри scissor, рисует через `drawHUDText`.
- Layout внутри панели: title, разделитель, контент. Контент скроллится позже (M10.7 polish если останется), пока fixed height.
- Hover-highlight: если `hovered == roster member`, его строка нарисована в squad-color background.

**Проверяем.** Кликнуть squad → видна вся его инфа. Hover на список — соответствующий юнит подсвечивается в 3D и наоборот. Кликнуть в пусто (3D) → list of squads.

### M10.5 — Time controls (Space, +/-, display)

**Цель.** `Space` toggle pause. `+`/`-` cycle 1→2→4→8→1×. Time panel показывает текущее значение крупным шрифтом, иконку pause/play.

**Делаем:**

- В core/app.go: добавить `TimeScale float32` (init = 1.0), `LastNonZeroScale float32` (для pause-toggle), правка `Tick` чтобы умножать delta на TimeScale.
- В main.go: hotkey handlers для Space / +/-. State `prevTimeScale` для toggle-with-restore.
- `ui/time.go`: `drawTimeControls(panel ui.Panel, app *core.App)`. Большая цифра + label, иконка ▶/⏸.

**Развилка про elapsed.** Per P7: app.elapsed — scaled (игровой). Profiler timestamps — real time. Trace timestamps — real time. Это уже в духе текущего кода (Profiler делает `time.Since`, не elapsed); тестировать после.

**Проверяем.** Space паузит — юниты замирают, FormationSystem не пишет (так как scaled-dt = 0). Космос продолжает рисоваться нормально (рендер не блокируется). +/- меняют скорость — юниты ускоряются. Profiler HUD продолжает показывать корректные ms-замеры даже на 8×.

### M10.6 — Map underlay pre-bake + render

**Цель.** Карта показывает greyscale hill-shade текстуру (pre-baked 2×2 км вокруг origin при startup'е). Camera MapCamera управляет видом: zoom через wheel, pan через MMB-drag. Anchor-маркер (синий крест или круг) в позиции anchor'а.

**Делаем:**

- `ui/map_underlay.go`: `BakeUnderlay(originChunk components.ChunkCoord, sizeM float32, scaleM float32) MapUnderlay`. Генерирует Image через `rl.GenImageColor` + ручное заполнение pixel'ов (либо `rl.ImageDrawPixel`, либо raw byte buffer + `rl.LoadTextureFromImage`). Hill-shade формула: `lighting = max(0, cos(angle_between(surface_normal, light_dir)))`, light from (-0.5, 0.7, 0.5) normalized.
- `ui/map_camera.go`: `MapCamera` struct, методы `WorldToPanel` / `PanelToWorld`. Pan-state (cursor anchored при MMB-press, дельта при MMB-down).
- `ui/map_render.go`: `drawMapPanel(panel ui.Panel, cam MapCamera, underlay MapUnderlay, anchorWP components.WorldPos)`. Внутри scissor: DrawTexturePro(underlay) + DrawCircle / Cross для anchor'а.
- Pre-bake вызывается в `main.go` после прогрева чанков (M10 не зависит от стриминга — bake идёт по `GroundHeight` напрямую, чанки не нужны).

**Развилка.** Pre-bake 2×2 км @ 4 м/пиксель = 500×500 px. На M2 / mid-range CPU — sub-second генерация. Bake блокирующий при startup'е — приемлемо.

**Развилка 2.** Размер pre-baked area — 2 км достаточно для test-сцены, но anchor может уехать за пределы. Для MVP — оставляем 2 км, дальше карта пустая (показывает фоновый цвет). В Phase 24 — DEM-streaming.

**Проверяем.** Карта показывает рельеф (видны горы и впадины через shading). Wheel zoom'ит. MMB-drag панорамирует. Anchor виден синим маркером, движется при WASD.

### M10.7 — Map dynamic layer + RMB-order resolver shared

**Цель.** На карте поверх underlay'а нарисованы squad-маркеры (круги squad-color'ом с slot 0 inside), selection highlight (cyan rings вокруг выделенных squad'ов), hover-highlight. LMB-click на squad-маркер в карте = выделение (синхронно с 3D). RMB на пустую точку карты = MoveTo через shared resolver (тот же что в 3D-RMB).

**Делаем:**

- `ui/map_render.go::drawDynamicLayer`: пробегает `squadFilter.Query()`, для каждого squad'а вычисляет `SquadCenter`, проецирует в panel-space через `cam.WorldToPanel`, рисует круг + название (slot 0 entity ID hash). Selection: ring overlay cyan. Hover: brighter outline.
- Map LMB pick: `mapPickSquad(cursor, cam, squadFilter, posMap) (ecs.Entity, bool)` — найти ближайший squad-маркер в радиусе 8-12 px.
- Map RMB: `target := mapPanelToWorld(cursor, cam, panelMap.Bounds); resolveRMBOrder(selected, target, ...)`.
- Refactor `resolveRMBOrder` в общий helper в `input.go` (или `command.go`).
- Hover sync: при cursor над squad-маркером — `hovered = squad`. То же для 3D unit hover в M10.3 polish.

**Проверяем.** Кликаешь squad в 3D → круг подсвечен в карте. Кликаешь круг в карте → selection обновлён, 3D показывает cyan-юнитов. RMB на пустую точку карты → squad идёт туда. Hover на юните в 3D подсвечивает соответствующий squad-маркер в карте.

**Опциональные доделки M10.7 если время есть** (помечены как «опц.», можно отложить):

- (опц.) Roads на карте: пробежать `roadGraph.Edges`, рисовать линии цветами по RoadKind.
- (опц.) Rivers на карте: `rivers.Polylines` → голубые линии.
- (опц.) Buildings на карте: `buildingPlanList.Plans` → filled rectangles.
- (опц.) Map-WASD: когда `focused == PanelMap`, WASD панорамирует map-камеру (вместо anchor'а).

Эти доделки делают карту визуально богаче, но не блокируют закрытие Phase 10 (per user feedback «достаточно чтобы хоть что-то отображалось»).

---

## Что считаем «закрытием Phase 10»

- 4 панели работают: 3D-сцена, 2D-карта, Inspector, Time controls. L1 fixed layout, Tab переключает Field ↔ Command preset.
- 3D-сцена живёт в RenderTexture2D, занимает свою панель, остальные overlay'и (debug N/C/V/F/Y/G/K) работают как раньше.
- Mouse routing через PanelManager: 3D-input'ы gate'ются `focused == Panel3D`, map-input'ы — `focused == PanelMap`.
- Map underlay pre-baked при startup'е (greyscale hill-shade, 2×2 км @ 4 м/пиксель). Pan / zoom работают. Anchor виден как маркер.
- Squad-маркеры на карте, selection и hover sync'ятся между 3D и картой. LMB-click на карте выделяет, RMB даёт MoveTo через тот же resolver что 3D.
- Inspector содержит контент для пустого / одного юнита / одного squad'а / multi-select.
- Time controls: Space pause, +/- speed-cycle, display корректный. Игра реально паузится (юниты замирают, FormationSystem стоп). Profiler timing'и остаются в реальном времени.
- Pipeline без изменений: новых ECS-систем Phase 10 не добавляет (всё это рендер + input + сервисный код, не System'ы).

После этого — обновление ROADMAP, Phase 10 → ✅, переход к Phase 11 (Orders foundation). Phase 11 уже сможет рисовать order markers на карте через готовый Map dynamic layer; pie menu через готовый PanelManager mouse routing.

---

## Заметки на полях

- **PanelManager изолирован в `ui/` package**. main.go не должен лезть в `panelMgr.panels` напрямую — всё через ID и `Get`. Это подготовка к L3/L4 (Phase 22), где внутренности PanelManager сильно изменятся (splitters, dock zones, layout tree).

- **3D-RT переаллокация — единственное место в Phase 10 где раныли raylib resources вручную**. При Tab-переключении preset'а / resize окна RT надо `UnloadRenderTexture(rt)` + reload. Помним про raylib-go-gotcha: используем `UnloadRenderTexture` (а не `UnloadModel`), как с `UnloadMesh` в Phase 3.

- **Map underlay — это `rl.Texture2D`, не `rl.RenderTexture2D`**. Pre-baked один раз через `GenImage` → `LoadTextureFromImage` → upload. `UnloadImage` после upload'а; `UnloadTexture` на shutdown'е.

- **Hover state — потенциальная ловушка глобальной переменной**. При архетипных мутациях `hovered` может указать на удалённую entity. `world.Alive(hovered)` нужно гонять перед использованием (как мы добавили в Phase 9 fix). Чек на каждом use-site, не централизованный — потому что Phase 10 hover мало где читается (только Inspector).

- **Map-камера vs 3D-камера — независимы**. 3D-камера orbit'ится через OrbitSystem вокруг anchor'а. Map-камера живёт в `ui/map_camera.go`, не интегрирована с ECS. Это правильно — карта это UI-layer, не world-layer.

- **PanelMgr.FocusedAt поведение когда несколько панелей перекрываются**. В Phase 10 L1 layout — panel'и не перекрываются (фиксированный grid). Так что Z-order не важен. В Phase 22 L3 (floating panels) — Z-order через `panels []Panel` order, top-most последний. FocusedAt итерирует с конца, возвращает первый матч.

- **Cursor coords в RT vs screen**. raylib's `rl.GetMousePosition()` всегда возвращает screen-coord. Внутри `BeginTextureMode` raylib **не пересчитывает** mouse — игре self-correct'ить, вычитая panel.Bounds.Origin. Это применимо ко ВСЕМУ что использует mouse pos в 3D-логике (pickUnit, marquee, mouseTargetWorldPos). В M10.3 это переименовать в panel-local — главное чтобы это было consistent.

- **Tab toggle vs zoom-to-map.** Tab переключает preset (3D большой ↔ map большой). Если игрок хочет сразу карту во весь экран — Phase 22 даст L3-resize, можно тащить разделитель. В Phase 10 ограничиваемся двумя preset'ами.

- **WASD когда focused на карте?** Не делаем в core M10 (только M10.7 опц.). MMB-drag-pan — достаточный pattern для MVP.

- **Производительность Phase 10**. RT-рендер 3D добавляет ~1 copy per frame (panel-size → screen). Underlay-render — 1 DrawTexture per frame. Squad-маркеры на карте — 1 Filter pass на squads (12 entities в Phase 9 test-сцене, копейки). Total overhead — sub-millisecond. Profiler-HUD не должен показать новые spike'и.

- **Closure pivot to Phase 11.** Phase 10 не вводит новые ECS-сущности (`Order` и order markers — Phase 11). Это значит что в момент завершения Phase 10 MacroPath.Goal остаётся primary order-state (как в Phase 9). Phase 11 убирает MacroPath.Goal в пользу Order-entity и подключает map order-markers. План Phase 11 пишем в следующей сессии после M10.7.

- **Что НЕ делает Phase 10 хотя смежно**. Pie menu (требует Order taxonomy — Phase 11). Vector orders (то же). Build toolbar (требует Engineering — Phase 18, UI слот — Phase 21). RoE quick-bar (Phase 13/21). Movement quick-bar (Phase 13/21). Formation panel (есть hotkeys F1-F4, но UI палитра — Phase 14). Equipment panel (требует Roles — Phase 12). Event log (требует tactical events — Phase 15). Tooltips (Phase 21). Layout persistence (Phase 22 L4-polish).
