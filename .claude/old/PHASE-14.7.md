# Phase 14.7 - рабочий план

Сквозная фаза полировки комментариев. Текущий codebase накопил тяжёлые phase-plan-style комментарии (multi-paragraph "почему мы это решили", "что в Phase X добавлено", "Phase Y расширит так-то"). Они были полезны как живая документация во время быстрого роста (Phase 0-14), но теперь:

- Перенасыщают код, замедляют чтение
- Дублируют информацию из PHASE-*.md плана (мёртвая копия истории)
- Используют тяжёлые Unicode-символы (em-dash, arrow, fancy bullets), которые ломаются в diff'ах и grep'е
- Перерастают живую правду - комментарий говорит "Phase 14.5 заменит X", X уже заменён, комментарий устарел

Phase 14.7 проходит по всему коду и приводит комментарии к новому стандарту: terse, ASCII-only, WHY-not-WHAT, без phase history (которая живёт в git log + PHASE-N.md).

Никаких функциональных изменений. Только tekst в .go файлах.

---

## Решения, которые лочим до начала кода

**P1. Comment style.**

Целевой стиль:

```go
// Spec table for OrderKindCode. Readers index by code; compile-time guard
// catches missed rows. See PHASE-14.5.md P1 for rationale.
type OrderKindSpec struct { ... }
```

Не:

```go
// Phase 14.5 P1 — Spec table pattern. Each `OrderKindCode` gets a typed
// `OrderKindSpec` row in `OrderKindSpecs`, replacing scattered switch dispatch
// across order_resolver / weapon / pie_menu / inspector / map_render /
// command. Readers index the array by enum-code, no fallback path needed —
// the compile-time assert below catches any new enum value that forgot to
// extend the table.
//
// Pattern rules:
//   - One spec table per enum. Don't dual-purpose ("OrderKindSpecs also holds
//     audio data" etc.) — split into a sibling table when fields multiply.
//   - Readers DO NOT switch on the enum; they read fields. ...
```

**Rules:**

- Одна-две строки максимум на блок-комментарий типа / функции / поля.
- Никаких phase-history ("Phase 14.5 added X", "Phase 15 will replace Y"). Удалить.
- WHAT-комментарии (что делает) удалить - имя функции / типа уже описывает.
- WHY-комментарии (почему сделано так, скрытые инварианты, нелогичные хаки) - оставить, но коротко.
- Cross-reference на PHASE-N.md / GAMEDESIGN.md / COMMAND-MODEL.md - оставлять как маркер "see X for details", не дублировать содержимое.

**P2. Punctuation normalization.**

Заменяем:

| Old | New |
|---|---|
| em-dash | `-` (hyphen) |
| en-dash | `-` |
| right arrow | `->` |
| left arrow | `<-` |
| bidirectional arrow | `<->` |
| left/right double quote | `"` |
| left/right single quote | `'` |
| ellipsis | `...` |
| times sign | `x` |
| middle dot | `.` |
| bullet | `-` |
| check mark | `done` (в тексте) или удалить |
| cross mark | `no` или удалить |

Все unicode-decorations превращаются в ASCII. Это упрощает grep, diff, и не зависит от font rendering.

**P3. Длина строк.**

Существующее: 80-100 char wrap. Сохраняем 80 char как target для прозы, 100 как hard limit. Code не wrap'аем (gofmt сам решает).

**P4. Heredoc и markdown в комментариях.**

Если комментарий описывает структуру / алгоритм и **полезно** показать псевдокод - оставить, но без многоуровневых вложенных bullet'ов. Максимум один уровень `-`.

```go
// pickTarget walks Awareness FIFO. Skips:
// - empty slots (Time == 0)
// - self (Target == self)
// - stale (now - Time > maxAge)
// - friendly (same faction)
// - dead (!world.Alive)
// - out of range
// Returns most recent surviving candidate.
```

Это OK. Не OK:

```go
// pickTarget walks the seer's Awareness FIFO and returns the most recent
// hostile sighting that's still alive, still in range, and within the
// awareness max-age. (ent, pos, true) on hit; (_, _, false) on miss.
//
// Walk algorithm:
//   1. Iterate over LastSeen slots
//      a. Skip if Time == 0 (empty slot)
//      b. Skip if Target == self ...
```

**P5. Не трогаем PHASE-*.md и другие .md.**

Markdown файлы остаются с heavy formatting (читают люди, не grep). Только .go файлы scope'аются.

**P6. Не трогаем CLAUDE.md, GAMEDESIGN.md, COMMAND-MODEL.md, ROADMAP.md.**

Эти - живые design документы, переписываются по мере эволюции дизайна. У них своя жизнь.

**P7. Удалить устаревшие cross-references.**

Комментарии типа "Phase 14.5 will replace this with X" - проверить, replaced ли. Если да - удалить. Если нет - оставить с пометкой `TODO Phase X`.

**P8. Heredoc license-like блоки удалить.**

Длинные header'ы типа "Phase N M14.X — описание системы, обоснование, дизайн-tradeoffs" в начале файла - urezать до 1-2 строк. Если действительно нужна архитектурная документация - вынести в .md, файлу оставить ссылку.

**P9. Pacing - один файл за раз, отдельные commits.**

Не делать "big bang" commit с 30 файлами. Каждый файл / маленькая группа связанных файлов = отдельный commit. Это позволяет:

- Bisect при regression (вряд ли возможна но safety)
- Code review (если когда-либо будет другой разработчик)
- Откатить отдельную секцию если стиль не понравится

**P10. Skip уже-минимальных файлов.**

Если файл уже terse (e.g. `core/spatial_hash.go` написан в Phase 14.5 с правильным стилем) - не трогаем. Энергия на тяжёлые legacy файлы.

---

## Милстоуны

### M14.7.0 - Punctuation sweep

**Цель.** Все .go файлы проекта используют только ASCII. Никаких em-dash / arrow / quotes / special symbols в комментариях.

**Делаем:**
- Tool/script для поиска (например `rg '—|→|"|"|''|...|×|·|•' --type=go`).
- Прогон по списку, замена через `sed` или manual edit per file.
- Edge cases: utf8 в строковых литералах (НЕ ТРОГАТЬ - это game text), utf8 в filenames (комментарии могут ссылаться на path с unicode).
- Sanity check: `go build ./...` после каждой группы файлов.

**Проверяем.** `rg '—|→' --type=go` возвращает пустой результат. Build green.

---

### M14.7.1 - Phase history removal

**Цель.** Все упоминания "Phase 14.5 added X", "M14.5.3 wired Y", "Phase 15 will Z" удалены либо превращены в `// TODO` если действительно forward-looking.

**Делаем:**
- Grep `Phase \d+` в .go файлах.
- Per file: для каждого upcoming-phase reference - проверить status (done? still planned?).
- Done -> просто описать что делает код (без истории).
- Planned -> `// TODO Phase X: ...` короткой строкой.
- Done с invariant'ом который важен to know - переформулировать как "Note: ..." без phase reference.

**Проверяем.** `rg 'Phase' --type=go` возвращает максимум 5-10 строк (TODO only). Не 200+ как сейчас.

---

### M14.7.2 - Block comment trimming

**Цель.** Long multi-paragraph comments shortened to 1-2 sentences. WHAT-комменты удалены. WHY-комменты сохранены но terse.

**Делаем:**
- Per-package walk. `systems/` самая толстая - начинаем там.
- Каждый файл: top-of-file header trim, per-function comment trim, per-field/type trim.
- Сохраняем только:
  - Hidden invariants (e.g. "alive-check required first")
  - Non-obvious why ("we use reflection because cgo Get crashes")
  - Cross-file constraints ("read X before writing Y")
  - Lock-in decisions с rationale (one line)
- Удаляем:
  - Function does X (name says it)
  - Field is the Y (name says it)
  - "Returns true if Z" (signature says it)
  - Multi-paragraph histories
  - Pseudocode duplicating actual code below

**Проверяем.** Random sample 5 files: comment density dropped ~3-5x. Build green. Diff readable.

---

### M14.7.3 - Code references updated

**Цель.** Cross-references между файлами / на .md документы корректны и минимальны.

**Делаем:**
- Grep `\.go:` и `PHASE-` в комментариях.
- Каждая ссылка - проверить что target существует, что описание соответствует.
- Удалить broken / outdated.
- Заменить толстые "see PHASE-14.md M14.4 §P9 для подробностей" на короткое "see PHASE-14.md" or "see GAMEDESIGN §3".

**Проверяем.** Sample 10 cross-refs, все валидны. Total cross-ref count significantly down.

---

### M14.7.4 - Final pass + closure

**Цель.** Глобальный review: один tour через codebase, eye-balling каждый файл. Build / vet / race tests green.

**Делаем:**
- `find . -name '*.go' | xargs wc -l` до/после - метрика общего line count delta. Comment lines dropped, code unchanged.
- `go build ./...`, `go vet ./...`, `go test -race ./core/`.
- Закрытие фазы.
- PHASE-14.7.md -> `.claude/old/`.

**Проверяем.** Codebase visibly cleaner. No build/test regression.

---

## Что считаем "закрытием Phase 14.7"

- Все .go файлы ASCII-only в комментариях.
- Multi-paragraph phase-history removed.
- Function/field WHAT-comments удалены (names speak for themselves).
- WHY-comments сохранены, terse.
- Cross-references trimmed.
- Build / vet / race tests green.
- PHASE-14.7.md в old/.

---

## Заметки на полях

- **Стиль примера.** Целевой стиль это что-то вроде Go stdlib (math, sort) - terse, no fluff. Не Linux kernel verbose.

- **Не превратить в shitpost cleanup.** Сохраняем нетривиальные инсайты. Если комментарий explains "we tried X, doesn't work because Y" - оставить (но коротко). Это спасает от повторения mistake.

- **GitHub Copilot / AI assistants.** Terser комменты лучше для AI tooling - меньше токенов на context. Дополнительная мотивация.

- **Inverse risk.** Слишком aggressive cleanup может удалить important context. Mitigation - один файл за раз, commits мелкие, easy to spot regression.

- **Файлы вне scope.** Test файлы (none сейчас), generated файлы (none), vendored deps (none) - не трогаем. Только наш hand-written code.

---

## Открытые вопросы

1. **Russian vs English mix в комментариях.** Сейчас mix (английский primary, русский в нескольких местах + в PHASE-*.md). Стандартизировать на английский в .go? Или оставить mix? Phase 14.7 default: оставить language as is, только сократить и normalize punctuation. Lang refactor - separate concern.

2. **Tracking comment density metric.** Хотим ли мы numerical target ("dropped 50% comment lines")? Или just eyeball "looks clean"? Eyeball default - количество не цель, цель читаемость.

3. **gofmt / linter help.** Есть ли инструмент который сам нормализует unicode? `gofmt` не делает, `golangci-lint` нет такой rule. Manual / sed-based pass. Возможно скрипт `tools/comment_polish.go` для bulk replace.
