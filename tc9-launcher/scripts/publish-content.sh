#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
    echo "usage: $0 VERSION ED25519_PRIVATE_KEY OUTPUT_DIRECTORY" >&2
    exit 2
fi

version=$1
private_key=$2
output_dir=$3
repo_dir=$(cd "$(dirname "$0")/.." && pwd)
base_url="https://launcher.expanded.space/downloads/swp/files"
realmlists_config=${REALMLISTS_CONFIG:-"$repo_dir/config/realmlists.json"}
launcher_version=$(sed -n 's/.*LauncherVersion[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$repo_dir/internal/client/selfupdate.go" | head -1)

jq -e '.default as $default | (.realms | length) > 0 and any(.realms[]; .id == $default) and all(.realms[]; (.id | length) > 0 and (.name | length) > 0 and (.realmlist | length) > 0 and (.realm_name | length) > 0)' "$realmlists_config" >/dev/null
if [[ -z "$launcher_version" ]]; then
    echo "cannot determine LauncherVersion" >&2
    exit 2
fi

stage_dir=$(mktemp -d)
payload_file=$(mktemp)
signature_file=$(mktemp)
trap 'rm -rf "$stage_dir"; rm -f "$payload_file" "$signature_file"' EXIT

"$repo_dir/scripts/stage-client.sh" "$stage_dir"
mkdir -p "$output_dir/files"
install -m 0755 "$repo_dir/dist/SWPLauncher.exe" "$output_dir/files/SWPLauncher.exe"

files_json='[]'
declare -A published_names
while IFS= read -r staged_file; do
    relative_path=${staged_file#"$stage_dir"/}
    if [[ "$relative_path" == "patch-T.MPQ" ]]; then
        managed_path="Data/patch-T.MPQ"
    else
        managed_path=$relative_path
    fi
    published_name=$(basename "$staged_file")
    if [[ -n ${published_names[$published_name]:-} ]]; then
        echo "published client filename collision: $published_name" >&2
        exit 1
    fi
    published_names[$published_name]=$managed_path
    install -m 0644 "$staged_file" "$output_dir/files/$published_name"
    files_json=$(jq -cn \
        --argjson files "$files_json" \
        --arg path "$managed_path" \
        --arg url "$base_url/$published_name" \
        --arg sha "$(sha256sum "$staged_file" | awk '{print $1}')" \
        --argjson size "$(stat -c %s "$staged_file")" \
        '$files + [{path:$path,url:$url,sha256:$sha,size:$size}]')
done < <(find "$stage_dir" -type f -print | sort)

jq -cn \
    --arg version "$version" \
    --arg launcher_version "$launcher_version" \
    --arg launcher_url "$base_url/SWPLauncher.exe" \
    --arg launcher_sha "$(sha256sum "$output_dir/files/SWPLauncher.exe" | awk '{print $1}')" \
    --argjson launcher_size "$(stat -c %s "$output_dir/files/SWPLauncher.exe")" \
    --argjson files "$files_json" \
    --slurpfile realm_config "$realmlists_config" \
    '{version:$version,launcher:{version:$launcher_version,url:$launcher_url,sha256:$launcher_sha,size:$launcher_size},default_environment:$realm_config[0].default,realms:$realm_config[0].realms,files:$files}' > "$payload_file"

openssl pkeyutl -sign -rawin -inkey "$private_key" -in "$payload_file" -out "$signature_file"
jq -n \
    --arg payload "$(base64 -w0 "$payload_file")" \
    --arg signature "$(base64 -w0 "$signature_file")" \
    '{payload:$payload,signature:$signature}' > "$output_dir/manifest.json"

echo "published signed client content manifest $version with launcher $launcher_version to $output_dir"
