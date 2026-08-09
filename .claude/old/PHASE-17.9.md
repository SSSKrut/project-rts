# Phase 17.9 — Pathfinding & wall-collision fixes (рабочий план)

Follow-up к Phase 17.8 (Tactical AI Wave 3). После закрытия M17.8.0-10 и прогона 10-сценного auto-test suite осталось 6/10 PASS, 3 stragglers, 1 hard fail (`ai_compound_north` 0/8). Глубокий read-through всех 51 файлов слоя движения вскрыл **три накладывающихся бага**, которые в compound_north сходятся в un-tight stuck-loop. Каждый по отдельности не катастрофичен, но вместе они дают 0/8 и ухудшают другие сцены тоже.

Phase 17.9 — узкий хирургический pass: фиксы для трёх багов, regression-проверка через 10-сценный suite, target ≥ 9/10 PASS перед закрытием Phase 17.8.

`PHASE-17.8.md` — base layer. `project_phase_17_8_progress.md` (memory) — список load-bearing fixes сессии, которые **не** откатываются. `feedback_storey_wall_y_filter.md` — критическая Y-filter invariant.

---

## Что в Phase 17.9 сознательно НЕТ

- **Wall ORCA constraints** (M17.8.5b). Phase 17.9 чинит escape recovery, но не превращает walls в первоклассные ORCA half-planes. Bottleneck-corridor case остаётся для будущей фазы — стены через ORCA дадут predictive avoidance, но это объёмный refactor.
- **Compound generator rewrite.** 1m gap между wings геометрически решил бы Bug #1+#2 одним движением, но меняет gameplay (другая carve-out terrain, другие cover-slot позиции, могут ломаться существующие auto-tests). Откладываем — пробуем сделать pathfinding работать на flush geometry.
- **Junction edge re-design.** Bug #2 — level-junction edge cost=1 побеждает реальные двери при cost=3. Phase 17.9 поднимает cost (см. M3), не убирает feature.
- **Strategic / Operational tier.** Phase 22 — neural-net-eligible. 17.9 чисто tactical hotfix.

---

## Контекст багов

**Что я нашёл при rubber-duck прогоне 51 файла:**

### Bug #1 — NavInBuilding делает door outside-cells изолированными walkable-островами

`spatial_bake_nav.go:applyNavBuildings` стампит `NavInBuilding` на surface cells по центру в любом building Footprint. Для compound (M wing 12×12 + N wing 8×8 + E wing 8×8, flush-touching) три из четырёх дверей попадают так:

| Дверь | outside cell | чьё footprint |
|---|---|---|
| Main south  | `(Surf, 32, 25)` | снаружи всех ✓ |
| Main north  | `(Surf, 32, 38)` | внутри N wing |
| N south     | `(Surf, 32, 37)` | внутри M wing |
| Main east   | `(Surf, 38, 32)` | внутри E wing |
| E west      | `(Surf, 37, 32)` | внутри M wing |

Мой registry-override (см. `nav_service.go:392-414` после M17.8 hotfix) делает эти cells walkable Cost=8, но **их 4 соседа** все NavInBuilding без edge → A* blocks. Каждая пара `(32, 37) ↔ (32, 38)` становится **изолированным walkable островом**, доступным только друг от друга. Pathfinder не может войти в compound через 3 из 4 дверей с поверхности — только через level path. **Из 4 дверей compound'а с открытого terrain доступна только Main south.**

Для squad спавнящегося на (32, 58) (compound_north scene) это значит **обязательный walk-around-west или -east** длиной ~30+ cells через узкий 1-cell-wide corridor (`I=25` west of compound) → 8 юнитов сгрудиваются.

### Bug #2 — Level-junction edges (M17.8.7) физически проходят сквозь стены

`spatial_bake_transitions.go:288-382` эмитит edge cost=1 между cells `(Lvl, M, 6, 10)` (центр 32.5, 36.5) и `(Lvl, N, 4, 0)` (центр 32.5, 38.5). Это logical "teleport" для A*, но физически между cells:

- Main wing's north wall на Z=38 (opening X∈[31.4, 32.6]).
- N wing's south wall на Z=38 (opening X∈[31.4, 32.6]).

Pathfinder picks junction cost=1 over door cost=3 (cheaper), но юнит, идущий через junction по micro-path, обязан физически пройти через obrh openings — если direction не выровнен, sliding пушит мимо.

### Bug #3 — `reflectAgainstWalls` escape recovery oscillation (HIGHEST IMPACT)

`unit_movement_walls.go:79-150`:

```go
const escapeMargin float32 = 0.30
const escapeSpeed float32 = 2.0
...
if escapeActive {
    return escapeX, escapeZ  // OVERRIDES velocity
}
```

Mental dry-run для юнита идущего на юг на (32.5, 38.5) с velocity (2.65, -4.24):

1. Достигает Z=38.05 (closest point on M wing's north wall, dist=0.05 < 0.30 margin) → escape **active**.
2. Push direction = `(0, 1) * 2 = (0, 2)` — север.
3. **Velocity полностью overridden** — `return` overrides. Юнит идёт строго на север.
4. За ~10 кадров уходит на Z=38.3, dist > 0.30 → escape off.
5. ORCA снова даёт south velocity → юнит приближается → re-enters margin → re-push.
6. **Oscillation** Z ∈ [38.05, 38.3], net forward velocity ≈ 0.

При этом `Sliding` (lines 156-212) сам по себе **корректно** обрабатывает этот случай: при пересечении стены slide удалил бы только Z-компонент → юнит шёл бы east вдоль стены → дошёл до opening at X=31.4-32.6 → прошёл бы. Но **escape стопает sliding до того как тот срабатывает**.

Особенно убийственно на compound NW corner (26, 38), где сходятся M north + M west стены. Escape от двух стен суммируется. Если nor нормали противоположные (как у M north + N south на одной линии Z=38), они **взаимно гасятся** до (0, 0) → юнит полностью замерзает.

**Это объясняет наблюдаемые speed 0.02-0.45 m/s** на (27.5, 39.2) для compound_north.

---

## Решения, которые лочим до начала кода

**P1. Escape recovery не overрайдит velocity, а добавляется к ней.**

```go
// Вместо:
if escapeActive { return escapeX, escapeZ }

// Делаем:
if escapeActive {
    rvx += escapeX * escapeBlendWeight  // ~0.3
    rvz += escapeZ * escapeBlendWeight
}
// и затем продолжаем main slide loop как обычно.
```

**Почему:** sliding — корректное поведение при штатном приближении к стене. Escape — последний rescue при настоящем clipping. Они **не должны конкурировать**, escape — это spring-back force, sliding — основной режим. Если юнит на 0.05m от стены, sliding делает свою работу; небольшой spring добавляет drift away. При реальном clip (dist=0 inside wall) escape доминирует через большое значение из degenerate branch.

**Альтернатива была:** drop margin to 0.05 и оставить override. Отвергнуто — это **не лечит** случай compound NW corner, где две стены сходятся и могут одновременно стопить unit. Аддитивная модель работает даже при стене-в-стене (опытные impulses суммируются и могут двигать unit out).

**Почему не "только при speed < 0.3"**: потому что unit может ИДТИ внутри стены (escape проигрывает velocity), но это не нужно — sliding и так разрешает корректное движение вдоль стены.

**P2. Door outside cells не получают NavInBuilding flag.**

Post-process в `bakeNavPass`: после `applyNavBuildings` пройти по всем `WallSegment` entities с `OpeningKind == OpeningDoor`. Для каждой двери вычислить outside cell (через `outward * 0.7`, как в Pass 4 transition). Сбросить `NavInBuilding` бит на этом cell.

**Почему именно door outside cells:** door — это **намеренный** transition point. Если cell, в которую дверь выходит, оказалась внутри chужого building footprint, это не "interior of B", это "exterior of A через дверь A". Бит "interior" неприменим.

**Чего НЕ делаем:** не очищаем NavInBuilding в произвольных cells вокруг footprint (это сломает intent — interior должен быть accessible только через doors). Только door outside cell — 1 cell на дверь.

**Почему этого хватит:** после фикса cell (Surf, 32, 37) становится Cost=4 open surface (не NavInBuilding). Pathfinder может expand из любого walkable neighbour. Cell (Surf, 32, 38) тоже — обе clear → реальные двери compound становятся доступны as plain surface entries.

**P3. Level-junction edge cost поднимается с 1 до 10.**

`spatial_bake_transitions.go` константа `levelJunctionCost: uint8 = 1` → `10`.

**Почему 10, не 5:** door edge cost=3, surface step cost=4 (open) or 8 (NavInBuilding override). Junction cost ≥ 10 гарантирует что доходить через реальную дверь дешевле для любого маршрута, который **может** дойти через дверь. Junction остаётся fallback для случаев когда door path несуществующ (например, wing без дверей на этом storey).

**Почему не убираем junction edge:** Phase 17.9 не доказала что compound_south PASS работает БЕЗ junction edges. Возможно при некоторых spawn-позициях через junction короче (вход через E wing → junction → M wing center). Сохраняем feature, понижаем приоритет.

**P4. Не меняем geometry compound generator.**

`gen/buildings/compound.go` остаётся flush-touching. Phase 17.9 проверяет что pathfinding works on flush geometry. Если после M1+M2+M3 compound_north всё ещё < 8/8, **только тогда** в Phase 18+ переоценим архитектурный выбор.

---

## Milestones

### M1 — Escape recovery additive (HIGH PRIORITY, 1 day)

**Файлы:** `systems/unit_movement_walls.go`.

Изменение `reflectAgainstWalls`:

```go
const escapeMargin float32 = 0.30
const escapeSpeed float32 = 2.0
const escapeBlendWeight float32 = 0.5  // новая константа
...
// Existing escape loop — без изменений до cumulative escapeX/Z.
...
// Заменяем:
//   if escapeActive { return escapeX, escapeZ }
// на:
if escapeActive {
    rvx = velX + escapeX * escapeBlendWeight
    rvz = velZ + escapeZ * escapeBlendWeight
    // Fall through into main slide loop with combined velocity.
}
```

После — main slide loop работает как обычно, обрабатывая wall crossing с smoothed velocity.

**Acceptance:** `ai_compound_north` 4-cell stuck на NW corner устраняется — юниты должны drifting east-ward вдоль M north wall до opening at X=31.4-32.6.

### M2 — Door outside cells exclusion from NavInBuilding (HIGH PRIORITY, 1 day)

**Файлы:** `systems/spatial_bake_nav.go`.

После `applyNavBuildings(...)` добавить sweep:

```go
// Phase 17.9: door outside cells are intentional transition points,
// not building interior. Clear NavInBuilding bit so pathfinder can
// approach them from open terrain.
for cc, walls := range wallsByChunk {
    if cc != rec.cc {
        continue
    }
    for i := range walls {
        w := &walls[i]
        if w.w.OpeningKind != components.OpeningDoor {
            continue
        }
        // Compute door's outside cell.
        sa := float32(math.Sin(float64(w.w.Yaw)))
        ca := float32(math.Cos(float64(w.w.Yaw)))
        centreT := w.w.OpeningCenterT * w.w.Length
        // ... cx, cz, outward (need CoverDirection / WallSpec.OutwardNormal — see Pass 4)
        // gj := floor(outsideZ); gi := floor(outsideX); ...
        grid.Cells[idx].Flags &^= components.NavInBuilding
    }
}
```

**Сложность:** `outward` лежит на `CoverDirection`, но он pre-spawned. Если pre-spawn order — bake вызывается до spawn — взять `OutwardNormal` напрямую из `WallSpec`. Решение: pass-1 теперь читает `walls` через тот же snapshot что и rasterizeWall, плюс **`OutwardNormal`** добавляется в `wallEntry`.

**Acceptance:** `(Surf, 32, 37)`, `(Surf, 32, 38)`, `(Surf, 37, 32)`, `(Surf, 38, 32)` для compound становятся обычными Cost=4 cells. Pathfinder с (32, 58) находит **прямой** путь через N wing south door (~15 cells вместо 30+).

### M3 — Junction edge cost bump (LOW PRIORITY, 0.5 day)

**Файлы:** `systems/spatial_bake_transitions.go`.

```go
const levelJunctionCost uint8 = 1  // → 10
```

И обновить comment с обоснованием.

**Acceptance:** в `ai_compound_north` после M1+M2, pathfinder picks N south door (cost=3) over junction (cost=10). Лог `[nav] OK len=X via-level=true` остаётся при path через level (через door и level), но не через junction-only.

### M4 — Regression run всего test suite (0.5 day)

`go run . -test=ai_compound_north`, `-test=ai_compound_south`, ..., все 10 сцен. Сравнить результаты с baseline:

| Scene | Before | Target |
|---|---|---|
| `ai_door_south` | PASS 8/8 | PASS 8/8 |
| `ai_door_north` | 7/8 | 8/8 |
| `ai_door_east` | PASS 8/8 | PASS 8/8 |
| `ai_door_west` | PASS 8/8 | PASS 8/8 |
| `ai_compound_south` | PASS 8/8 | PASS 8/8 |
| `ai_compound_east` | 6/8 | 8/8 |
| `ai_compound_north` | **0/8** | **≥ 6/8** |
| `ai_compound_west` | 6/8 | 8/8 |
| `ai_office_front` | PASS 8/8 | PASS 8/8 |
| `ai_far_building` | 7/8 | 8/8 |

**Acceptance:** ≥ 9/10 PASS (allowing compound_north to stay partial since corridor geometry remains tight, but no regressions from current baseline).

---

## Closure criteria

Phase 17.9 закрыта когда:

1. `ai_compound_north` ≥ 6/8 (значимое улучшение с 0/8; полное PASS 8/8 — bonus, не обязательное).
2. Не регрессировали 6 PASS сцен (door_south, door_east, door_west, compound_south, office_front).
3. 3 стрэглер-сцены (door_north, compound_east/west, far_building) → 8/8 или сохранили текущий уровень.
4. Aggregate ≥ 9/10 PASS (хотя бы 8 из 10 на 8/8, остальные ≥ 7/8).
5. Code review pass: новая `escapeBlendWeight` константа документирована, M2 sweep не зацикливается, M3 cost change объяснён.
6. Memory обновлена: `project_phase_17_8_progress.md` отмечает закрытие 17.9; `feedback_*` добавляется если выявленный поведенческий паттерн (escape additive) — load-bearing для следующих фаз.

После closure Phase 17.8 closeable. Если compound_north stuck < 6/8 даже после M1+M2+M3, открывается Phase 17.9.5 — там либо compound generator gap, либо wall ORCA constraints.
