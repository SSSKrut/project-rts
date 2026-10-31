# PHASE-BUILDING-UX — приказы на здание: разные исполнения, честное превью, выбор цели

Открыта 2026-07-31 по owner-фидбеку: «нет разницы, какой приказ на занятие
здания отдавать — выполняется одно и то же». Диагноз подтверждён кодом: все
три пункта попапа (Clear / Garrison / Hidden) исполняются одним
interiorIntent-путём FormationSystem; различия только в RoE-флагах и
completion. Garrison «распределиться по окнам» (спека GAMEDESIGN §Orders)
существует лишь в ghost-превью; Occupancy/ShootingArc на окнах (Phase 5)
никто не читает.

## P-решения

- **P1. Единый планировщик расстановки.** `systems/building_slots.go`:
  `BuildingSlotPlanner.PlanSlots(building, policy, count, facing) []BuildingSlot`
  — чистая детерминированная функция от живого мира. Его читают И
  FormationSystem (исполнение), И ghost-превью, И маркеры назначений —
  превью не может врать по построению.
- **P2. Политика на kind:** Garrison → `SlotWindows` (огневые позиции у
  окон: точка = центр проёма − outward×0.6, yaw = outward, переполнение →
  комнаты-резерв); OccupyBuilding → `SlotRooms` (этажи → комнаты
  round-robin, как сейчас); OccupyBuilding+EngagementOverride (Hidden) →
  `SlotHidden` (комнаты, прижатые к центрам — подальше от проёмов);
  ClearBuilding → `SlotGroundFloor` (сначала весь отряд на нижний этаж).
- **P3. Назначение слотов stateless по roster-index** (как floor-spread):
  ноль save-стейта, детерминизм бесплатно. Sub-state «какой юнит у какого
  окна» на Order (GAMEDESIGN §105) отложен до Build/joint-приказов.
- **P4. Выбор «куда» без новых жестов:** этаж — попап L<n> (есть); комната —
  точка клика внутри footprint выбирает комнату уровня (raw press-point до
  снапа на центр L0); сторона окон — `OrderParamFacing` в планировщике
  (сектор-приоритет), UI-провод для facing-drag через попап отложен, дефолт
  Garrison = круговая оборона в порядке спавна стен.
- **P5. Clear and occupy = цепочка приказов:** ClearBuilding (completion
  hostiles==0 уже есть) + автоappend OccupyBuilding — никакой новой
  механики зачистки; покомнатный CQB-sweep остаётся Phase 17.8.
- **P6. Парковочный фейсинг у окна пишет FormationSystem** (member в
  пределах 0.8 м от оконного слота → Motion.Yaw = slot.Yaw); стойка у окна
  остаётся StanceController'у (v0 не трогает).

## Милестоуны

- **M1** — планировщик + Garrison у окон + parked-yaw; сцена
  `ai_garrison_windows` (все окна укомплектованы, фейсинг наружу,
  остальные внутри). Гейты.
- **M2** — превью пункта попапа = PlanSlots той же политики (Hidden —
  crouch-ghost, Clear — L0, Garrison — окна из планировщика вместо
  greedy-first-N); room-pick точкой клика для Occupy L<n> (fallback —
  центр уровня, если raw-точка вне комнат). Гейты.
- **M3** — SlotHidden + Clear-цепочка (Clear → append Occupy). Гейты.
- **M4** — маркеры назначенных слотов при выделенном отряде (стиль
  committed-маршрутов #17) + inspector-детали приказа (Garrison
  «windows n/m», Hidden «HoldFire» pill). Гейты.

## Ход работ (2026-07-31)

- M1 вскрыл и закрыл два фундаментальных дефекта (ISSUES #26): пустые
  LevelNavGrid'ы (бейк до спавна детей) и заблокированные inflate'ом клетки
  лестничных якорей. Плюс: findPath-подстановка ближайшей проходимой клетки
  для заблокированной цели; completion Garrison ждёт укомплектованных окон
  (иначе приказ завершался «по входу» и расстановка обрывалась); снап
  фейсинга на completion (гонка последнего бойца с 100 мс формации).
- M2: превью пункта попапа через `PlanSlots` той же политики; `PressRaw` в
  rmbSession; room-pick общим `pickRoomTarget` (превью == коммит);
  Hidden-превью в crouch. `drawGhostInBuilding`/`drawGhostAtWindows`
  заменены планировщиком.
- M3: SlotHidden (RoE-override на приказе) + SlotGroundFloor; авто-чейн
  Clear→Occupy уже существовал в резолвере.
- M4: `drawAssignedBuildingSlots` — маркеры слотов выделенного отряда
  (плашка + черта фейсинга у оконных); Inspector: «HF» pill на приказе с
  RoE-override, target-лейблы building-приказов; прогресс Garrison =
  manned/expected.

## Закрытие

Все ai_* сцены PASS+HASH, saveload OK; ai_garrison_windows в обоих гейтах.
Попап-пункты дают ВИДИМО разные расстановки в превью и в исполнении.
Рендер-половина (превью пунктов, маркеры слотов, pill) — компил-чек гейтами,
нужен ручной плейтест.
Отложено явно: sub-state назначений на Order, facing-drag сектора для
Garrison (планировщик уже принимает facing — не проведён только UI-жест),
CQB-sweep, Suppress fire пункт, joint Garrison, стойка у окна (Stance у
подоконника — StanceController).
