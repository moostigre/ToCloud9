#!/usr/bin/env python3
"""Generate normalized mod-retro-itemization SQL from reference databases."""

import argparse
import pathlib
import struct
import subprocess
import sys


STAT_COLUMNS = ",".join(
    f"s.stat_type{i},s.stat_value{i}" for i in range(1, 11)
)
SPELL_COLUMNS = ",".join(
    f"s.spellid_{i},s.spelltrigger_{i},s.spellcharges_{i},s.spellppmRate_{i},"
    f"s.spellcooldown_{i},s.spellcategory_{i},s.spellcategorycooldown_{i}"
    for i in range(1, 6)
)


def arguments() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--profile", required=True, choices=("vanilla", "tbc"))
    parser.add_argument("--container", default="tc9-retro-reference-db")
    parser.add_argument("--vanilla-db", default="retro_ref_vanilla")
    parser.add_argument("--tbc-db", default="retro_ref_tbc")
    parser.add_argument("--wotlk-db", default="retro_ref_wotlk")
    parser.add_argument("--source-revision", required=True)
    parser.add_argument("--wotlk-spell-dbc", type=pathlib.Path, required=True)
    parser.add_argument("--output", type=pathlib.Path, required=True)
    return parser.parse_args()


def dbc_ids(path: pathlib.Path) -> set[int]:
    data = path.read_bytes()
    if len(data) < 20 or data[:4] != b"WDBC":
        raise RuntimeError(f"{path} is not a WDBC file")
    count, _fields, record_size, string_size = struct.unpack_from("<4I", data, 4)
    record_end = 20 + count * record_size
    if record_size < 4 or record_end + string_size != len(data):
        raise RuntimeError(f"{path} has invalid DBC dimensions")
    return {struct.unpack_from("<I", data, 20 + index * record_size)[0] for index in range(count)}


def source_query(args: argparse.Namespace) -> str:
    if args.profile == "vanilla":
        source = (
            f"{args.vanilla_db}.item_template s JOIN "
            f"(SELECT entry,MAX(patch) patch FROM {args.vanilla_db}.item_template "
            "GROUP BY entry) latest USING(entry,patch)"
        )
    else:
        source = f"{args.tbc_db}.item_template s"

    return (
        f"SELECT s.entry,{STAT_COLUMNS},{SPELL_COLUMNS} FROM {source} "
        f"JOIN {args.wotlk_db}.item_template w ON w.entry=s.entry ORDER BY s.entry"
    )


def mysql_rows(args: argparse.Namespace):
    command = [
        "docker", "exec", args.container, "mysql", "-uroot", "--batch", "--raw",
        "--skip-column-names", "-e", source_query(args),
    ]
    process = subprocess.Popen(command, stdout=subprocess.PIPE, text=True)
    assert process.stdout is not None
    for line_number, line in enumerate(process.stdout, 1):
        fields = line.rstrip("\n").split("\t")
        if len(fields) != 56:
            process.kill()
            raise RuntimeError(f"unexpected field count on MySQL row {line_number}: {len(fields)}")
        yield fields
    if process.wait() != 0:
        raise RuntimeError("reference database query failed")


def sql_text(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


def emit_insert(output, table: str, columns: str, rows, batch_size: int = 500) -> None:
    batch = []
    for row in rows:
        batch.append("(" + ",".join(row) + ")")
        if len(batch) == batch_size:
            output.write(f"INSERT INTO `{table}` ({columns}) VALUES\n")
            output.write(",\n".join(batch) + ";\n")
            batch.clear()
    if batch:
        output.write(f"INSERT INTO `{table}` ({columns}) VALUES\n")
        output.write(",\n".join(batch) + ";\n")


def main() -> int:
    args = arguments()
    supported_spells = dbc_ids(args.wotlk_spell_dbc)
    items = []
    stats = []
    spells = []
    skipped_spells = set()

    for fields in mysql_rows(args):
        entry = fields[0]
        source_stats = fields[1:21]
        source_spells = fields[21:56]
        compact_stats = []
        for index in range(10):
            stat_type, stat_value = source_stats[index * 2:index * 2 + 2]
            if int(stat_value) != 0:
                compact_stats.append((stat_type, stat_value))

        items.append((sql_text(args.profile), entry, str(len(compact_stats))))
        for slot, (stat_type, stat_value) in enumerate(compact_stats, 1):
            stats.append((sql_text(args.profile), entry, str(slot), stat_type, stat_value))
        for slot in range(5):
            values = source_spells[slot * 7:slot * 7 + 7]
            # Itemization bonuses are passive on-equip spells (trigger 1).
            # Leave consumables, teaching items, and on-hit effects on the
            # WotLK baseline; those belong to later gameplay-fidelity layers.
            if int(values[0]) != 0 and int(values[1]) == 1 and int(values[0]) in supported_spells:
                spells.append((sql_text(args.profile), entry, str(slot + 1), *values))
            elif int(values[0]) != 0 and int(values[1]) == 1:
                skipped_spells.add(int(values[0]))

    source_name = "VMaNGOS" if args.profile == "vanilla" else "TBC-DB"
    source_build = "5875" if args.profile == "vanilla" else "8606"
    args.output.parent.mkdir(parents=True, exist_ok=True)
    with args.output.open("w", encoding="utf-8", newline="\n") as output:
        output.write("-- Generated by tools/generate_profile.py; do not edit by hand.\n")
        output.write("START TRANSACTION;\n")
        output.write(f"DELETE FROM `retro_itemization_profile` WHERE `profile`={sql_text(args.profile)};\n")
        output.write(
            "INSERT INTO `retro_itemization_profile` "
            "(`profile`,`source_name`,`source_build`,`source_revision`,`expected_items`) VALUES "
            f"({sql_text(args.profile)},{sql_text(source_name)},{source_build},"
            f"{sql_text(args.source_revision)},{len(items)});\n"
        )
        emit_insert(output, "retro_itemization_item", "`profile`,`entry`,`stats_count`", items)
        emit_insert(
            output, "retro_itemization_stat",
            "`profile`,`entry`,`stat_slot`,`stat_type`,`stat_value`", stats,
        )
        emit_insert(
            output, "retro_itemization_spell",
            "`profile`,`entry`,`spell_slot`,`spell_id`,`spell_trigger`,`spell_charges`,"
            "`spell_ppm_rate`,`spell_cooldown`,`spell_category`,`spell_category_cooldown`",
            spells,
        )
        output.write("COMMIT;\n")

    print(f"generated {args.profile}: {len(items)} items, {len(stats)} stats, {len(spells)} spell slots")
    if skipped_spells:
        print(f"skipped {len(skipped_spells)} on-equip spell IDs absent from the WotLK DBC: " +
              ",".join(str(spell_id) for spell_id in sorted(skipped_spells)))
    print(args.output)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, RuntimeError, ValueError) as error:
        print(f"error: {error}", file=sys.stderr)
        sys.exit(1)
