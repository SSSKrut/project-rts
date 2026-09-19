#!/usr/bin/env bash
# Records one clip of a scene into docs/media: an mp4 master, a README-sized
# gif and a poster png. The game streams raw frames through a fifo; ffmpeg
# encodes on the other end, so nothing lands on disk uncompressed.
#
#   scripts/record_clip.sh <name> [options] -- <rts flags>
#
#   --size WxH       window and master size            (default 1280x720)
#   --every N        capture every Nth frame; 60/N fps  (default 2 -> 30 fps)
#   --gif-width W    gif width in px                    (default 800)
#   --gif-fps N      gif frame rate                     (default 15)
#   --gif-from SEC   gif start inside the master        (default 0)
#   --gif-len SEC    gif length; 0 = whole master       (default 0)
#   --poster SEC     poster frame time in the master    (default: middle)
#   --crf N          x264 quality of the master           (default 18)
#   --out DIR        media root                         (default docs/media)
#
# Everything after -- goes to the game verbatim: -scene / -mission, -rec-to,
# -rec-speed, -rec-clean, -shot-cam, -rec-track, -rec-orbit, -hour ...
# Frames are indexed, not timed: the same command records the same clip.
set -euo pipefail
export LC_NUMERIC=C

ROOT=$(cd "$(dirname "$0")/.." && pwd)
NAME=${1:?clip name}; shift
SIZE=1280x720 EVERY=2 GIF_W=800 GIF_FPS=15 GIF_FROM=0 GIF_LEN=0 POSTER="" CRF=18 OUT="$ROOT/docs/media"
while [ $# -gt 0 ]; do
  case "$1" in
    --size) SIZE=$2; shift 2 ;;
    --every) EVERY=$2; shift 2 ;;
    --gif-width) GIF_W=$2; shift 2 ;;
    --gif-fps) GIF_FPS=$2; shift 2 ;;
    --gif-from) GIF_FROM=$2; shift 2 ;;
    --gif-len) GIF_LEN=$2; shift 2 ;;
    --poster) POSTER=$2; shift 2 ;;
    --crf) CRF=$2; shift 2 ;;
    --out) OUT=$2; shift 2 ;;
    --) shift; break ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done

mkdir -p "$OUT/mp4" "$OUT/gif" "$OUT/png"
cd "$ROOT"
[ -x bin/rts ] || go build -o bin/rts .

MP4="$OUT/mp4/$NAME.mp4"
GIF="$OUT/gif/$NAME.gif"
PNG="$OUT/png/$NAME.png"
LOG=$(mktemp -t rts-rec-"$NAME".XXXX.log)
FIFO=$(mktemp -u -t rts-rec-"$NAME".XXXX.rgba)
mkfifo "$FIFO"
trap 'rm -f "$FIFO"' EXIT

FPS=$((60 / EVERY))
ffmpeg -y -loglevel error -f rawvideo -pixel_format rgba -video_size "$SIZE" -framerate "$FPS" \
  -i "$FIFO" -c:v libx264 -preset slow -crf "$CRF" -pix_fmt yuv420p -movflags +faststart "$MP4" &
FFPID=$!

set +e
./bin/rts "$@" -watch -rec="$FIFO" -rec-size="$SIZE" -rec-every="$EVERY" >"$LOG" 2>&1
RC=$?
set -e
# A run that never opened the fifo leaves ffmpeg blocked on it.
timeout 5 sh -c ": > '$FIFO'" 2>/dev/null || true
wait $FFPID
if [ $RC -ne 0 ]; then
  echo "game exited $RC, log: $LOG" >&2
  tail -20 "$LOG" >&2
  exit $RC
fi
grep -E '^\[rec\]|^== |PASS|FAIL' "$LOG" || true

DUR=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$MP4")
[ -n "$POSTER" ] || POSTER=$(awk -v d="$DUR" 'BEGIN{printf "%.2f", d/2}')
LEN_ARGS=()
[ "$GIF_LEN" != "0" ] && LEN_ARGS=(-t "$GIF_LEN")
ffmpeg -y -loglevel error -ss "$GIF_FROM" "${LEN_ARGS[@]}" -i "$MP4" \
  -vf "fps=$GIF_FPS,scale=$GIF_W:-2:flags=lanczos,split[a][b];[a]palettegen=max_colors=200:stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle" \
  "$GIF"
ffmpeg -y -loglevel error -ss "$POSTER" -i "$MP4" -frames:v 1 "$PNG"

mb() { awk -v b="$(stat -c %s "$1")" 'BEGIN{printf "%.1f", b/1048576}'; }
printf '%-16s %6.1fs  mp4 %5s MB  gif %5s MB  png %s\n' "$NAME" "$DUR" "$(mb "$MP4")" "$(mb "$GIF")" "$(basename "$PNG")"
