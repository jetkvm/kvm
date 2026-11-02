#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

LV_FONT_CONV="npx --yes lv_font_conv@1.5.2"

gen() {
  local name=$1 ttf=$2 size=$3 line_height=$4 base_line=$5 out="ui_font_font_$1.c"
  $LV_FONT_CONV --bpp 4 --size "$size" --no-compress --font "inter/$ttf" --range 32-127 --format lvgl \
    -o "$out"
  sed -i \
    -e 's|#include "lvgl/lvgl.h"|#include "lvgl.h"|' \
    -e "s/^\( *\.line_height = \)[0-9]*,/\1$line_height,/" \
    -e "s/^\( *\.base_line = \)[0-9]*,/\1$base_line,/" \
    "$out"
}

gen book16 Inter-Regular.ttf 15 17 3
gen book18 Inter-Regular.ttf 17 19 4
gen book20 Inter-Regular.ttf 19 20 5
gen book24 Inter-Regular.ttf 23 25 5
gen bold30 Inter-Bold.ttf 27 31 6
