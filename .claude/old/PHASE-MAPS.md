# PHASE-MAPS — World manifest + эталонные карты (часть 2 мини-трека «Карты»)

Дата: 2026-07-03. Часть 1 (PHASE-VISION) закрыта: 21/21 PASS + HASH OK.
Гейт всех майлстоунов: `scripts/replay_gate.sh` (сцены игнорируют -map ⇒ хэши неизменны).

## P-решения

- **P1. Формат — JSON**, `maps/<name>.json`, stdlib, hand-editable. Это авторский
  контент, не сейв — бинарник не нужен. Один файл = одна карта.
- **P2. Манифест несёт generator-спеки, не геометрию:** terrain-параметры + список
  зданий (template + seed + pos + params), дорожный граф, траншеи, реки. Y зданий
  резолвится при загрузке через GroundHeight (как world_data сегодня).
- **P3. TerrainParams** (Seed / WavelengthM / Octaves / Lacunarity / Persistence /
  AmplitudeM) — package-var в systems/noise.go, `SetTerrainParams` строго до создания
  мира (startup-only; permTable пересобирается от сида). Дефолт = сегодняшние константы
  бит-в-бит.
- **P4. Один потребительский путь:** `mapDef` строится либо кодом (`defaultMapDef()` —
  текущий мир), либо из JSON; world_data-функции читают `worldMap`. `mainWorldBuildings`
  (нужен ai_main_* сценам) = `defaultMapDef().buildingPlans()` — один источник истины.
- **P5. Сцены важнее карт:** при `-scene` флаг `-map` игнорируется полностью (терраин
  дефолтный) — регрессионная сюита не зависит от карт.
- **P6. SaveDir per map:** `./save/<map-name>` для именованных карт; дефолт остаётся
  `./save/world-default` (существующие сейвы живы).
- **P7. Contact cap = 128** (WS-E ш.2): LRU-выселение в serial upsert-пассе — сначала
  самые старые Unknown, затем старые остальные; PlayerSet-пин не выселяется никогда.
  Детерминизм: кандидаты в порядке Filter, sort.SliceStable по (unknown, LastSeenTime).

## M-майлстоуны

- **M1.** ✅ 2026-07-03. `systems.TerrainParams` + `SetTerrainParams` (дефолт бит-в-бит
  прежний); `map_def.go` (mapDef + builders + `defaultMapDef`); world_data → worldMap;
  `mainWorldBuildings` = `defaultMapDef().buildingPlans()`. Сцены PASS на дефолт-пути.
- **M2.** ✅ 2026-07-03. `-map=` + JSON-лоадер (`maps/<name>.json`, сцены игнорируют
  флаг), `systems.SaveDir` var → `./save/<map-name>` для именованных карт.
- **M3.** ✅ 2026-07-03. `maps/flat|hills|mountains|valley.json`; все четыре бутятся без
  паник (valley — с рекой через хайвей ⇒ мост). Визуальная прогулка — владелец.
- **M4.** ✅ 2026-07-03. `contactCap = 128` + `enforceContactCap` (LRU: старые Unknown →
  старые остальные; PlayerSet-пин не выселяется; кандидаты в Filter-порядке +
  SliceStable ⇒ детерминизм).

## Closure

1. `-map=flat|hills|mountains|valley` играются; ландшафт различим от равнины до гор.
2. Дефолтный запуск бит-в-бит прежний (replay-хэши сцен не изменились).
3. Именованная карта сейвится в свой каталог; дефолт — в старый.
4. Contact-население ограничено cap'ом (WS-E ш.2 закрыт).
5. Ridge-сцены на горных картах — задел на Detection 2.0 / доводку VISION.
