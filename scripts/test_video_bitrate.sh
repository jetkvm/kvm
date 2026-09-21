#!/bin/sh
set -eu
repo_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
test_binary=$(mktemp)
trap 'rm -f "$test_binary"' EXIT
for test in video_bitrate_test video_remb_gate_test; do
    ${CC:-cc} -std=c11 -Wall -Wextra -Werror "$repo_dir/internal/native/tests/$test.c" -o "$test_binary"
    "$test_binary"
done
