#!/usr/bin/env python3
"""Transactionally patch AzerothCore item_template from a loaded profile."""

import argparse
import subprocess


def update_sql(profile: str) -> str:
    stat_joins = "\n".join(
        f"LEFT JOIN retro_itemization_stat s{i} ON s{i}.profile=p.profile "
        f"AND s{i}.entry=p.entry AND s{i}.stat_slot={i}" for i in range(1, 11)
    )
    stat_sets = ["i.StatsCount=p.stats_count"]
    for i in range(1, 11):
        stat_sets.extend((
            f"i.stat_type{i}=COALESCE(s{i}.stat_type,0)",
            f"i.stat_value{i}=COALESCE(s{i}.stat_value,0)",
        ))
    spell_joins = "\n".join(
        f"JOIN retro_itemization_spell x{i} ON x{i}.profile=p.profile "
        f"AND x{i}.entry=p.entry AND x{i}.spell_slot={i}" for i in range(1, 6)
    )
    spell_sets = []
    mapping = (
        ("spellid", "spell_id"), ("spelltrigger", "spell_trigger"),
        ("spellcharges", "spell_charges"), ("spellppmRate", "spell_ppm_rate"),
        ("spellcooldown", "spell_cooldown"), ("spellcategory", "spell_category"),
        ("spellcategorycooldown", "spell_category_cooldown"),
    )
    for i in range(1, 6):
        spell_sets.extend(f"i.{target}_{i}=x{i}.{source}" for target, source in mapping)
    quoted = "'" + profile.replace("'", "''") + "'"
    return f"""START TRANSACTION;
UPDATE item_template i
JOIN retro_itemization_item p ON p.entry=i.entry AND p.profile={quoted}
{stat_joins}
SET {','.join(stat_sets)};
UPDATE item_template i
JOIN retro_itemization_item p ON p.entry=i.entry AND p.profile={quoted}
{spell_joins}
SET {','.join(spell_sets)};
COMMIT;
"""


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--profile", required=True, choices=("vanilla", "tbc", "wotlk"))
    parser.add_argument("--container", default="tc9-retro-reference-db")
    parser.add_argument("--database", default="acore_world")
    parser.add_argument("--user", default="root")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    sql = update_sql(args.profile)
    if args.dry_run:
        print(sql, end="")
        return
    command = ["docker", "exec", "-i", args.container, "mysql", f"-u{args.user}", args.database]
    subprocess.run(command, input=sql, text=True, check=True)
    print(f"patched {args.database}.item_template to {args.profile}; restart worldserver")


if __name__ == "__main__":
    main()
