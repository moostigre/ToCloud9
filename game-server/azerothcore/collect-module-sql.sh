#!/usr/bin/env bash
set -euo pipefail

modules_root="${1:-/repo/modules}"
custom_root="${2:-/repo/data/sql/custom}"
max_file_bytes=$((32 * 1024 * 1024))
max_total_bytes=$((256 * 1024 * 1024))
total_bytes=0

mkdir -p "${custom_root}/db_auth" "${custom_root}/db_characters" "${custom_root}/db_world"

for module_root in "${modules_root}"/*; do
  [[ -d "${module_root}" ]] || continue
  module="$(basename "${module_root}")"
  [[ "${module}" =~ ^[a-z0-9][a-z0-9-]{1,62}$ ]]

  for mapping in \
    "db-auth:db_auth" \
    "db-characters:db_characters" \
    "db-world:db_world"; do
    source_database="${mapping%%:*}"
    target_database="${mapping#*:}"
    for phase in base updates; do
      source_root="${module_root}/data/sql/${source_database}/${phase}"
      [[ -d "${source_root}" ]] || continue
      while IFS= read -r -d '' source; do
        bytes="$(stat -c %s "${source}")"
        (( bytes <= max_file_bytes ))
        total_bytes=$((total_bytes + bytes))
        (( total_bytes <= max_total_bytes ))
        relative="${source#"${source_root}/"}"
        digest="$(printf '%s' "${relative}" | sha256sum | cut -c1-16)"
        target="${custom_root}/${target_database}/${module}--${phase}--${digest}.sql"
        install -m 0644 "${source}" "${target}"
      done < <(find -P "${source_root}" -type f -name '*.sql' -print0 | sort -z)
    done
  done
done
