#!/usr/bin/env python3
"""Port historical passive item-spell records into a 3.3.5a Spell.dbc."""

import argparse
import math
import re
import struct
from pathlib import Path


def read_dbc(path):
    data = Path(path).read_bytes()
    magic, count, fields, record_size, string_size = struct.unpack_from("<4s4I", data)
    if magic != b"WDBC" or record_size != fields * 4:
        raise ValueError(f"invalid DBC: {path}")
    end = 20 + count * record_size
    if end + string_size != len(data):
        raise ValueError(f"invalid DBC dimensions: {path}")
    rows = [bytearray(data[20 + i * record_size:20 + (i + 1) * record_size]) for i in range(count)]
    return fields, rows, bytearray(data[end:])


def field(row, index):
    return struct.unpack_from("<I", row, index * 4)[0]


def set_field(row, index, value):
    struct.pack_into("<I", row, index * 4, value)


def string_at(strings, offset):
    if not offset or offset >= len(strings):
        return b""
    end = strings.find(b"\0", offset)
    return bytes(strings[offset:end if end >= 0 else len(strings)])


def item_spell_ids(sql_path, profile):
    sql = Path(sql_path).read_text(encoding="utf-8")
    # Only on-equip passive spells are safe and relevant to item stat tooltips.
    pattern = re.compile(rf"\('{re.escape(profile)}',\d+,\d+,(\d+),1,")
    return {int(match.group(1)) for match in pattern.finditer(sql)}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--profile", choices=("vanilla", "tbc", "wotlk"), required=True)
    parser.add_argument("--historical-dbc")
    parser.add_argument("--profile-sql")
    parser.add_argument("--input", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()

    if args.profile == "wotlk":
        Path(args.output).write_bytes(Path(args.input).read_bytes())
        print("generated reversible WotLK client baseline")
        return
    if not args.historical_dbc or not args.profile_sql:
        parser.error("--historical-dbc and --profile-sql are required for Vanilla/TBC")

    source_fields, source_rows, source_strings = read_dbc(args.historical_dbc)
    target_fields, target_rows, target_strings = read_dbc(args.input)
    expected_source = 162 if args.profile == "vanilla" else 179
    if source_fields != expected_source or target_fields != 234:
        raise ValueError(
            f"expected {args.profile}/WotLK Spell.dbc layouts "
            f"({expected_source}/234), got {source_fields}/{target_fields}"
        )

    wanted = item_spell_ids(args.profile_sql, args.profile)
    source = {field(row, 0): row for row in source_rows}
    target = {field(row, 0): row for row in target_rows}

    # Historical physical fields -> WotLK 3.3.5 physical fields. BaseDice and
    # DicePerLevel disappeared in WotLK; BasePoints contains the effective
    # historical amount used by passive aura records.
    tbc_groups = (
        (64, 71),   # Effect
        (67, 74),   # EffectDieSides
        (76, 77),   # EffectRealPointsPerLevel
        (79, 80),   # EffectBasePoints
        (82, 83),   # EffectMechanic
        (85, 86),   # EffectImplicitTargetA
        (88, 89),   # EffectImplicitTargetB
        (91, 92),   # EffectRadiusIndex
        (94, 95),   # EffectApplyAuraName
        (97, 98),   # EffectAmplitude
        (100, 101), # EffectValueMultiplier
        (103, 104), # EffectChainTarget
        (106, 107), # EffectItemType
        (109, 110), # EffectMiscValue
        (112, 113), # EffectMiscValueB
        (115, 116), # EffectTriggerSpell
        (118, 119), # EffectPointsPerComboPoint
    )
    vanilla_groups = (
        (56, 71), (59, 74), (68, 77), (71, 80), (74, 83),
        (77, 86), (80, 92), (83, 95), (86, 98), (89, 101),
        (92, 104), (95, 107), (98, 110), (101, 116), (104, 119),
    )
    groups = vanilla_groups if args.profile == "vanilla" else tbc_groups
    text_groups = (
        ((112, 136), (121, 153), (130, 170), (139, 187))
        if args.profile == "vanilla" else
        ((123, 136), (132, 153), (141, 170), (150, 187))
    )

    patched = 0
    missing = []
    for spell_id in sorted(wanted):
        src, dst = source.get(spell_id), target.get(spell_id)
        if src is None:
            missing.append(spell_id)
            continue
        if dst is None:
            dst = bytearray(target_fields * 4)
            set_field(dst, 0, spell_id)
            target_rows.append(dst)
            target[spell_id] = dst
        for src_start, dst_start in groups:
            for effect in range(3):
                set_field(dst, dst_start + effect, field(src, src_start + effect))
        for src_start, dst_start in text_groups:
            for locale in range(8):
                value = string_at(source_strings, field(src, src_start + locale))
                if value:
                    offset = len(target_strings)
                    target_strings.extend(value + b"\0")
                    set_field(dst, dst_start + locale, offset)

        # Pre-WotLK aura 135 carried an implicit spell-damage component equal
        # to one third of its healing amount. WotLK split that into aura 135
        # (healing) and aura 13 (damage), so a literal field copy silently
        # produces healing-only items such as Band of Halos (29373).
        for effect in range(3):
            if field(dst, 71 + effect) == 6 and field(dst, 95 + effect) == 135:
                healing = field(dst, 80 + effect) + 1
                free_effect = next(
                    (index for index in range(3) if field(dst, 71 + index) == 0), None
                )
                if free_effect is not None:
                    damage = math.ceil(healing / 3)
                    set_field(dst, 71 + free_effect, 6)
                    set_field(dst, 74 + free_effect, 1)
                    set_field(dst, 80 + free_effect, damage - 1)
                    set_field(dst, 86 + free_effect, 1)
                    set_field(dst, 95 + free_effect, 13)
                    set_field(dst, 110 + free_effect, field(dst, 110 + effect))
                    description = (
                        b"Increases healing done by up to $s1 and damage done by up to $s2 "
                        b"for all magical spells and effects."
                    )
                    offset = len(target_strings)
                    target_strings.extend(description + b"\0")
                    set_field(dst, 170, offset)
        patched += 1

    body = b"".join(target_rows)
    header = struct.pack("<4s4I", b"WDBC", len(target_rows), target_fields,
                         target_fields * 4, len(target_strings))
    Path(args.output).write_bytes(header + body + target_strings)
    print(f"patched {patched} {args.profile} passive item spells; "
          f"{len(missing)} unavailable source records skipped")
    if 34495 in target:
        row = target[34495]
        print(f"spell 34495: effects={[field(row, 71+i) for i in range(3)]} "
              f"auras={[field(row, 95+i) for i in range(3)]} "
              f"basepoints={[field(row, 80+i) for i in range(3)]}")


if __name__ == "__main__":
    main()
