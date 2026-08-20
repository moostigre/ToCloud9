#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
    echo "usage: $0 MODULE_CATALOG OUTPUT_DIRECTORY" >&2
    exit 2
fi

catalog=$1
output_dir=$2

jq -e '
    .schema == 1 and
    (.modules | type == "array") and
    all(.modules[];
        (.name | test("^[A-Za-z0-9._-]+$")) and
        (.repository | test("^git@github\\.com:super-wow-project/[A-Za-z0-9._-]+\\.git$")) and
        (.revision | test("^[A-Za-z0-9._/-]+$")) and
        .client_path == "client")
' "$catalog" >/dev/null

mkdir -p "$output_dir"
while IFS=$'\t' read -r name repository revision client_path; do
    destination="$output_dir/$name"
    if [[ -e "$destination" ]]; then
        echo "module destination already exists: $destination" >&2
        exit 1
    fi
    git clone --quiet --depth 1 --branch "$revision" "$repository" "$destination"
    if [[ ! -d "$destination/$client_path" ]]; then
        echo "module has no declared client path: $name/$client_path" >&2
        exit 1
    fi
    echo "fetched module client assets: $name@$revision"
done < <(jq -r '.modules[] | [.name, .repository, .revision, .client_path] | @tsv' "$catalog")
