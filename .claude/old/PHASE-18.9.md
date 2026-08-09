# PHASE-18.9 — Save/Load (WS-F, M0 из GAME-VISION)

Полный снапшот мира → загрузка в чистый процесс → продолжение боя. Продуктовая несущая
длинных операций (GAME-VISION §7), инженерный гейт всех последующих фаз. Оценка 8–10 дней.

## Статус: M1–M6 реализованы 2026-07-07, гейт зелёный

`scripts/saveload_gate.sh` — 4/4 SAVELOAD OK (los_open / los_creep / compound_main /
main_m1): хэш-суффикс прогона B (save@1000 → чистый процесс → load → 2000) побайтово
равен хвосту непрерывного прогона A. Классы багов, найденные гейтом непрерывности:

1. **Every-tick lastRun** (главный): для систем с `ActiveEvery: 0` восстановление часов
   обязано ставить `lastRun = elapsed` — иначе первый пост-load тик получает
   `ctx.Delta = elapsed − 0` (16+ секунд) и unit_movement интегрирует их одним шагом.
   Интервальные (>0) тиры восстанавливаются `replayFireGrid` — решётка ДРЕЙФУЕТ
   (интервалы не кратны SimDt=16666666ns: 250ms срабатывает каждые 16 тиков), поэтому
   она replay'ится пофайрово, а не floor'ом.
2. **float32 prev-now поля** (`WeaponSystem.lastTick`, `ThreatSystem.lastTick`):
   dt = float32(now)−float32(last) битово ≠ float32(Delta) — нужен `core.PostLoadHook`
   (`PostLoad(simNow)`), `App.NotifyLoaded()` рассылает после RestoreClock.
3. **Приватные леджеры** (`SurvivalInstinct.occupancyClaim`): восстановление подсчётом
   живых `TacticalOverride.AssignedSlot` (тот же PostLoadHook).
4. **`components.Level.Name string`** — POD-гейт отказал в memcpy; поле стало `[8]byte`
   + `Label()`.

Форензика (постоянная): `RTS_HASH_EVERY=1` (хэш каждый тик), `RTS_DUMP_AT=N` +
`RTS_DUMP_FILE=` (текстовый дамп hashed-полей + FNV хайтмапов чанков), лог load-прогона
начинается с хэша load-тика. Методика: state@load-тик равен → подозревай приватное
состояние систем и решётки тиров; не равен → снапшот.

Отклонение от плана: RebuildTransitions не нужен (bake Pass 4 безусловен каждый Active-тик);
StreamingMap переклассифицирован Codec → FromManifest (пуст после удаления demo-графа).
M6: F5 quicksave → `save/<map>/quick.rtss`; F9 quickload через exec с `-load=`
(in-place reload требовал бы полного teardown систем — честный путь прототипа, Linux).

## P-решения

**P1. Full-pool snapshot через `Unsafe().DumpEntities()` / `LoadEntities()`** (Ark v0.8.0,
проверено в исходниках модуля). Пул восстанавливается дословно — IDs, поколения, free-list —
на свежем мире; все `ecs.Entity`-ссылки внутри компонентов валидны без пересопоставления.
Remap-таблицы НЕ существует в кодовой базе (REFACTOR-PLAN P3: remap = скрытая XL, запрещён).
Версия Ark закреплена: апгрейд = повторная проверка контракта LoadEntities (table-0 Extend,
reservedEntities).

**P2. Сейвим ВСЕ живые сущности — включая чанки, пропы, частицы, ThreatSource.**
Отклонение от эскиза WS-F (там skip-список сущностей): выборочный respawn после загрузки
раздаёт другие entity ID и меняет порядок строк таблиц ⇒ ломает непрерывность replay-хэша
классом расхождений «та же логика, другой порядок». Сейв всего убирает класс целиком; цена —
мегабайты в файле (чанк 16.9 KB × ~сотня — приемлемо). Классификация остаётся только
**per-component**: `Save` (memcpy) / `Skip` (не писать, восстановить маркером) / `Custom`.
Skip: `ChunkMesh` (GPU; на load ставим `MeshDirty`). Всё остальное на сущностях — POD
fixed-size (проверено: CommandRoster/MacroPath/Heightmap/FormationCustomSlots — массивы,
не слайсы) ⇒ Save. «Null-on-save + перевыбор за тик» ЗАПРЕЩЁН для всего, что влияет на
траекторию симуляции: перевыбор ≠ гарантированно тот же выбор, хэш-непрерывность умрёт.

**P3. Порядок строк таблиц сохраняется.** Per-archetype блобы пишутся в порядке итерации
Filter (= порядок таблицы); на load компоненты добавляются через `Unsafe().Add` + memcpy в
`Unsafe().Get` в порядке файла ⇒ таблицы собираются в исходном порядке ⇒ итерация систем
после load идентична непрерывному прогону.

**P4. Бинарный формат v1, свой кодек** (не ark-serde/JSON: объём, скорость, float round-trip).
LE. Header: magic `RTSS`, версия uint16, имя карты, SimNow float64 / TickIndex uint64 /
frameIndex, **schema-hash** (FNV по списку «имя типа + размер» всех зарегистрированных
компонентов — расхождение = честный отказ загрузки; миграций между версиями прототипа нет).
Секции: EntityDump → архетипы (маска компонентов + count + блобы) → ресурсы → futures.
Atomic via tmp+rename (как WriteChunk). Кодеки per-component — delta-слой ляжет сверху,
если MP-дверь откроется; NetID / delta / interest — НЕ строить.

**P5. Ресурсы — три класса** (18 зарегистрированных):
- **FromManifest** (не сейвим, восстают при буте из `-map`): Rivers, RoadGraph, TrenchNetwork,
  BuildingPlanList, PropTypeRegistry. Header несёт имя карты; `-load=` сам поднимает манифест.
- **Rebuild** (не сейвим, пересобираются из живых сущностей load-пассом или существующим
  путём): TerrainChunkIndex, PropChunkIndex, BuildingChildIndex, BuildingPlanIndex,
  TransitionRegistry (spatial_bake wipe+repopulate), MapMarkerCache (@250 ms сам),
  SpatialHash (rebuild-система каждый тик), CoverSlotIndex. Каждому — строка в таблице M1
  с указанием: «путь существует» или «нужен load-pass».
- **Codec** (кастомная сериализация): StreamingMap, EventLog, ContactRegistry (если не
  докажем rebuild из живых Contact-сущностей — решается в M1), FormationPresets,
  SymbologyPresets → `save/symbols.json` (закрывает deferred 18.5).

**P6. Полнота таблицы — runtime-гейт.** При сейве итерируем `ecs.ComponentIDs(world)` +
`ecs.ComponentInfo`: каждый зарегистрированный тип обязан иметь политику, незнакомый = panic.
Регистрация в Ark ленивая ⇒ гейт покрывает ровно реально используемые типы. Новый компонент
без строки в таблице валит первый же сейв в гейте — это и есть compile-time-по-духу.

**P7. Часы и load-бут.** Load = обычный бут (AddResource + InitUI + системы) с манифестом
из header'а, БЕЗ спавна стартовых сущностей → `LoadEntities` → компоненты (P3) → ресурсы →
`App` восстанавливает SimNow/TickIndex/frameIndex → первый тик. Маркеры (`*Processed`,
`BuildingsProcessed`, отсутствие `HeightmapDirty`) сохранены ⇒ терраин-пайплайн не
перерабатывает чанки повторно; `MeshDirty` из Skip-восстановления даёт rebuild мешей.
TimeScale не сейвим (UI-состояние, load = пауза).

**P8. Гейт непрерывности.** Прогон A: сцена N=2000 тиков, хэш-лог. Прогон B: 1000 тиков →
save (`-save-at=1000`) → чистый процесс → `-load=` → 1000 тиков. Суффикс хэшей B ==
хвост A побайтово. `scripts/saveload_gate.sh` — постоянный гейт рядом с replay_gate.
Известный риск: расхождение через float-order в местах, где итерация зависит от порядка,
не зафиксированного P3 (например, порядок чанков в bake-ранжировании cover-слотов).
План Б при честном тупике: ослабить до verdict-эквивалентности + равенство populations —
только с явного согласия владельца, с записью причины здесь.

## M-милстоуны

- **M1. Классификация.** `systems/save_spec.go`: политика per-component (Save/Skip/Custom) +
  таблица 18 ресурсов (класс + rebuild-путь/кодек) + runtime-гейт полноты (P6).
  Инвентаризация: headless-прогон, дамп зарегистрированных типов, сверка с таблицей.
- **M2. Writer.** `systems/save_world.go`: header + schema-hash + EntityDump + per-archetype
  блобы в порядке Filter + atomic write. Флаг `-save-at=N` для headless-сцен.
- **M3. Loader.** Load-бут (P7): `-load=path`, LoadEntities, re-add в порядке файла,
  rebuild-пассы ресурсов, часы. Первая ручная проверка: save в живой игре → load → бой идёт.
- **M4. Кодеки.** StreamingMap / EventLog / FormationPresets (+ContactRegistry по итогам M1),
  `save/symbols.json`.
- **M5. Гейты.** `scripts/saveload_gate.sh` (P8) на подмножестве ai_* сцен (минимум:
  ai_main_m1, ai_los_creep, ai_compound_main); replay_gate.sh без регрессий; полный прогон.
- **M6. Игровые хуки.** Quicksave/quickload (F5/F9 — свободны по hotkey-карте), слоты
  `save/<map>/slot_N.rtss`, минимальная индикация в HUD.

Порядок работы: M1→M4 подряд без промежуточных прогонов (batch-режим), M5 валидирует всё.

## Закрытие

- saveload_gate.sh зелёный (хэш-непрерывность через границу), replay_gate.sh 22/22 без
  регрессий, ручной цикл save→quit→load в живой игре на карте default и valley.
- Критерий REFACTOR-PLAN §7 закрыт: full-cycle ≥1000 тиков; remap-таблицы не существует.
- ROADMAP: 18.9 в закрытые, GAME-VISION M0 отмечен.
