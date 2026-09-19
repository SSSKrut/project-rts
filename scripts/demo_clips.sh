#!/usr/bin/env bash
# Regenerates the README media in docs/media (gif + png tracked, mp4 masters
# ignored). Every clip is a scripted scene or mission under a fixed camera, so
# a re-run reproduces the same frames. Pass a clip name to record just one.
#
# Wall time is dominated by the city mission (~6 min per clip: 130 bodies and
# 268 buildings at 4x). The whole set is about 40 minutes.
set -u
cd "$(dirname "$0")/.."
go build -o bin/rts . || exit 1
ONLY=${1:-}
clip() {
  local name=$1; shift
  if [ -n "$ONLY" ] && [ "$ONLY" != "$name" ]; then return; fi
  scripts/record_clip.sh "$name" "$@"
}

# --- behaviour, field only (chromeless) ------------------------------------
clip bounding       --gif-from 4  --gif-len 10 -- -scene=ai_bounding        -rec-clean -rec-speed=2 -rec-to=1300 -shot-cam=24,30,0    -rec-track=player -rec-track-snap=20
clip clear_building --gif-from 2  --gif-len 10 -- -scene=ai_clear_building  -rec-clean -rec-speed=2 -rec-to=1500 -shot-cam=22,52,20
clip focus_fire     --gif-from 1  --gif-len 8  -- -scene=ai_focus_fire      -rec-clean -rec-speed=1 -rec-to=700  -shot-cam=26,30,-30  -rec-track=all -rec-track-snap=30
clip vehicle_combat --gif-from 1  --gif-len 10 -- -scene=ai_vehicle_combat  -rec-clean -rec-speed=1 -rec-to=900  -shot-cam=45,28,20
clip ambush         --gif-from 6  --gif-len 10 -- -scene=lite_balance_ambush -rec-clean -rec-speed=2 -rec-to=1400 -shot-cam=45,30,0   -rec-track=player -rec-track-snap=25
clip open_field     --gif-from 6  --gif-len 10 -- -scene=lite_balance_open  -rec-clean -rec-speed=2 -rec-to=1300 -shot-cam=50,30,90   -rec-track=all -rec-track-snap=30
clip convoy_road    --gif-from 2  --gif-len 10 -- -scene=ai_vehicle_convoy_road -rec-clean -rec-speed=2 -rec-to=1500 -shot-cam=50,30,-40 -rec-track=player -rec-track-snap=25
clip shellfire      --gif-from 2  --gif-len 10 -- -scene=ai_shellfire       -rec-clean -rec-speed=1 -rec-to=1200 -shot-cam=28,35,0    -rec-track=player -rec-track-snap=25
clip march_column   --gif-from 2  --gif-len 8  -- -scene=ai_march_column    -rec-clean -rec-speed=2 -rec-to=1200 -shot-cam=30,30,-60  -rec-track=player -rec-track-snap=25
clip air_cas        --gif-from 8  --gif-len 10 -- -scene=ai_air_cas         -rec-clean -rec-speed=2 -rec-to=1800 -shot-cam=70,25,0    -rec-track=all -rec-track-snap=45

# --- scale: the city mission, both sides bot-driven -------------------------
# A still camera: the gif is the two sides crossing the city, and a moving
# camera over a noisy ground texture is 40 MB of gif. The orbit lives in
# city_orbit, whose value is the mp4.
clip city_aerial    --gif-from 4  --gif-len 12 -- -mission=city -rec-clean -rec-speed=4 -rec-to=1800 -shot-cam=260,48,-135 -rec-anchor=0,0
clip city_orbit     --crf 26 --gif-width 480 --gif-fps 10 --gif-from 6 --gif-len 5 -- -mission=city -rec-clean -rec-speed=4 -rec-to=1800 -shot-cam=240,45,-135 -rec-anchor=0,0 -rec-orbit=5
clip city_street    --gif-from 0  --gif-len 10 -- -mission=city -rec-clean -rec-speed=4 -rec-from=2100 -rec-to=2700 -shot-cam=55,26,45 -rec-anchor=0,0

# --- the command surface (full UI) ------------------------------------------
clip ui_command     --gif-width 960 --gif-from 2 --gif-len 10 -- -mission=crossroads -rec-layout=field -rec-speed=2 -rec-to=1500 -shot-cam=40,35,-60 -shot-select=8 '-shot-order=-70,-60;0,0' -rec-track=sel -rec-track-snap=25
clip ui_map         --gif-width 960 --gif-from 4 --gif-len 12 -- -mission=city -rec-layout=command -rec-speed=4 -rec-to=1800 -shot-cam=120,45,-135 -rec-anchor=0,0
