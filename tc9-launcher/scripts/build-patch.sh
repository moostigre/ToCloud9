#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 3 ]]; then
    echo "usage: $0 /path/to/3.3.5a/server/dbc [/path/to/mod_qol.conf] [/path/to/mod_gemstones.yaml]" >&2
    exit 2
fi

repo_dir=$(cd "$(dirname "$0")/.." && pwd)
modules_dir="$repo_dir/../game-server/azerothcore/modules"
retro_config="$modules_dir/mod-retro-client/conf/mod_retro_client.conf"
[[ -f "$retro_config" ]] || retro_config="$modules_dir/mod-retro-client/conf/mod_retro_client.conf.dist"
base_dbc_dir=$1
mod_qol_config=${2:-"$repo_dir/../game-server/azerothcore/modules/mod-qol/conf/mod_qol.conf.dist"}
mod_gemstones_config=${3:-"$repo_dir/../game-server/azerothcore/modules/mod-gemstones/conf/mod_gemstones.yaml.dist"}
build_dir=$(mktemp -d)
trap 'rm -rf "$build_dir"' EXIT

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

retro_expansion=${RETRO_CLIENT_EXPANSION:-$(config_value RetroClient.Expansion)}
retro_expansion=${retro_expansion:-WotLK}
retro_expansion=${retro_expansion,,}
case "$retro_expansion" in
    vanilla|tbc|wotlk) ;;
    *)
        echo "invalid RetroClient.Expansion in $retro_config: expected Vanilla, TBC, or WotLK" >&2
        exit 2
        ;;
esac

for requirement in \
    "$modules_dir/mod-qol/client/dbc/requirements.json" \
    "$modules_dir/mod-gemstones/client/dbc/requirements.json"; do
    if [[ ! -f "$requirement" ]] || ! jq -e '.generator == "swp-dbcpatch" and (.records | length > 0)' "$requirement" >/dev/null; then
        echo "missing or invalid module client DBC declaration: $requirement" >&2
        exit 2
    fi
done

if ! grep -q '^[[:space:]]*Qol\.OutOfCombatRunSpeed[[:space:]]*=' "$mod_qol_config"; then
    echo "Qol.OutOfCombatRunSpeed is missing from $mod_qol_config" >&2
    exit 2
fi
if ! grep -q '^gemstones:' "$mod_gemstones_config"; then
    echo "gemstones YAML has no gemstones list in $mod_gemstones_config" >&2
    exit 2
fi

go run "$repo_dir/tools/dbcpatch/main.go" \
    "$base_dbc_dir" \
    "$build_dir/files" \
    "$repo_dir/data/racial_mounts.json" \
    "$repo_dir/server/generated_tiny_mounts.sql" \
    "$mod_qol_config" \
    "$mod_gemstones_config"

if [[ "$retro_expansion" == tbc ]]; then
    legacy_manifest=${LEGACY_TALENTS_TBC_MANIFEST:-/data/swp-tbc-profile/server/tbc-enforcement.json}
    historical_dbc_dir=${LEGACY_TALENTS_TBC_DBC_DIR:-/data/swp-tbc-source/extracted/DBFilesClient}
    python3 "$modules_dir/mod-legacy-talents/tools/patch_native_dbc.py" \
        --manifest "$legacy_manifest" \
        --input-dbc-dir "$build_dir/files/DBFilesClient" \
        --historical-dbc-dir "$historical_dbc_dir" \
        --compatibility-report "${LEGACY_TALENTS_TBC_COMPATIBILITY_REPORT:-/data/swp-tbc-profile/server/tbc-launcher-compatibility.json}" \
        --output-dbc-dir "$build_dir/files/DBFilesClient"
fi

# Modules may also provide MPQ-ready files that do not require record-level
# generation. Preserve their path below client/patch and reject collisions so
# one module can never silently overwrite another or a generated DBC.
while IFS= read -r module_patch; do
    if [[ "$module_patch" == "$modules_dir/mod-retro-client/client/"* ]]; then
        config_enabled RetroClient.Enable || continue
        case "$module_patch" in
            */features/tbc-login-screen/patch/*)
                [[ "$retro_expansion" == tbc ]] || continue
                config_enabled RetroClient.LoginScreen || continue
                ;;
            */features/tbc-logo/patch/*)
                [[ "$retro_expansion" == tbc ]] || continue
                config_enabled RetroClient.Logo || continue
                ;;
            */features/vanilla-login-screen/patch/*)
                [[ "$retro_expansion" == vanilla ]] || continue
                config_enabled RetroClient.LoginScreen || continue
                ;;
            */features/vanilla-logo/patch/*)
                [[ "$retro_expansion" == vanilla ]] || continue
                config_enabled RetroClient.Logo || continue
                ;;
            */features/red-buttons/patch/*)
                [[ "$retro_expansion" != wotlk ]] || continue
                config_enabled RetroClient.RedButtons || continue
                ;;
        esac
    fi
    if [[ "$module_patch" == */client/features/*/patch/* ]]; then
        relative_path=${module_patch#*/patch/}
    else
        relative_path=${module_patch#*/client/patch/}
    fi
    target="$build_dir/files/$relative_path"
    if [[ -e "$target" ]]; then
        echo "module client patch collision at $relative_path" >&2
        exit 1
    fi
    install -D -m 0644 "$module_patch" "$target"
done < <(find "$modules_dir" -type f \
    \( -path '*/client/patch/*' -o -path '*/client/features/*/patch/*' \) \
    ! -name '.gitkeep' -print | sort)

mapfile -t patch_files < <(cd "$build_dir/files" && find . -type f -printf '%P\n' | sort)
(cd "$build_dir/files" && smpq -c -M 1 -C ZLIB "$build_dir/patch-T.MPQ" "${patch_files[@]}")

patch_output=${PATCH_OUTPUT:-"$repo_dir/assets/patch-T.MPQ"}
install -D -m 0644 "$build_dir/patch-T.MPQ" "$patch_output"
echo "rebuilt $patch_output for $retro_expansion"
