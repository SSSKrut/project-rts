# PHASE-WS-B — Детерминизм-слой (одна машина)

Статус: **закрыта 2026-07-03** (M1-M5 в один день; см. итог внизу).
Дата старта: 2026-07-03. Источник: `REFACTOR-PLAN.md` §WS-B (пункты согласованы 2026-06-12).
Роль в общем треке: **WS-B → Карты → 18.9 → 19 → 22-lite** (см. ROADMAP «Следующий шаг»).

## Цель

Симуляция воспроизводима в объёме «один бинарь / одна машина»: два headless-прогона одной
сцены дают побайтово равные хэши состояния. Это (1) фикс реальных SP-багов (гонка ORCA —
настоящий data race; нестабильный порядок контактов), (2) предусловие Jolt-физики Phase 19
(fixed timestep), (3) стенд будущего ИИ (replay-harness = evaluation-стенд, быстрые headless
эпизоды).

**Не-цели:** кросс-машинный детерминизм (закрыт P2 REFACTOR-PLAN); SimID-замена
entity-ID-хэшей (speedJitter, rngSeed) — бессмысленно, пока история спавнов
камеро-зависима, отложено до residency (Phase 24); изменение геймплейного поведения.

## P-решения

- **P1. Sim-тик 60 Hz fixed, frame-locked (реализация M4).** `App.Advance()` гонит
  `int(TimeScale)` тиков по `SimDt = 1/60 s` за кадр рендера; real-dt в сим не входит
  вообще (maxTickDelta-clamp удалён за ненадобностью). При fps < 60 игра замедляется —
  осознанный трейд до WS-D ш.8 (камера уйдёт из тика → можно перейти на real-time
  аккумулятор). Пауза = один zero-delta pass, чтобы orbit/camera жили (поведение как
  раньше); OrbitSystem применяет мышь строго раз за кадр (frame-guard).
- **P2. TimeScale = целое число тиков на кадр**; 0 пауза, 1/2/4/8 скорости. Sim
  продвигается только целыми тиками; `App.Tick(delta)` остаётся сырым входом для
  sandbox/тестов.
- **P3. Единые часы.** `UpdateContext` получает `SimNow float64` (= TickIndex × 1/60) и
  `TickIndex uint64`. Все приватные float32-аккумуляторы систем удаляются; потребители
  берут `float32(ctx.SimNow)`. `SquadService.Clock()` становится тонкой прослойкой над
  каноническим временем (или удаляется вместе с `SetClock`-вызовами main.go:883-884).
- **P4. LOD-интервалы не переводятся в тики:** при фиксированном SimDt арифметика
  `elapsed` — целочисленные наносекунды time.Duration, планирование уже детерминировано.
- **P5. Replay-хэш.** FNV-1a по фиксированному списку компонент (WorldPos, Motion, HP,
  Threat, Suppression, Stance, ActionQueue, OrderState/OrderProgress, CommandRoster)
  каждые 100 тиков; headless-режим пишет хэш-лог в файл (`-replay-hash=path`). Порядок
  итерации — порядок Filter: стабилен при идентичной истории спавнов (один бинарь, одна
  сцена, один seed) — этого достаточно для одномашинного replay.
- **P6. Гейты неизменности поведения:** вердикты всей ai_*-сюиты и медианы Ctrl+P не хуже.

## M-майлстоуны

Порядок: race-фиксы (M1-M2) первыми — независимы и немедленно ценны; часы/тик (M3-M4)
затем; harness (M5) последним — до M3/M4 хэши заведомо нестабильны (float-время).
Каждый M = компилируемый коммит + гейты.

- **M1. ORCA-снапшот.** `core.SpatialEntry` += `VelX, VelZ, Radius` (заполняются в
  serial rebuild из Motion/Collider); новый `ForEachEntryInRadius`; ORCA-блок
  `unit_movement_step.go` читает только снапшот — ноль live-reads чужих компонент в
  параллельной секции (сейчас: `posMap/motionMap/colliderMap.Get` соседей — data race).
  Alive-check в этом колбэке снимается осознанно: ничего не дереференсится, одно-тиковый
  фантом мёртвого соседа безвреден. Гейт: `-race` headless (ai_door_south, ai_main_m1)
  без репортов; до фикса репорт воспроизводится (валидация гейта).
- **M2. Детерминированный порядок.** `contact_detect.go`: mutex-мерж → `workerContacts
  [][]contactRec` по индексу воркера (паттерн WeaponSystem), конкатенация по порядку
  индексов ⇒ порядок upsert/NewEntity стабилен run-to-run. `nav_service.go`
  (`closestSurfaceEntry` BFS): сиды из range-по-map → слайс с сортировкой по ключу
  NavNode. Гейт: `-race` чисто; повторные прогоны дают одинаковый состав/порядок контактов.
- **M3. Единые часы.** SimNow/TickIndex в `core.UpdateContext`; удалить 14 приватных
  аккумуляторов: particle, map_ping_decay, circle_patrol, micro_path, survival_instinct,
  unit_movement, weapon, utility_evaluator.clock, threat_decay, squad_macro_path,
  level_visibility.clock, contact, stance_controller, threat; + канонизировать
  SquadService.clock / DamageService.clock-замыкание / MapPingService. Гейт: grep
  `elapsed +=|clock +=` по systems/ пуст; ai_* PASS.
- **M4. Fixed timestep.** Аккумулятор в main loop (обе ветки — обычная и headless,
  main.go:877 уже 1/60); `App.Tick(dtFixed)` вызывается 0..8 раз за кадр;
  LOD-интервалы → счётчики тиков; input/камера остаются на real-time dt. Гейт: пауза и
  1/2/4/8× работают, играбельность на глаз, медианы профайлера в бюджете, ai_* PASS.
- **M5. Replay-harness.** `-replay-hash=path`, FNV-хэш каждые 100 тиков, скрипт двойного
  прогона + diff; вшить в ai_*-гейт как постоянный. Гейт: два прогона каждой ai_*-сцены →
  идентичные хэш-логи.

## Closure

1. `go test -race ./core/` зелёный; `-race` headless-прогоны двух сцен + 30 с ручного
   движения толпы — ноль репортов.
2. Два headless-прогона каждой ai_*-сцены → побайтово равные хэш-логи (постоянный гейт).
3. Один источник времени: grep не находит приватных аккумуляторов; SimNow/TickIndex в ctx.
4. Sim 60 Hz; TimeScale = целые тики; maxTickDelta-clamp заменён аккумулятором.
5. Медианы Ctrl+P не выросли; вся ai_*-сюита PASS.
6. ROADMAP «Что сейчас работает» + CLAUDE.md (scheduler-секция) обновлены; план в old/.

## Итог закрытия (2026-07-03)

- **M1** ✅ гонка доказана экспериментально (порог пула временно 0: 3 × DATA RACE до
  фикса, 0 после). Попутная находка: `SerialThresholdHint = 64` — все ai_-сцены и текущая
  главная сцена (~48 юнитов) гоняют ParallelFor* серийно; гонка была латентной до первых
  64+ юнитов.
- **M2** ✅ contact-мерж per-worker (порядок восстановлен конкатенацией по chunkIdx),
  BFS-сиды сортированы по (I, J).
- **M3** ✅ `ctx.SimNow`/`TickIndex`; 14 аккумуляторов снесено; у contact/utility — mirror-
  assign (читатели в sibling-файлах). Попутный фикс: 2×-часы у circle_patrol / micro_path /
  stance_controller / threat (многотиерные системы аккумулировали Delta каждого тиера).
- **M4** ✅ frame-locked вариант (не real-time аккумулятор — камера пока живёт внутри тика,
  см. P1); `App.Advance()`, orbit frame-guard, sandbox остаётся на `App.Tick`.
- **M5** ✅ `systems/replay_hash.go` (FNV-1a: юниты / приказы / контакты каждые 100 тиков),
  флаг `-replay-hash`, `scripts/replay_gate.sh` (19 сцен × 2 прогона + diff).
  door_south (36 точек) и main_m1 (37 точек, тик 3600) — побайтово идентичны с первого
  прогона.
- Полный `replay_gate.sh` прогнан 2026-07-03 (на VISION-M1 коде): **19/19 PASS,
  19/19 HASH OK** — closure-критерий 2 подтверждён всей сюитой. Остаток: медианы Ctrl+P
  и пауза/скорости/орбита — глазами владельца, план → old/ после ревью.
