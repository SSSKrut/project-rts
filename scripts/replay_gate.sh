#!/usr/bin/env bash
# WS-B replay gate: every ai_* scene twice, hash logs must be byte-identical.
# Usage: scripts/replay_gate.sh [scene ...]   (default: full suite)
set -u
cd "$(dirname "$0")/.."

SCENES=${@:-"ai_door_south ai_door_north ai_door_east ai_door_west \
ai_compound_south ai_compound_east ai_compound_north ai_compound_west \
ai_compound_main ai_compound_plus_south ai_compound_plus_east \
ai_compound_plus_north ai_compound_plus_west ai_office_front ai_office_l2 \
ai_office_rooms ai_house2_north \
ai_far_building ai_main_m0 ai_main_m1 ai_main_e ai_main_n \
ai_los_open ai_los_defilade ai_los_creep ai_vehicle_move ai_vehicle_road \
ai_vehicle_combat ai_vehicle_reflex ai_march_line ai_march_slope ai_cover_side"}

go build -o bin/rts . || exit 1
mkdir -p /tmp/rts-replay
fail=0
for s in $SCENES; do
  v1=$(timeout 300 ./bin/rts -scene="$s" -replay-hash=/tmp/rts-replay/"$s"_1.txt 2>/dev/null | grep -o "VERDICT.*")
  v2=$(timeout 300 ./bin/rts -scene="$s" -replay-hash=/tmp/rts-replay/"$s"_2.txt 2>/dev/null | grep -o "VERDICT.*")
  if diff -q /tmp/rts-replay/"$s"_1.txt /tmp/rts-replay/"$s"_2.txt >/dev/null 2>&1; then
    hash_ok="HASH OK"
  else
    hash_ok="HASH MISMATCH"
    fail=1
  fi
  case "$v1" in *PASS*) ;; *) fail=1 ;; esac
  printf "%-24s %-14s run1: %s\n" "$s" "$hash_ok" "$v1"
  [ "$v1" != "$v2" ] && { printf "%-24s VERDICT DIVERGED run2: %s\n" "$s" "$v2"; fail=1; }
done
exit $fail
