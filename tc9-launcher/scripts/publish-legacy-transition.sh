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
base_url="http://163.172.51.144:3000/downloads/client/files"
mkdir -p "$output_dir/files"

install -m 0644 "$repo_dir/assets/patch-T.MPQ" "$output_dir/files/patch-T.MPQ"
install -m 0644 "$repo_dir/client/SWP/SWP.toc" "$output_dir/files/SWPClient.toc"
install -m 0644 "$repo_dir/client/SWP/SWP.lua" "$output_dir/files/SWPClient.lua"
install -m 0755 "$repo_dir/dist/SWPLauncher.exe" "$output_dir/files/SWPLauncher.exe"

payload_file=$(mktemp)
signature_file=$(mktemp)
trap 'rm -f "$payload_file" "$signature_file"' EXIT

jq -cn \
    --arg version "$version" \
    --arg patch_url "$base_url/patch-T.MPQ" \
    --arg patch_sha "$(sha256sum "$output_dir/files/patch-T.MPQ" | awk '{print $1}')" \
    --argjson patch_size "$(stat -c %s "$output_dir/files/patch-T.MPQ")" \
    --arg toc_url "$base_url/SWPClient.toc" \
    --arg toc_sha "$(sha256sum "$output_dir/files/SWPClient.toc" | awk '{print $1}')" \
    --argjson toc_size "$(stat -c %s "$output_dir/files/SWPClient.toc")" \
    --arg lua_url "$base_url/SWPClient.lua" \
    --arg lua_sha "$(sha256sum "$output_dir/files/SWPClient.lua" | awk '{print $1}')" \
    --argjson lua_size "$(stat -c %s "$output_dir/files/SWPClient.lua")" \
    --arg launcher_url "$base_url/SWPLauncher.exe" \
    --arg launcher_sha "$(sha256sum "$output_dir/files/SWPLauncher.exe" | awk '{print $1}')" \
    --argjson launcher_size "$(stat -c %s "$output_dir/files/SWPLauncher.exe")" \
    '{version:$version,launcher:{version:"0.6.0",url:$launcher_url,sha256:$launcher_sha,size:$launcher_size},files:[
      {path:"Data/patch-T.MPQ",url:$patch_url,sha256:$patch_sha,size:$patch_size},
      {path:"Interface/AddOns/ToCloud9Client/ToCloud9Client.toc",url:$toc_url,sha256:$toc_sha,size:$toc_size},
      {path:"Interface/AddOns/ToCloud9Client/ToCloud9Client.lua",url:$lua_url,sha256:$lua_sha,size:$lua_size}
    ]}' > "$payload_file"

openssl pkeyutl -sign -rawin -inkey "$private_key" -in "$payload_file" -out "$signature_file"
jq -n --arg payload "$(base64 -w0 "$payload_file")" --arg signature "$(base64 -w0 "$signature_file")" \
    '{payload:$payload,signature:$signature}' > "$output_dir/manifest.json"

echo "published legacy-to-SWP transition manifest $version to $output_dir"
