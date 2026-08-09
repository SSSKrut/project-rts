# Known issues

Live bug list collected during playtesting. Each entry: short repro, suspected
cause, where to look first. Closed entries move to git history (delete the row).

## Phase 10 — Interface foundation

### 2. Лаг на ~0.5 с после Tab-переключения

**Repro.** Юниты движутся (RMB-MoveTo дан, идут по пути). Нажать Tab —
3D ↔ map swap. На полсекунды юниты замирают, потом продолжают.

**Suspected cause.** При Tab меняется размер 3D-RT (`scene3DRT.EnsureSize`)
+ `panelMgr.Recompute`. Стол времени `rl.GetFrameTime()` за этот кадр
скорее всего нормальный, но возможно frame-time spike от GPU-realloc'а
проходит как один большой `dt` ⇒ системы получают аномальное Delta, или
наоборот — кадр блокируется и `app.elapsed` отстаёт.

**Где смотреть.** `core/app.go::Tick` (scaled delta), `ui/scene3d.go::EnsureSize`
(GPU realloc). Можно проверить заголовком кадра через `time.Now()` до и
после `BeginTextureMode`/`EndTextureMode` сразу после Tab.

---

## Закрытые issue (history)

### 1. Squad-маркеры на карте «телепортируются» из-за LOD — closed in Phase 11 (M11.7)

Фикс: world-space lerp с factor 0.18 в
`ui/map_render.go::drawSquadMarkers`. Marker позиция хранится в
`MapRenderCtx.SmoothedSquadPos` (map[ecs.Entity]WorldPos), который main.go
держит между кадрами; lerp применяется в world-space до проекции, чтобы pan/
zoom карты не лагал за движением.

### 3. Multi-squad selection + RMB → отряды распадаются — closed in Phase 11 (M11.5)

Фикс: `groupSelectionByOwner` собирает уникальные squad'ы из selection;
`resolveRMBOrder` вызывает `IssueOrder` на каждом без `squadService.Leave`.
Soloists получают per-unit ActionQueue MoveTo как раньше. H (Stop) тоже
использует тот же split (CancelAllOrders для squad'ов + ActionStop для
soloists).

### 4. Юнит далеко от анкора не движется — closed in Phase 11.5

Корень был в архитектуре «LOD-tier-gated simulation»: симуляционные системы
смотрели только entity своего тира, юниты вне Relevant ring'а не получали
свежий MoveTo. Phase 11.5 переоснастил это к universal simulation: tier-gating
полностью убран из UnitMovement / Vision / Formation / SquadMacroPath /
OrderResolver (один Filter, один Update pass), и LOD-маркеры с юнитов сняты
(`main.go` больше не зовёт `lodRelevantMap.Add(...)`, `LODSystem` исключает
Unit-архетип). Системы теперь touch'ат каждый юнит каждый тик независимо от
расстояния до anchor'а.

### 5. Юниты иногда «застревают» во время выполнения приказа — closed in Phase 11.5 (P8)

Фикс: `CohesionLeashCoeff = 8.0` (было 4) + `cohesionEjectionEnabled = false`.
Stragglers больше не выкидываются из ростера; формация продолжает писать
MoveTo по offset'у и юнит догоняет. Детект stragger'а оставлен для будущей
Tactical AI (Phase 15), которая включит ejection обратно через doctrine
override.

### 6. Юниты на Dormant tier не двигаются — closed in Phase 11.5

Гипотеза подтверждена: `LODSystem` действительно не назначал `LODDormant`
unit'ам — фильтр был ограничен общим WorldPos-классом, юниты были инициированы
с `LODRelevant` и никогда не получали свап. Phase 11.5 решил это не точечно
(swap-fix), а архитектурно: убрал LOD-маркеры с юнитов вообще + удалил
tier-gating из симуляции. Юниты теперь всегда видны симуляции независимо
от расстояния. Trade-off (Dormant юнит через тонкие препятствия) тоже
устранён — раньше Dormant-tier-update делал шаг 2.5 м, теперь шаг — нормальный
per-tick.
