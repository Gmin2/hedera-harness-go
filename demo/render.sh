#!/usr/bin/env bash
# renders every tape. readme gifs land in demo/assets, video clips in demo/out
set -euo pipefail
cd "$(dirname "$0")/.."
for tape in hero run compare; do
  echo "rendering $tape"
  vhs "demo/$tape.tape"
done
if [ -f .env ]; then
  echo "rendering proof (testnet)"
  vhs demo/proof.tape
fi
