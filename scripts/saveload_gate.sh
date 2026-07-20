#!/usr/bin/env bash
# 18.9 save/load gate: per scene — run A hashes ticks 0..N continuously;
# run B saves at tick S, a FRESH process loads and continues to N.
# B's hash log must be byte-identical to A's suffix from tick S.
set -u
cd "$(dirname "$0")/.."

SCENES=${@:-"ai_los_open ai_los_creep ai_compound_main ai_main_m1 ai_vehicle_move ai_vehicle_road ai_vehicle_combat ai_march_slope ai_cover_side"}
TICKS=2000
SAVE_AT=1000
D=/tmp/rts-saveload

go build -o bin/rts . || exit 1
mkdir -p "$D"
fail=0
for s in $SCENES; do
  timeout 300 ./bin/rts -scene="$s" -replay-hash="$D/${s}_A.txt" -run-ticks=$TICKS >/dev/null 2>&1
  timeout 300 ./bin/rts -scene="$s" -save-at=$SAVE_AT -save-path="$D/$s.rtss" -run-ticks=$((SAVE_AT + 10)) >/dev/null 2>&1
  timeout 300 ./bin/rts -scene="$s" -load="$D/$s.rtss" -replay-hash="$D/${s}_B.txt" -run-ticks=$TICKS >/dev/null 2>&1
  awk -v t=$SAVE_AT '$1>=t' "$D/${s}_A.txt" > "$D/${s}_A_suffix.txt"
  if diff -q "$D/${s}_A_suffix.txt" "$D/${s}_B.txt" >/dev/null 2>&1; then
    printf "%-24s SAVELOAD OK\n" "$s"
  else
    printf "%-24s SAVELOAD MISMATCH\n" "$s"
    fail=1
  fi
done
exit $fail
