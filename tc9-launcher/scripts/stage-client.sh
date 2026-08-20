#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "usage: $0 OUTPUT_DIRECTORY" >&2
    exit 2
fi

output_dir=$1
repo_dir=$(cd "$(dirname "$0")/.." && pwd)
modules_dir="$repo_dir/../game-server/azerothcore/modules"
swp_dir="$output_dir/Interface/AddOns/SWP"
retro_config="$modules_dir/mod-retro-client/conf/mod_retro_client.conf"
[[ -f "$retro_config" ]] || retro_config="$modules_dir/mod-retro-client/conf/mod_retro_client.conf.dist"

config_enabled() {
    local key=$1
    local value
    value=$(sed -n "s/^[[:space:]]*${key//./\\.}[[:space:]]*=[[:space:]]*\\([^#;[:space:]]*\\).*/\\1/p" "$retro_config" | tail -1)
    case "${value,,}" in
        0|false|off|no) return 1 ;;
        *) return 0 ;;
    esac
}

config_value() {
    local key=$1
    sed -n "s/^[[:space:]]*${key//./\\.}[[:space:]]*=[[:space:]]*\\([^#;[:space:]]*\\).*/\\1/p" "$retro_config" | tail -1
}

retro_expansion=$(config_value RetroClient.Expansion)
retro_expansion=${retro_expansion:-WotLK}
retro_expansion=${retro_expansion,,}

mkdir -p "$swp_dir"
install -m 0644 "$repo_dir/client/SWP/SWP.toc" "$swp_dir/SWP.toc"
install -m 0644 "$repo_dir/client/SWP/SWP.lua" "$swp_dir/SWP.lua"

# Optional release-only payload used to exercise updater progress UI without
# loading the file as an addon. Normal releases leave this unset.
if [[ -n ${SWP_LOADING_BAR_TEST_FILE:-} ]]; then
    install -m 0644 "$SWP_LOADING_BAR_TEST_FILE" "$swp_dir/loading-bar-test.bin"
fi

declare -A staged
staged[SWP.toc]="$repo_dir/client/SWP/SWP.toc"
staged[SWP.lua]="$repo_dir/client/SWP/SWP.lua"

while IFS= read -r merge_file; do
    if [[ "$merge_file" == "$modules_dir/mod-retro-client/client/features/talent-ui/"* ]]; then
        config_enabled RetroClient.Enable || continue
        [[ "$retro_expansion" == tbc ]] || continue
        config_enabled RetroClient.TbcTalentUI || continue
    fi
    module_client=$(dirname "$merge_file")
    addon_root="$module_client/Interface/AddOns"
    if [[ ! -d "$addon_root" ]]; then
        addon_root="$module_client"
    fi
    while IFS= read -r relative_path || [[ -n "$relative_path" ]]; do
        relative_path=${relative_path%%#*}
        relative_path=$(echo "$relative_path" | xargs)
        [[ -z "$relative_path" ]] && continue
        source_path="$addon_root/$relative_path"
        target_name=$(basename "$relative_path")
        if [[ ! -f "$source_path" ]]; then
            echo "missing module client file: $source_path" >&2
            exit 1
        fi
        if [[ -n ${staged[$target_name]:-} ]]; then
            echo "module client collision for $target_name: ${staged[$target_name]} and $source_path" >&2
            exit 1
        fi
        install -m 0644 "$source_path" "$swp_dir/$target_name"
        if ! grep -Fxq "$target_name" "$swp_dir/SWP.toc"; then
            printf '\n%s\n' "$target_name" >> "$swp_dir/SWP.toc"
        fi
        staged[$target_name]=$source_path
        echo "merged module client file $relative_path into SWP/$target_name"
    done < "$merge_file"
done < <(find "$modules_dir" -type f \
    \( -path '*/client/merge-into-swp.txt' -o -path '*/client/features/*/merge-into-swp.txt' \) \
    -print | sort)

legacy_talents_config="$modules_dir/mod-legacy-talents/conf/mod_legacy_talents.conf"
[[ -f "$legacy_talents_config" ]] || legacy_talents_config="$modules_dir/mod-legacy-talents/conf/mod_legacy_talents.conf.dist"
if [[ -f "$swp_dir/LegacyTalentsProfile.lua" ]]; then
    legacy_talents_profile=$(sed -n 's/^[[:space:]]*LegacyTalents\.Profile[[:space:]]*=[[:space:]]*\([^#;[:space:]]*\).*/\1/p' "$legacy_talents_config" | tail -1)
    legacy_talents_profile=${legacy_talents_profile:-WotLK}
    printf 'LegacyTalentsProfile = "%s"\n' "${legacy_talents_profile,,}" > "$swp_dir/LegacyTalentsProfile.lua"
fi

# The TBC compatibility layer declares that the talent frame is single-tree.
# Load it before multispec so the latter cannot briefly create Wrath/triple-spec
# controls when Blizzard_TalentUI was already loaded (notably after /reload).
if [[ -f "$swp_dir/SWPTbcTalentUI.lua" ]]; then
    sed -i '/^SWPTbcTalentUI\.lua$/d' "$swp_dir/SWP.toc"
    sed -i '/^SWPMultispecs\.lua$/i SWPTbcTalentUI.lua' "$swp_dir/SWP.toc"
fi

install -m 0644 "$repo_dir/assets/patch-T.MPQ" "$output_dir/patch-T.MPQ"
