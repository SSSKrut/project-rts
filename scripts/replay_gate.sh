#!/usr/bin/env bash
# WS-B replay gate: every ai_* scene twice, hash logs must be byte-identical.
# Usage: scripts/replay_gate.sh [scene ...]   (default: full suite)
set -u
cd "$(dirname "$0")/.."

SCENES=${@:-"ai_door_south ai_door_north ai_door_east ai_door_west \
ai_compound_south ai_compound_east ai_compound_north ai_compound_west \
ai_compound_main ai_compound_plus_south ai_compound_plus_east \
ai_compound_plus_north ai_compound_plus_west ai_office_front ai_office_l2 \
ai_office_rooms ai_house2_north ai_garrison_windows \
ai_house_l1 ai_courtyard_l1 ai_garrison_house ai_hidden_house \
ai_far_building ai_main_m0 ai_main_m1 ai_main_e ai_main_n \
ai_los_open ai_los_defilade ai_los_creep ai_vehicle_move ai_vehicle_road \
ai_vehicle_combat ai_vehicle_reflex ai_vehicle_avoid ai_vehicle_building \
ai_vehicle_yield ai_vehicle_group ai_vehicle_convoy ai_vehicle_convoy_road \
ai_vehicle_flee ai_march_line ai_march_slope \
ai_march_column ai_wall_glide ai_crowd_cross ai_vehicle_forest ai_vehicle_slope ai_vehicle_stuck ai_cover_side ai_shellfire ai_cover_trench ai_cover_hull ai_cover_defilade ai_cover_none ai_bounding ai_clear_building ai_focus_fire ai_mass \
ai_air_transit ai_air_recon ai_solo_orders ai_air_aa_gun ai_air_manpads ai_air_cas lite_comms_range lite_comms_orders lite_comms_autonomy \
lite_balance_open lite_balance_ambush lite_balance_eyes"}

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
