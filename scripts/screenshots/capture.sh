#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
command -v freeze >/dev/null || { echo 'Install Charm Freeze to render the screenshots.' >&2; exit 1; }
capture_dir=$(mktemp -d)
trap 'rm -rf "$capture_dir"' EXIT
mkdir -p assets/screenshots
go build -o "$capture_dir/render" ./scripts/screenshots
for view in directory forge search; do
  "$capture_dir/render" --view "$view" > "$capture_dir/$view.ansi"
  # Freeze renders the ANSI output of our fixture-only tool, including its colors.
  freeze --execute "cat '$capture_dir/$view.ansi'" \
    --output "assets/screenshots/$view.png" \
    --background '#101015' --padding 12 --margin 0 \
    --font.family 'Source Code Pro' --font.size 8 --line-height 1.45 \
    --window=false --border.radius 6 --border.width 0.5 --border.color '#292633'
done
