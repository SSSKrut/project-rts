#!/usr/bin/env bash
# Behaviour-preservation gate: run the replay suite and diff every hash log
# against a saved baseline. replay_gate.sh proves determinism (two runs agree);
# this proves the sim is BIT-IDENTICAL to a reference build — what a refactor
# needs. Baseline dir defaults to .claude/baseline/replay (untracked).
#
# Usage: scripts/replay_baseline.sh [baseline_dir]
set -u
cd "$(dirname "$0")/.."

BASE=${1:-.claude/baseline/replay}
[ -d "$BASE" ] || { echo "no baseline at $BASE — snapshot one first:"; \
  echo "  scripts/replay_gate.sh && mkdir -p $BASE && cp /tmp/rts-replay/*_1.txt $BASE/"; exit 1; }

./scripts/replay_gate.sh > /tmp/rts-replay-gate.txt 2>&1
gate=$?
grep -E "MISMATCH|VERDICT" /tmp/rts-replay-gate.txt | grep -vc PASS >/dev/null

fail=0
drift=0
for b in "$BASE"/*_1.txt; do
  s=$(basename "$b" _1.txt)
  cur=/tmp/rts-replay/"$s"_1.txt
  if [ ! -f "$cur" ]; then
    printf "%-24s MISSING (scene not in suite?)\n" "$s"; fail=1; continue
  fi
  if diff -q "$b" "$cur" >/dev/null 2>&1; then
    printf "%-24s IDENTICAL\n" "$s"
  else
    first=$(diff "$b" "$cur" | grep -m1 '^>' | awk '{print $2}')
    printf "%-24s DRIFT (first diverging tick: %s)\n" "$s" "${first:-?}"
    drift=1; fail=1
  fi
done

echo
if [ $gate -ne 0 ]; then echo "replay_gate: FAIL (see /tmp/rts-replay-gate.txt)"; fail=1
else echo "replay_gate: PASS"; fi
if [ $drift -ne 0 ]; then echo "baseline:    DRIFT — behaviour changed"; else echo "baseline:    IDENTICAL"; fi
exit $fail
