#!/usr/bin/env bash
# Single frames for the README, 1920x1080, into docs/media/png. Same capture
# path as the clips (-rec in PNG mode, one frame), same reproducibility.
set -u
export LC_NUMERIC=C
cd "$(dirname "$0")/.."
go build -o bin/rts . || exit 1
mkdir -p docs/media/png
ONLY=${1:-}
still() {
  local name=$1 frame=$2; shift 2
  if [ -n "$ONLY" ] && [ "$ONLY" != "$name" ]; then return; fi
  local tmp; tmp=$(mktemp -d -t rts-still-XXXX)
  ./bin/rts "$@" -watch -rec="$tmp/" -rec-from="$frame" -rec-to="$frame" -rec-every=1 -rec-size=1920x1080 >"$tmp/log" 2>&1
  if ls "$tmp"/f*.png >/dev/null 2>&1; then
    mv "$tmp"/f*.png "docs/media/png/$name.png"
    echo "$name  $(stat -c %s "docs/media/png/$name.png" | awk '{printf "%.1f MB", $1/1048576}')"
  else
    echo "$name: no frame (see $tmp/log)" >&2
    return 1
  fi
  rm -rf "$tmp"
}

still city_aerial_evening 900  -mission=city -rec-clean -rec-speed=4 -shot-cam=260,48,-135 -rec-anchor=0,0 -hour=17.0
still city_bridge         1500 -mission=city -rec-clean -rec-speed=4 -shot-cam=70,22,60    -rec-anchor=-200,0
still mass_columns        400  -mission=city -rec-clean -rec-speed=2 -shot-cam=130,35,-90  -rec-anchor=-280,0
still tank_closeup        400  -scene=ai_vehicle_combat -rec-clean -rec-bare -shot-cam=13,14,150 -shot-select-veh=1 -rec-track=sel
still convoy_bridge       700  -scene=ai_vehicle_convoy_road -rec-clean -rec-speed=2 -shot-cam=40,20,-30 -rec-track=player
still convoy_sunset       200  -scene=ai_vehicle_convoy_road -rec-clean -rec-speed=1 -shot-cam=50,30,-40 -rec-track=player -hour=17.5
