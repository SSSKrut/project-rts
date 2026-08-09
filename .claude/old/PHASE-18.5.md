# Phase 18.5 — Fog of War + Symbology + Sensors (рабочий план)

Первая полноценная FoW-стадия. До этого Vision (Phase 7) делал бинарный cone-detect и писал в `Awareness.LastSeen`, но никакой "player perception layer" не было — мы рендерили ground-truth напрямую. Phase 18.5 вводит:

1. **Faction model** — каждый юнит знает свою сторону (PlayerFaction / EnemyFaction / NeutralFaction / WildlifeFaction). Только PlayerFaction юниты контрибутят в FoW игрока.
2. **Sensor pipeline** — `Sensors{Channels [4]}` модель с распределением по каналам (Optical / Thermal / Radar / Acoustic — MVP только Optical). Effective-range считается через **конфигурируемую falloff-функцию** × facing-profile × concealment-multiplier.
3. **Contact registry** — `Contact` entity per (observer, tracked) пара. Живёт навсегда, fade'ится по времени, удаляется только вручную. Авто-промоут классификации по CombatEvidence / CloseRangeID.
4. **NATO APP-6 symbology** — composable `SymbolSpec` (frame × affiliation × icon × echelon × modifiers), bake-to-texture cache, render на Map и в Inspector header'е.
5. **Symbol Editor panel** — workspace-aware builder с пресет-библиотекой. Открывается из top-bar toolbar'а.
6. **Top-bar toolbar** — context-sensitive action buttons (Symbol Editor / Formation Editor / Settings). Date+speed уезжают в правую часть.
7. **RMB context на контакте** — 3×4 quick-pick (affiliation × dimension) + "Change icon advanced…" + Reset to auto + Delete.

ROADMAP §Fog-of-War — FoW значится как Phase 18+ "когда landed", без жёстко зарезервированного номера. GAMEDESIGN §1 (interface) / §6 (RoE) — символика и FoW упоминаются как cross-cutting. COMMAND-MODEL.md — рассказ про event log затронут (контакт-классификация = potential notification source, но это вне scope этой фазы).

VisionSystem (Phase 7) поглощается ContactSystem'ом — один проход делает detection-math + Awareness write + Contact upsert. Старая `Vision{RangeM, AngleDot}` компонента deprecate'ится в пользу `Sensors`.

---

## Что в Phase 18.5 сознательно НЕТ

- **Thermal / Radar / Acoustic каналы.** Структура `SensorChannel[4]` готова и `SensorKind` enum заполнен, но MVP населяет только Optical. Радарные юниты, тепловизоры на технике — следующая фаза (после vehicles).
- **Probabilistic per-tick detection.** Детерминированный effective-range через falloff-функцию — единственный режим. Случайные rolls / "blinking contacts" не делаем (см. P-A1).
- **Movement как concealment source.** "Двигающийся юнит проще заметить" — отложен. Concealment MVP: stance + CoverMap. См. P-A4.
- **3D billboard символы.** Только Map + Inspector. 3D мир остаётся с кубиками (для своих) + ground-truth render для замеченных врагов (см. Track G). См. P-B2.
- **Multi-player contact registries.** `Contact.Observer` поле есть (FactionSide), но MVP пишет только PlayerFaction observers. MP-разделение — позже.
- **Vision providers from non-unit sources.** Здания / радары / БПЛА не контрибутят в FoW в этой фазе. Только Unit-entities с Sensors.
- **Click-on-3D-marker → camera focus.** Только click-on-Map работает. 3D-маркеры контактов не кликабельны (потому что 3D показывает только ground-truth, см. Track G — там специальный flickering, селекта нет). Юзер явно отложил.
- **Contact merging / splitting.** Один Contact == один Tracked entity. Если два контакта рядом — игрок видит две метки. Эвристики "это могут быть одни и те же люди" — позже.
- **Notification system на новый контакт.** Phase 13 / COMMAND-MODEL event log есть, но "вспышка + строка `new contact: hostile vehicle at NE`" — отдельная задача.
- **Echelon modifiers сложнее dot/line group.** APP-6 поддерживает HQ taskforce / reinforced / detached амэндменты — MVP только base echelon (team..battalion). Modifiers row в builder'е present как "later" placeholder.

---

## Структура фазы

Восемь tracks. **Tracks 18.5.0 + 18.5.A + 18.5.B критичны для playable MVP** — без них нет ни enemy юнитов, ни detection-модели, ни visible контактов. 18.5.C-G — UI обвязка, без неё контакты рендерятся плейсхолдер-цветным квадратиком, но играбельность не страдает.

- **18.5.0** Faction component + wildlife dummies — несколько статичных + несколько кружащих по waypoints EnemyFaction юнитов в стартовой сцене. UnitFactory.Spawn(pos, side).
- **18.5.A** Sensors model + ContactSystem (поглощает VisionSystem) — Sensors{Channels[4]} + falloff functions + facing-profile + concealment. ContactSystem пишет в Contact entities + в Awareness.LastSeen (для SurvivalInstinct).
- **18.5.B** Map / Inspector contact render + camera focus on double-click. Placeholder symbol (цветной квадратик с буквой affiliation) — финальная NATO-графика приходит в 18.5.C.
- **18.5.C** SymbolSpec data + bake-to-texture renderer + preset library (`save/symbols.json`). Map и Inspector переключаются с placeholder'а на real symbols.
- **18.5.D** Symbol Editor builder panel + workspace leaf integration (паттерн Formation editor).
- **18.5.E** Top-bar toolbar refactor (date+speed → right; action buttons left) + Settings panel + settings.json persist.
- **18.5.F** RMB context menu на контакте (3×4 quick-pick + advanced + reset + delete).
- **18.5.G** 3D enemy render с 2-sec fade tail после LOS-loss.

Порядок: 18.5.0 → 18.5.A → 18.5.B → 18.5.C → 18.5.D → 18.5.E → 18.5.F → 18.5.G. Strict — каждый track читает данные предыдущего. Можно объединять близкие в один коммит (например 18.5.C+D, 18.5.E+F), но 18.5.A крупный и заслуживает отдельной волны.

---

## Глобальные решения (cross-track)

### P-G1. Faction — enum компонента, не marker-tag

```go
type FactionSide uint8
const (
    PlayerFaction   FactionSide = iota
    EnemyFaction
    NeutralFaction
    WildlifeFaction
)
type Faction struct { Side FactionSide }
```

**Альтернатива:** marker `FriendlyAllegiance` (есть = наш, нет = чужой). Отвергнуто — масштабируется плохо, при добавлении третьей+ фракции придётся перекраивать. Enum даёт линейное расширение без рефакторинга.

`UnitFactory.Spawn(pos)` расширяется до `UnitFactory.Spawn(pos, side FactionSide)`. Все существующие call-sites в `main.go` дополняются `PlayerFaction` (один pass с grep+replace).

`Weapon.shouldFire` (Phase 14) дополняется faction-check — стрелять можно только в другую фракцию, не считая RoE-флагов. Хотя в MVP wildlife и enemy dummies не вооружены, gate всё равно ставим — иначе на первом тике faction-сетапа player начнёт стрелять по своим, если случайно совпадут позиции.

### P-G2. Contact — отдельная entity, никогда не auto-удаляется

```go
type Contact struct {
    Tracked          ecs.Entity      // ground-truth ref
    Observer         FactionSide     // на будущее MP
    EstimatedPos     WorldPos        // last-known
    LastSeenTime     float64         // global elapsed seconds
    PerceivedAffil   Affiliation
    PerceivedDim     Dimension
    Source           ClassificationSource
}
type ContactPlayerSet struct{} // marker — player override active
```

`Contact` несёт `AlwaysActive`, не привязан к chunk eviction. `EstimatedPos.Chunk` обновляется при каждом refresh.

**Не удаляется по таймауту.** Только fade'ится по age. Будут отдельные UX-моменты (Inspector "last seen 47s ago", Map alpha lerp). Юзер вручную удалит через RMB → Delete или через batch "Clear stale" (отложен).

**Альтернатива:** auto-delete после N минут. Отвергнуто пользователем — "когда появятся метки last seen, юзер сам решит". Запоминаем что в долгосрочной перспективе FoW не должен забывать без явного player intent.

### P-G3. Source precedence — захардкоженная цепочка

```
PlayerClassified  >  CloseRangeID  >  CombatEvidence  >  Sensor (Unknown)
```

Auto-rules никогда не downgrade'ят source выше своего уровня. PlayerClassified маркер (`ContactPlayerSet`) — terminal. "Reset to auto" в context menu убирает маркер, источник пересчитывается с нуля при следующем ContactSystem-проходе.

```go
type ClassificationSource uint8
const (
    Sensor          ClassificationSource = iota  // unknown / cloverleaf
    CombatEvidence                                // promoted to Hostile by ThreatSource match
    CloseRangeID                                  // close LOS → actual ground-truth
    PlayerClassified                              // manual override
)
```

### P-G4. ContactSystem поглощает VisionSystem

Phase 7 `VisionSystem` снимается с registration в `main.go`. Новый `ContactSystem` делает:

1. Sensor-math (effRange × facing × concealment + LOS) — заменяет vision cone test.
2. Awareness writeback — для каждого detected target своему юниту пишет в `Awareness.LastSeen` (для SurvivalInstinct / future TacticalAI).
3. Contact upsert — для не-PlayerFaction targets создаёт/обновляет Contact entity.

Один проход — два write paths. Сэкономлено: повторный distance/LOS-compute не делается.

`Vision{RangeM, AngleDot}` компонента остаётся в коде временно (для backward read у любого dormant consumer'а), но в `UnitFactory.Spawn` больше не добавляется. После Phase 18.5 closure — full remove если не нашлось read-sites.

### P-G5. Сенсорная модель — детерминированный effective-range через configurable falloff function

```go
type FalloffKind uint8
const (
    FalloffStep        FalloffKind = iota  // 1 if d≤r else 0  (legacy hard cutoff)
    FalloffLinear                           // max(0, 1 - d/r)
    FalloffInvSquare                        // r² / (r² + d²)         peaks at 1, =0.5 at d=r
    FalloffExp                              // exp(-d/r)              =1 at 0, ≈0.37 at d=r
    FalloffSigmoid                          // 1 / (1 + exp((d-r)/k)) smooth around r
)

func Falloff(kind FalloffKind, d, r float32) float32 { /* dispatch */ }
```

Detection threshold = **0.5** глобально. BaseRangeM в SensorChannel — "nominal range" (значение где falloff пересекает threshold для типичных curves Linear / InvSquare / Sigmoid).

**Почему не вероятностный roll:** см. длинное обсуждение — детерминированный режим даёт стабильные контакты, легче дебажится, легче тюнится. Configurable falloff даёт нужную нелинейность без noise. Per-sensor можно ставить разные curves: пехота-оптика InvSquare (резко падает на дистанции), радар Step (hard threshold по SNR), thermal Exp (тепло диссипирует быстро).

MVP: вся пехота / техника / wildlife — Optical with FalloffLinear. Phase 18.6+ настроит curves под фракции.

### P-G6. Concealment — Stance + CoverMap, через target side

```go
func concealmentMul(target Entity) float32 {
    mul := stanceMul(stance.Code)  // Stand 1.0 / Crouch 0.75 / Prone 0.5
    cover := coverMap.At(target.WorldPos)  // 0..1, Phase 6 baked
    mul *= lerp(1.0, 0.6, cover)
    return mul
}
```

Detection formula:
```go
strength := Falloff(channel.FalloffKind, dist, channel.BaseRangeM)
strength *= facingMul(angle, channel.Facing)
strength *= concealmentMul(target)
detected := strength >= 0.5 &&
            hasLOS(observer, target) &&
            (channel.DetectMask & target.Dimension) != 0
```

**Vegetation / building interior учитываются эмерджентно:**
- Vegetation props (Bush/Oak/Pine) уже наполняют CoverMap в Phase 6.
- Building interior эмерджентно через LOS-raycast — стена блокирует, окно прозрачное, дверь закрытая блокирует. Отдельный multiplier не нужен.

**Movement multiplier — отложен** ("двигающийся проще заметить"). Если в плейтесте появится feel "слишком статично" — добавится в 18.6.

### P-G7. Symbology persisted в отдельный файл

`./save/settings.json` — display mode (Cubes / NATO / Both), 3D-billboards flag.
`./save/symbols.json` — preset library (named SymbolSpecs).

Два отдельных файла потому что:
- settings зависят от пользователя, не от мира. Должны переноситься при смене world dir.
- symbols тоже user-scoped, но больше по объёму и редактируются через builder.
- Изолируем concerns — порча одного не ломает второй.

Оба под `./save/` — уже gitignored.

### P-G8. Top-bar toolbar — disabled, не hidden, по контексту

Action buttons на top bar'е не прячутся когда контекст отсутствует — они grey'ятся + tooltip "select a unit first". Причина: prevent jumpy UI, сохранить muscle memory, дать "почему недоступно" feedback.

State icon (открыт ли editor где-то): подсветка underline'ом если ассоциированный widget present в каком-то leaf'е или floating.

---

## Track 18.5.0 — Faction component + Wildlife dummies

### Решения

**0-P1. Wildlife — отдельная FactionSide, не подкатегория Neutral.**

Wildlife (звери, гражданские) — не должны давать CombatEvidence triggers (они не shoot). Neutral может (вооружённое гражданское ополчение, allied AI который случайно стрельнул). Различие важно для auto-promote логики — Wildlife всегда стоит на perception=Neutral независимо от того что какой-то ThreatSource случайно прошёл рядом.

**0-P2. Walking-circle AI для bandit-test dummies — встроенный, не AISystem.**

Простой контроллер на ECS компоненте `CirclePatrol{Center, RadiusM, Speed, Phase}`. Один отдельный system `circle_patrol` 30 LOC. Не путать с реальным Tactical AI (Phase 17.8). Этот код останется как test-utility — когда придут настоящие enemy AI, dummies заменятся / переедут в test scenes.

### Milestones

**M0.1.** Создать `components/faction.go` с `FactionSide` enum + `Faction{Side}` struct. Add `Faction` к UnitFactory's Map handles.

**M0.2.** `entities/unit_factory.go`: расширить `Spawn(pos)` → `Spawn(pos WorldPos, side FactionSide)`. Все callers в `main.go` дополняются `PlayerFaction` (grep+replace).

**M0.3.** Создать `systems/circle_patrol.go` — компонента + system. Read CirclePatrol → wrap angle by elapsed*speed/radius → target pos = center + (cos, sin)*radius → push `MoveTo` action in ActionQueue если ещё не на target. ActiveEvery=500ms.

**M0.4.** В `main.go` spawn debug-scene:
- 3-4 статичных EnemyFaction юнитов на (200, 200), (210, 195), (220, 205).
- 2 walking-circle EnemyFaction юнитов: center (180, 180), radius 15; center (250, 220), radius 20.
- 1 WildlifeFaction unit (placeholder "deer" — тот же кубик пока, цвет разве что коричневый): walking-circle radius 30 around (200, 250).

**M0.5.** `weapon_fire_gate.go` (Phase 14): добавить faction check — `if observer.Faction.Side == target.Faction.Side: skip`. Защита от friendly fire на старте.

### Closure

Запустить `go run .` — 7 enemy/wildlife units видны в 3D как цветные кубики (red для EnemyFaction, brown для WildlifeFaction). Walking-circle units кружат вокруг своих центров. Свои солдаты не стреляют по ним (потому что Weapon system + RoE требует engagement через order). Без 18.5.A они видны "сквозь FoW" — это правится в следующем track'е.

---

## Track 18.5.A — Sensors + ContactSystem

### Решения

**A-P1. SensorChannel — фиксированный массив [4] в Sensors компоненте.**

```go
type Sensors struct {
    Channels [4]SensorChannel
    Count    uint8
}
type SensorChannel struct {
    Kind        SensorKind   // Optical / Thermal / Radar / Acoustic
    BaseRangeM  float32
    FalloffKind FalloffKind
    Facing      FacingProfile
    DetectMask  DimensionMask
}
type FacingProfile struct {
    ForwardMul, SideMul, RearMul   float32
    ForwardConeRad, SideConeRad    float32
}
type DimensionMask uint8  // bits: 1=Infantry, 2=Vehicle, 4=Air, 8=Naval
```

Зачем фикс [4]: без аллокаций, DOD-friendly, типичный сенсор-пакет (Optical + Thermal + Acoustic + Radar) сюда влазит. Count даёт активную длину.

**A-P2. Per-class defaults жёстко зашиты в UnitProfileSpec.**

`gen/units/profiles.go` (новый файл — или вкорячиваем в существующий `entities/`) держит сенсорные дефолты per UnitRole. Infantry / Vehicle / Radar — три разных профиля facing+range. Spawning unit берёт профиль по role + дополняет Sensors.

Альтернатива — хардкод в UnitFactory.Spawn. Отвергнуто — Spec table pattern удобнее для будущего тюнинга и unit role expansion.

**A-P3. Facing — three-zone piecewise constant + linear blend.**

```go
func facingMul(angle float32, p FacingProfile) float32 {
    switch {
    case angle <= p.ForwardConeRad:
        return p.ForwardMul
    case angle <= p.SideConeRad:
        t := (angle - p.ForwardConeRad) / (p.SideConeRad - p.ForwardConeRad)
        return lerp(p.ForwardMul, p.SideMul, t)
    case angle <= math.Pi:
        t := (angle - p.SideConeRad) / (math.Pi - p.SideConeRad)
        return lerp(p.SideMul, p.RearMul, t)
    }
    return p.RearMul
}
```

Не один cone-cutoff а 3 зоны с linear blend между ними. Smoothness без скачков. Per-class конкретика — см. таблицу в обсуждении.

**A-P4. ContactSystem — single-pass detect + Awareness + Contact upsert.**

Один system, поглощает Phase 7 VisionSystem. Алгоритм:

```
for each PlayerFaction observer O (Active tier, throttled 250ms; Relevant 1s; Dormant off):
    candidates = spatialQuery 3×3 chunks around O.WorldPos.Chunk
    for each candidate T:
        if T.Faction.Side == PlayerFaction: skip
        for each channel C in O.Sensors[:Count]:
            if (C.DetectMask & dimensionOf(T)) == 0: continue
            d = WorldPos.Distance(O.Pos, T.Pos)
            if d > C.BaseRangeM * 2: continue  // far cull
            ang = angle(O.Motion.Yaw, dirTo(T))
            strength = Falloff(C.FalloffKind, d, C.BaseRangeM)
                     * facingMul(ang, C.Facing)
                     * concealmentMul(T)
            if strength < 0.5: continue
            if !hasLOS(O.Pos, T.Pos, wallSnapshot): continue
            awarenessWrite(O, T, now)
            contactUpsert(T, T.Pos, now)
            goto nextTarget  // one channel match enough
postpass: applyCombatEvidence(threatSources, contacts)
postpass: applyCloseRangeID(playerUnits, contacts, IDRangeM=25)
postpass: age all contacts (compute alpha/ghost, never delete)
```

Pre-pass serial: collect wall snapshot per chunk (как `weapon.go` делает). Может быть parallelized в Phase 18.6 — MVP serial.

**A-P5. CloseRangeID радиус — 25 м, не дифференцируется по dimension на MVP.**

Один тюнабль `IDRangeM float32 = 25`. Дифференциация (пехоту опознавать ближе, технику дальше) — отложена. Если плейтест чувствует "технику опознают подозрительно близко" — добавляется per-dimension в 18.6.

**A-P6. CombatEvidence не считается refresh для aging.**

Только direct sensor refresh обновляет `LastSeenTime`. ThreatSource match только меняет PerceivedAffil → Hostile, но не продлевает жизнь контакта (если они стреляют издалека и нас не видят — контакт честно стареет, мы знаем что они где-то были, но позицию не подтверждаем).

Альтернатива: `CombatEvidence` refresh'ит LastSeenTime для интуитивности ("пока стреляют — мы их 'видим' хоть и не визуально"). Отвергнуто — путаница alpha-fade vs combat-state. Чище: alpha falls if no sensor refresh, отдельно есть Inspector-фид "currently engaging" если ThreatSource activный.

### Milestones

**M-A.1.** `components/sensors.go` — Sensors + SensorChannel + FacingProfile + DimensionMask + FalloffKind + SensorKind. Helper functions `Falloff(kind, d, r) float32` + `facingMul(ang, profile) float32`.

**M-A.2.** `components/contact.go` — Contact + ContactPlayerSet marker + ClassificationSource + Affiliation + Dimension. Helper `affilFromFaction(side FactionSide) Affiliation`.

**M-A.3.** `gen/units/profiles.go` — profile table per UnitRole (Infantry / Vehicle / Radar — Radar пока без implementation, но slot ready). Infantry: Optical 200m, Linear, Forward 60°/100%, Side 120°/80%, Rear 50%. Vehicle: Optical 250m, Linear, Forward 30°/100%, Side 90°/60%, Rear 20%.

**M-A.4.** `entities/unit_factory.go` — extend Spawn to fill Sensors from profile. Remove `Vision` add from factory (компонента остаётся в коде, но не addится новому unit).

**M-A.5.** `systems/contact.go` orchestrator + `contact_detect.go` (sensor pass) + `contact_promote.go` (CombatEvidence + CloseRangeID post-passes) + `contact_aging.go` (alpha + ghost flag).

**M-A.6.** `concealmentMul(target)` helper в `contact_detect.go` — читает Stance, CoverMap. Inline (один call site).

**M-A.7.** Register ContactSystem в `main.go`. Снять VisionSystem с registration (закомментировать на одну фазу, удалить после closure).

**M-A.8.** Resource `ContactRegistry` — `map[ecs.Entity]ecs.Entity` (tracked → contact entity) для O(1) upsert lookup. ecs.NewResource + ecs.AddResource в main.go.

**M-A.9.** Debug overlay: hotkey `K` (hold) — draw effective range circles per friendly unit + facing arc. Помогает тюнить и видеть FoW в дев-режиме.

### Acceptance

Запустить game. Свои юниты двигаются — статичные enemy dummies становятся visible (3D-render появляется) на dist ~200m, исчезают за 200m. Стоит вплотную к bush prop'у → range падает. Crouch / prone → range падает. Юнит lookдит назад — range сильно меньше. Walking-circle enemy: дольше остаётся в Awareness FIFO своих units если они движутся за ним. Contact entities создаются — можно проверить через debug overlay K (количество).

---

## Track 18.5.B — Map / Inspector render + camera focus

### Решения

**B-P1. Контакт селектабельный, single-click на Map.**

Селект кладёт contact entity в `SelectionState` (тот же что для units). Inspector переключается на Contact panel (отличается от Unit panel — нет ammo, нет stance, есть classification source / last seen / estimated pos).

**B-P2. Double-click = lerp camera target.**

`OrbitController.Target` lerped к contact.EstimatedPos за 0.5s. Уже есть smoothing в Phase 13 — нужно только set target. Реализуется в Map panel input handler.

**B-P3. Placeholder symbols в этом track'е — цветной квадрат + 1-buchstaben label.**

Реальная APP-6 графика — Track 18.5.C. В B рендерим:
- Friend (own units) — синий квадратик с "F"
- Hostile — красный ромб с "H"
- Unknown — жёлтый клевер (упрощённо — желтый квадрат с "?") с "?"
- Neutral — зелёный квадрат с "N"

Это позволяет валидировать FoW + selection + camera focus + Inspector без зависимости от полного symbol-pipeline.

### Milestones

**M-B.1.** `ui/panel_map.go` (или wherever map renders сейчас) — extend rendering loop:
- iterate Filter[Unit, Faction] where Faction.Side == PlayerFaction → placeholder symbol
- iterate Filter[Contact] → placeholder symbol with alpha = ageAlpha(contact.LastSeenTime, now)

**M-B.2.** Map input handler — single click on contact entity → SelectionState.SetSingle(contact); double click → set OrbitController.Target = contact.EstimatedPos.AsRenderSpace.

**M-B.3.** Inspector — новая section `inspector_contact.go`:
- Header: placeholder symbol + classification ("Hostile" / "Unknown" / etc) + source ("by combat" / "manual")
- Body: last seen Xs ago / estimated chunk (X, Z) / [Focus camera] button / [Open builder] button (placeholder, no-op til 18.5.D) / [Delete contact] button.
- describeSelection extend — handle Contact entity.

**M-B.4.** Age alpha function в `contact_aging.go`:
```go
func ageAlpha(lastSeen, now float64) float32 {
    age := now - lastSeen
    if age < 5 { return 1.0 }
    if age > 30 { return 0.3 } // floor — don't fully disappear
    return 1.0 - 0.7*float32((age-5)/25)
}
```

### Acceptance

Контакты появляются на Map с placeholder-graphics. Single click selecting works — Inspector flips to Contact panel. Double click animates camera over 0.5s к EstimatedPos. Stale contact (last seen 30s ago) рендерится 30% alpha-faded. Delete button removes contact entity.

---

## Track 18.5.C — SymbolSpec + bake-to-texture renderer

### Решения

**C-P1. SymbolSpec — flat struct, не frame-of-references.**

```go
type Affiliation uint8   // Friend / Hostile / Neutral / Unknown
type Dimension   uint8   // Infantry / Vehicle / Air / Naval / Unknown
type Echelon     uint8   // None / Team / Squad / Section / Platoon / Company / Battalion / Brigade

type SymbolSpec struct {
    Affiliation Affiliation
    Dimension   Dimension
    Icon        IconKind   // InfantryCross / ArmorOval / ArtilleryDot / ReconArrow / HQFlag / ...
    Echelon     Echelon
    Modifiers   [4]ModifierKind  // up-to-4 amendments (HQ-tag, taskforce, reinforced...) — MVP zero
}
```

24-byte struct, copy-by-value. Idiomatic ECS — никаких ссылок.

**C-P2. Bake-to-texture в `rl.RenderTexture2D` lazy-cached.**

```go
type SymbolRenderer struct {
    cache map[SymbolSpec]rl.RenderTexture2D
}
func (sr *SymbolRenderer) Get(spec SymbolSpec) rl.Texture2D
func (sr *SymbolRenderer) Invalidate(spec SymbolSpec)
```

Сборка одна раз (~256 пикс / спрайт) — frame + fill + icon + echelon dots + modifiers. Затем `rl.DrawTexturePro` на Map (масштабирование от 16px-far до 32px-zoom).

Spec — map key, не PresetID. Один Spec → одна текстура. Дублирующие presets (same Spec, разные имена) шарят bake.

**C-P3. Preset library — named SymbolSpecs, persisted.**

```go
type SymbologyPreset struct {
    Name string
    Spec SymbolSpec
}
type SymbologyPresets struct {
    All []SymbologyPreset  // user-saved + built-in defaults
}
```

Resource. Хранится в `./save/symbols.json`. Built-in defaults (12 quick-pick presets — 3 affil × 4 dim) шипятся в коде и добавляются на load если их нет в file.

**C-P4. Frame geometry — direct DrawRectangleLines / DrawPolyLines, без sprite-atlas.**

Каждый frame — параметрическая отрисовка в RenderTexture target:
- Friend → DrawRectangleLines + DrawRectangle (fill) — синяя
- Hostile → DrawPolyLines 4-point diamond — красная
- Neutral → DrawRectangleLines квадрат (rotated 0°) — зелёная
- Unknown → DrawRectangleRoundedLines / DrawPolyLines cloverleaf — жёлтая

Icon отдельно — DrawText 1-2 буквы или DrawLine для крестика (infantry) / DrawCircle (artillery). Без PNG-атласа на MVP — параметрический рендер быстрее в имплементации и легче тюнится. PNG-pipeline — отложен, дойдём при visual fidelity phase.

### Milestones

**M-C.1.** `components/symbol_spec.go` — SymbolSpec + nested enum types + helper `defaultSpec(side, dim) SymbolSpec`.

**M-C.2.** `ui/symbol_renderer.go` — SymbolRenderer struct + cache + bakeSpec(spec) → RenderTexture2D (single function, ~200 LOC, drawing primitives only).

**M-C.3.** `resources/symbology_presets.go` (or wherever resources live) — SymbologyPresets resource + persistence load/save (`save/symbols.json`).

**M-C.4.** Replace placeholder rendering в Map panel + Inspector header'е — use SymbolRenderer.Get(spec).

**M-C.5.** Map render: `defaultSpec(unit.Faction.Side, unit.Dimension)` for own units (no override yet). `contact.PerceivedSpec()` for contacts (helper that builds Spec from Contact fields).

**M-C.6.** Spec resolver — `func resolveSpec(entity ecs.Entity) SymbolSpec`:
- Own unit with `UnitSymbolOverride{PresetID}` → preset's Spec
- Own unit без override → defaultSpec(side, dim)
- Contact with `ContactSymbolOverride{PresetID}` → preset's Spec
- Contact без override → assembled from PerceivedAffil + PerceivedDim + (Echelon=Unknown)

### Acceptance

Placeholder squares → реальные NATO-like символы на Map + Inspector. Кеш проверяется — открыть Map с 50+ символами, top показывает frame_ms не выросло (один bake per unique Spec, кеш hot после первого тика). Sample defaults (12 builtin quick-pick presets) грузятся в SymbologyPresets resource при старте.

---

## Track 18.5.D — Symbol Editor builder panel

### Решения

**D-P1. Builder — separate PanelKind, embeddable in workspace leaf и floating.**

Паттерн Formation Editor (Phase 18). Workspace-aware singleton — открыт как leaf OR floating OR none, при switch share state.

**D-P2. Live preview — SymbolRenderer + invalidate on every spec change.**

Preview pane показывает current Spec build. Каждый change в любой dropdown / radio invalidate'ит кеш для текущего in-progress Spec'а и rebake'ит. Cheap (1 bake = ~256 пикс).

**D-P3. Builder rows — flat enum-pickers, не tabs/tree.**

```
[Affiliation]   ○ Friend  ○ Hostile  ● Neutral  ○ Unknown
[Dimension]     ○ Infantry  ● Vehicle  ○ Air  ○ Naval
[Icon]          [dropdown ▾ Artillery]
[Echelon]       ● None ○ Team ○ Squad ○ Section ○ Platoon ○ Company ○ Battalion
[Modifiers]     (deferred — disabled на MVP)
─────────────────────────────────────
[ Preview: ⬢ ]
[Name: artillery_btn  ] [Save preset]
[Apply to selection]   [Cancel]
```

Простой vertical scroll. UX не "wizard" — все поля видны сразу.

**D-P4. "Apply to selection" — what does it apply to?**

Если selection = own unit → set UnitSymbolOverride{PresetID}.
Если selection = contact → set ContactSymbolOverride{PresetID} + ContactPlayerSet marker.
Если ничего не выбрано → button disabled.

### Milestones

**M-D.1.** `ui/panel_symbol_editor.go` — new PanelKind. PanelManager integration (placement in leaf via chevron menu + Float pane). Same singleton pattern as Formation editor.

**M-D.2.** `editor_state.go` — `SymbolEditorState{InProgressSpec SymbolSpec, Name string, Mode (Create / Edit existing)}` resource. Editor reads + writes.

**M-D.3.** Rows rendering — 4-5 enum-picker rows + preview + name field + buttons.

**M-D.4.** [Save preset] — append SymbologyPreset{Name, Spec} к SymbologyPresets.All. Persist `save/symbols.json`.

**M-D.5.** [Apply to selection] — write override component to selected entity. Re-bake symbol with new Spec for immediate visual feedback.

### Acceptance

Builder открывается из workspace leaf (через chevron menu) и floating (через 18.5.E toolbar button — будет в next track). Player может собрать символ из 5 рядов, видеть live preview, save с именем, применить к выбранному юниту / контакту. Symbol на Map / Inspector обновляется.

---

## Track 18.5.E — Top-bar toolbar + Settings panel

### Решения

**E-P1. Top-bar layout — actions left, status right.**

```
┌──────────────────────────────────────────────────────────────────┐
│ [SE] [FE] [⚙]                              T+02:34   ▶ 1x         │
└──────────────────────────────────────────────────────────────────┘
   ↑                                              ↑
   action toolbar                              clock + speed
```

Phase 18 уже определил TopBar как separate Panel без chrome. Расширяем `ui/topbar.go` — добавляем left toolbar group.

**E-P2. Action buttons — 24×24 px icons + tooltip.**

Иконки — параметрически нарисованные glyphs (тот же стиль что symbol renderer — primitive shapes). На MVP — 3 кнопки:
- **SE** — Symbol Editor (открывает / закрывает / focuses panel).
- **FE** — Formation Editor (то же).
- **⚙** — Settings.

Disabled = ~40% opacity, tooltip = "select a unit first" / etc.

**E-P3. Settings — minimal resource + simple panel.**

```go
type Settings struct {
    DisplayMode   DisplayMode   // CubesOnly / NATOOnly / Both
    Show3DSymbols bool          // false MVP
    UIScale       float32       // 1.0 default, не используется в 18.5
}
```

Settings panel — третья PanelKind. Опять может быть leaf / floating. Внутри — несколько rows: display mode radio + checkboxes.

Persistence: `./save/settings.json` — JSON encode of struct. Load на app start, save on change (debounced 500ms — не на каждый клик).

### Milestones

**M-E.1.** Extend `ui/topbar.go` — render toolbar group left, time display right. Clock + speed moves to right group (already there in Phase 18 — re-position).

**M-E.2.** Toolbar button generic — icon + tooltip + disabled state + click handler + active-state underline.

**M-E.3.** Symbol Editor button — handler opens/closes/focuses PanelSymbolEditor leaf или floating. Disabled if SelectionState empty или selection is не-unit-not-contact.

**M-E.4.** Formation Editor button — same as `E` hotkey (which stays as alternative). Disabled if no squad selected. Underline active when panel is up.

**M-E.5.** Settings button — opens PanelSettings.

**M-E.6.** `resources/settings.go` — Settings resource + load `save/settings.json` on startup + save (debounced).

**M-E.7.** Settings panel `ui/panel_settings.go` — radio + checkbox rows + version label.

**M-E.8.** DisplayMode honoured в Map render: CubesOnly = original placeholder cubes (no symbols), NATOOnly = symbols only, Both = symbols overlaid on cubes.

### Acceptance

Top bar: 3 icons left, clock+speed right. SE button grey'ится без selection. FE button — same. Click SE → opens Symbol Editor. Settings panel toggles display mode — Map render visibly switches.

---

## Track 18.5.F — RMB context menu on contact

### Решения

**F-P1. Context menu — same UI primitive as Phase 17.6 building popup.**

`ui/context_menu.go` уже есть (rectangular menu с sections). Re-use the building-popup widget — pass contact entity instead. Save development time.

**F-P2. Menu structure — flat 12 quick-pick + advanced + reset + delete.**

```
RMB on contact symbol →
┌─────────────────────────────────────┐
│ Hostile                             │
│   ▸ Infantry  Vehicle  Air  Naval  │ ← 4 buttons in one row
│ Neutral                             │
│   ▸ Infantry  Vehicle  Air  Naval  │
│ Unknown                             │
│   ▸ Infantry  Vehicle  Air  Naval  │
│ ─────────────────────────────────── │
│ Change icon (advanced)…            │
│ Reset to auto                       │
│ Delete contact                      │
└─────────────────────────────────────┘
```

Click on quick-pick (3 affil × 4 dim = 12 actions) → set ContactSymbolOverride to matching builtin preset + ContactPlayerSet marker. Visible update immediately (re-resolve spec + re-render).

"Change icon (advanced)…" → opens Symbol Editor floating with this contact's current Spec as starting point + name "contact_N_custom" suggested.

"Reset to auto" → remove ContactSymbolOverride + ContactPlayerSet. ClassificationSource recalculated by ContactSystem next tick.

"Delete contact" → ecs.RemoveEntity(contact). Permanent (no undo). Confirmation dialog? — NO, players who don't want to lose contacts have shift+click or another modifier (future).

**F-P3. Friendly not in quick-pick.**

Свои юниты не появляются как Contact entity. Если когда-то добавится "Friendly Foreign" affiliation (союзная AI faction) — добавится 4-я колонка. MVP — 3.

### Milestones

**M-F.1.** `ui/context_menu_contact.go` — new method `OpenContactMenu(c ecs.Entity)` reusing existing primitives. Closure: 12 quick-pick handlers + 3 utility actions.

**M-F.2.** Map input handler — RMB on contact symbol → OpenContactMenu.

**M-F.3.** Builtin presets (12 quick-pick combos) — добавляются в SymbologyPresets resource на startup если их там нет ("hostile_infantry", "hostile_vehicle", ...). Named consistently для discoverability в builder panel.

**M-F.4.** [Change icon advanced…] → open Symbol Editor floating + pre-fill InProgressSpec from contact's current spec + Mode=EditExisting.

**M-F.5.** [Reset to auto] — remove ContactSymbolOverride + ContactPlayerSet. ContactSystem promote pass next tick picks up.

**M-F.6.** [Delete contact] — RemoveEntity. Map render skips deleted. Inspector clears if deleted contact was selected.

### Acceptance

RMB on contact symbol → menu appears. Click "Hostile → Vehicle" → contact instantly turns red diamond with vehicle icon. Click "Reset" → reverts to auto-source (Unknown or CombatEvidence depending on threat). Click "Delete" → contact vanishes. "Change icon advanced…" opens Symbol Editor with current state.

---

## Track 18.5.G — 3D enemy render with fade tail

### Решения

**G-P1. 3D render shows ground-truth only when active sensor refresh.**

В 3D scene render loop: для каждого не-PlayerFaction юнита, проверить — есть ли activate-refresh в текущем tick'е (ContactSystem.refreshedThisTick set) OR в окне tail (now - LastSeenTime < 2.0s)?

Если да — рендерим. Если нет — пропускаем.

2-секундный tail необходим чтобы при потере LOS юнит не пропал моментально — есть время понять направление движения.

**G-P2. Tail rendered с alpha decay.**

`alpha = clamp(1 - (now - lastSeen) / 2.0, 0, 1)` — линейный fade за 2 секунды до 0.

После 2s — full hide. Контакт на Map остаётся (там своя aging).

**G-P3. ContactSystem maintains `refreshedThisTick map[ecs.Entity]struct{}`.**

Resource или field на ContactSystem (resource предпочтительнее — render loop читает). Cleared at start of every ContactSystem tick. Populated when sensor pass writes detection.

3D render loop reads:
```go
if _, refreshed := refreshedThisTick[entity]; refreshed {
    drawFull(entity, alpha=1)
} else {
    contact := contactByTracked[entity]
    if contact != 0 {
        age := now - contact.LastSeenTime
        if age < 2.0 {
            drawFull(entity, alpha = 1 - age/2.0)
        }
    }
}
```

### Milestones

**M-G.1.** `resources/contact_refresh.go` — `ContactRefreshThisTick struct { Set map[ecs.Entity]struct{} }` resource. ecs.NewResource + ecs.AddResource в main.

**M-G.2.** `contact_detect.go` — clear set at top of Update; insert on every successful detection.

**M-G.3.** `render_units.go` (or wherever 3D unit render is) — extend per-frame loop с visibility filter. Friend → always full. Not-friend → check refresh / contact age. Compute alpha.

**M-G.4.** Smoke test: walk own units past enemy dummy, observe — dummy appears in 3D, walk past so LOS broken, dummy fades over 2s in 3D. Map symbol stays (aging slower).

### Acceptance

Запустить — enemy dummies не видны в 3D до того как наш юнит подойдёт. После — visible. После потери LOS — fade 2 сек, потом полностью исчезает из 3D. Map keeps symbol (с собственной aging). Track G — последний — финальная "FoW works visually" demo.

---

## Closure criteria

Phase 18.5 закрыта когда:

1. **Faction model работает.** Все unit spawns через `UnitFactory.Spawn(pos, side)`. PlayerFaction + EnemyFaction + WildlifeFaction юниты в стартовой сцене (≥7 enemies, ≥1 wildlife).
2. **Sensors модель работает.** Optical channel populated по UnitRole. Walking-circle dummies детектятся по distance + facing + concealment с правильным falloff'ом. Crouch/prone visibly уменьшает effRange.
3. **Contact entities создаются.** Debug overlay K показывает >0 contacts когда PlayerFaction units видят non-PlayerFaction targets. Не удаляются автоматически.
4. **Auto-promote работает.** Враг стреляет → его контакт получает Source=CombatEvidence, PerceivedAffil=Hostile, visible на Map как red diamond. Свои подходят на <25m с LOS → Source=CloseRangeID, PerceivedDim становится actual.
5. **Map render shows real symbols** (post 18.5.C). Свои синие, hostile красные, unknown жёлтые. Aging fade видим (>30s stale = 30% alpha).
6. **Inspector contact panel.** Selected contact показывает symbol, classification, last seen, source. Buttons [Focus camera] / [Open builder] / [Delete] работают.
7. **Double-click camera focus.** Map double-click smoothly lerps OrbitController.Target за ~0.5s.
8. **Symbol Editor builder.** 5-row builder открывается через top-bar SE button + chevron menu. Live preview обновляется. Save preset → `save/symbols.json`. Apply to selection меняет visible symbol.
9. **Top-bar toolbar.** 3 buttons + tooltip + disabled state работают. Clock + speed справа.
10. **Settings panel + persistence.** DisplayMode радио меняет render. settings.json создаётся и читается между launches.
11. **RMB context menu на контакте.** 12 quick-pick + advanced + reset + delete все работают.
12. **3D enemy render с 2s tail fade.** Enemy появляется в 3D на sensor refresh, fade'ится за 2s после loss.
13. **VisionSystem deregistered.** ContactSystem полностью заменил его. `Vision` component удалена из UnitFactory's added set.
14. **No regressions.** Phase 17.9 10-сценный test suite по-прежнему ≥9/10 PASS. Profiler — overall tick ms не вырос больше чем на 0.5ms median (ContactSystem заменяет VisionSystem, должно быть neutral-to-cheaper).
15. **Memory updated.** `project_phase_18_5_closed.md`, `feedback_sensor_model.md` (effective-range + falloff паттерн), `feedback_contact_classification.md` (precedence cascade), `reference_symbology_files.md` (where save/symbols.json + save/settings.json live).

Если 1-14 проходят, Phase 18.5 closed. Дальнейшие фазы (vehicles 19, aviation 20, strategic AI 22) опираются на эту FoW-инфраструктуру: vehicles получают Thermal sensor channels, radar units — Radar channels с DimensionMask = Vehicle|Air|Naval, strategic AI читает Contact registry как input.
