#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 || $# -gt 4 ]]; then
    echo "usage: $0 /path/to/3.3.5a/server/dbc OUTPUT_DIRECTORY [/path/to/mod_qol.conf] [/path/to/mod_gemstones.yaml]" >&2
    exit 2
fi

repo_dir=$(cd "$(dirname "$0")/.." && pwd)
base_dbc_dir=$1
output_dir=$2
mod_qol_config=${3:-"$repo_dir/../game-server/azerothcore/modules/mod-qol/conf/mod_qol.conf.dist"}
mod_gemstones_config=${4:-"$repo_dir/../game-server/azerothcore/modules/mod-gemstones/conf/mod_gemstones.yaml.dist"}

mkdir -p "$output_dir"
for profile in vanilla tbc wotlk; do
    RETRO_CLIENT_EXPANSION=$profile \
    PATCH_OUTPUT="$output_dir/patch-T-$profile.MPQ" \
        "$repo_dir/scripts/build-patch.sh" "$base_dbc_dir" "$mod_qol_config" "$mod_gemstones_config"
done

sha256sum "$output_dir"/patch-T-*.MPQ > "$output_dir/SHA256SUMS"
echo "generated Vanilla, TBC, and WotLK launcher patches in $output_dir"
