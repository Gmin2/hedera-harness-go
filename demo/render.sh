#!/usr/bin/env bash
# renders every tape. readme gifs land in demo/assets, video clips in demo/out.
# vhs 0.12.0 records fine but never encodes: evaluator.go cancels its recording
# context and then passes that same context to exec.CommandContext for ffmpeg,
# so ffmpeg never starts and the error is printed as an empty line. 0.11.0 is
# not affected. until a fix ships, vhs only records frames and we encode here.
set -euo pipefail
cd "$(dirname "$0")/.."

tapes="hero agent run check init compare"
[ -f .env ] && tapes="$tapes proof"

for tape in $tapes; do
  echo "rendering $tape"
  frames="tmp/frames/$tape"
  rm -rf "$frames" && mkdir -p tmp/frames
  out=$(grep -m1 '^Output ' "demo/$tape.tape" | awk '{print $2}')
  sed "s|^Output .*|Output $frames/|" "demo/$tape.tape" > "tmp/frames/$tape.tape"
  vhs "tmp/frames/$tape.tape" || true

  start=$(ls "$frames" | grep frame-text | sort | head -1 | sed 's/[^0-9]//g')
  fps=$(awk '/^Set Framerate/ {print $3; exit}' "demo/$tape.tape")
  fps=${fps:-50}
  in=(-framerate "$fps" -start_number "$((10#$start))" -i "$frames/frame-text-%05d.png"
      -framerate "$fps" -start_number "$((10#$start))" -i "$frames/frame-cursor-%05d.png")
  mkdir -p "$(dirname "$out")"
  case "$out" in
    *.gif) ffmpeg -loglevel error -y "${in[@]}" -filter_complex "[0][1]overlay,fps=$fps,split[a][b];[a]palettegen=max_colors=128[p];[b][p]paletteuse=dither=none" "$out" ;;
    *.mp4) ffmpeg -loglevel error -y "${in[@]}" -filter_complex "[0][1]overlay,scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p" -c:v libx264 -crf 20 "$out" ;;
  esac
  echo "  wrote $out"
done
