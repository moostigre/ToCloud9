#!/usr/bin/env python3
"""Apply an enforcement manifest to native 3.3.5 talent/spellbook DBCs."""

import argparse
import json
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
    records = [data[20 + index * record_size:20 + (index + 1) * record_size] for index in range(count)]
    return fields, records, data[end:]


def write_dbc(path, fields, records, strings):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    header = struct.pack("<4s4I", b"WDBC", len(records), fields, fields * 4, len(strings))
    path.write_bytes(header + b"".join(records) + strings)


def record_field(record, index):
    return struct.unpack_from("<I", record, index * 4)[0]


def packed_record(fields, values):
    if len(values) != fields:
        raise ValueError(f"record has {len(values)} fields, expected {fields}")
    return struct.pack(f"<{fields}I", *values)


def dbc_ids(path):
    fields, records, _ = read_dbc(path)
    return fields, {record_field(row, 0) for row in records}


def convert_tbc_talents(historical, available_spells):
    fields, rows, _ = read_dbc(historical / "Talent.dbc")
    if fields != 21:
        raise ValueError(f"historical Talent.dbc must use the TBC layout, got {fields} fields")

    candidates = {}
    missing_spells = {}
    for row in rows:
        values = struct.unpack("<21I", row)
        talent_id = values[0]
        missing = sorted(spell for spell in values[4:9] if spell and spell not in available_spells)
        if missing:
            missing_spells[talent_id] = missing
        else:
            candidates[talent_id] = values

    removed_dependencies = {}
    changed = True
    while changed:
        changed = False
        for talent_id, values in list(candidates.items()):
            dependency = values[13]
            if dependency and dependency not in candidates:
                removed_dependencies[talent_id] = dependency
                del candidates[talent_id]
                changed = True

    # GetNumTalents/GetTalentInfo enumerate a tree in physical DBC record
    # order.  TBC groups records by class/tree and lays each tree out in its
    # intended tier/column order; sorting by TalentID collapses the 3.3.5 UI
    # into an apparently empty tree.  Filter in source order instead.
    converted = []
    for row in rows:
        values = struct.unpack("<21I", row)
        if values[0] in candidates:
            converted.append(packed_record(23, (*values, 0, 0)))
    return converted, missing_spells, removed_dependencies


def convert_tbc_talent_tabs(historical):
    fields, rows, strings = read_dbc(historical / "TalentTab.dbc")
    if fields != 15:
        raise ValueError(f"historical TalentTab.dbc must use the TBC layout, got {fields} fields")

    converted = []
    for row in rows:
        values = struct.unpack("<15I", row)
        converted.append(packed_record(24, (
            values[0],                 # 0: TalentTabID
            *values[1:9],              # 1-8: TBC localized names
            *([0] * 8),                # 9-16: added WotLK locales
            values[9],                 # 17: localized-name flags
            values[10],                # 18: spell icon
            values[11],                # 19: race mask
            values[12],                # 20: class mask
            0,                          # 21: WotLK pet-talent mask
            values[13],                # 22: tree page/order
            values[14],                # 23: background filename
        )))
    return converted, strings


def _copy_localized_group(source_values, source_strings, source_start,
                          target_values, target_strings, target_start):
    for locale in range(8):
        offset = source_values[source_start + locale]
        if not offset:
            continue
        end = source_strings.find(b"\0", offset)
        value = source_strings[offset:end if end >= 0 else len(source_strings)]
        target_values[target_start + locale] = len(target_strings)
        target_strings.extend(value + b"\0")


def _convert_tbc_spell(source_values, source_strings, target_strings):
    """Translate a raw 2.4.3 Spell.dbc row to the 3.3.5 layout."""
    target = [0] * 234
    target[0] = source_values[0]

    # Scalar header fields. TBC still stores School as an enum in field 1;
    # Wrath stores its bit mask near the end of the record instead.
    target[1:10] = source_values[2:11]
    target[12] = source_values[12]
    target[14] = source_values[13]
    target[16:19] = source_values[14:17]
    target[20:24] = source_values[17:21]
    target[28:45] = source_values[21:38]
    target[45] = source_values[38]
    target[46:68] = source_values[39:61]
    target[68:71] = source_values[61:64]

    # EffectBaseDice and EffectDicePerLevel disappeared in Wrath. The
    # effective historical amount is represented by EffectBasePoints; all
    # other effect arrays retain their semantic meaning.
    for source_start, target_start in (
        (64, 71), (67, 74), (76, 77), (79, 80), (82, 83),
        (85, 86), (88, 89), (91, 92), (94, 95), (97, 98),
        (100, 101), (103, 104), (106, 107), (109, 110),
        (112, 116), (115, 119),
    ):
        target[target_start:target_start + 3] = source_values[source_start:source_start + 3]

    target[131:136] = source_values[118:123]
    for source_start, target_start in ((123, 136), (132, 153), (141, 170), (150, 187)):
        _copy_localized_group(source_values, source_strings, source_start,
                              target, target_strings, target_start)
    target[152] = source_values[131]
    target[169] = source_values[140]
    target[186] = source_values[149]
    target[203] = source_values[158]

    target[204:211] = source_values[159:166]
    target[212:225] = source_values[166:179]
    school = source_values[1]
    target[225] = (1 << school) if school < 32 else 0
    return packed_record(234, target)


def restore_tbc_talent_spells(historical, spell_path, talents):
    """Port complete TBC talent spell ranks and their trigger dependencies."""
    source_fields, source_rows, source_strings = read_dbc(historical / "Spell.dbc")
    target_fields, target_rows, target_strings = read_dbc(spell_path)
    target_strings = bytearray(target_strings)
    if source_fields != 179 or target_fields != 234:
        raise ValueError(
            f"expected TBC/WotLK Spell.dbc layouts (179/234), got "
            f"{source_fields}/{target_fields}"
        )

    wanted = set()
    for row in talents:
        values = struct.unpack("<23I", row)
        wanted.update(spell for spell in values[4:9] if spell)

    source = {record_field(row, 0): row for row in source_rows}
    # Passive talents can trigger helper spells. Port those recursively when
    # the historical client contains them so no restored rank points at a
    # spell record which is absent from the Wrath client.
    pending = list(wanted)
    while pending:
        spell_id = pending.pop()
        row = source.get(spell_id)
        if row is None:
            continue
        values = struct.unpack("<179I", row)
        for trigger in values[112:115]:
            if trigger and trigger in source and trigger not in wanted:
                wanted.add(trigger)
                pending.append(trigger)

    target = {record_field(row, 0): row for row in target_rows}
    imported = 0
    converted = 0
    for spell_id in sorted(wanted):
        row = source.get(spell_id)
        if row is None:
            continue
        imported += spell_id not in target
        target[spell_id] = _convert_tbc_spell(
            struct.unpack("<179I", row), source_strings, target_strings
        )
        converted += 1

    # Preserve the original client ordering and append genuinely historical
    # records deterministically.
    original_ids = [record_field(row, 0) for row in target_rows]
    patched_rows = [target[spell_id] for spell_id in original_ids]
    patched_rows.extend(target[spell_id] for spell_id in sorted(wanted)
                        if spell_id not in set(original_ids) and spell_id in target)
    write_dbc(spell_path, target_fields, patched_rows, target_strings)
    return converted, imported


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--input-dbc-dir", required=True)
    parser.add_argument("--fallback-dbc-dir")
    parser.add_argument("--historical-dbc-dir")
    parser.add_argument("--compatibility-report")
    parser.add_argument("--output-dbc-dir", required=True)
    args = parser.parse_args()

    manifest = json.loads(Path(args.manifest).read_text(encoding="utf-8"))
    blocked_spells = set(manifest["blocked_spells"])
    blocked_talents = set(manifest["blocked_talents"])
    source = Path(args.input_dbc_dir)
    fallback = Path(args.fallback_dbc_dir) if args.fallback_dbc_dir else source
    output = Path(args.output_dbc_dir)

    if args.historical_dbc_dir:
        historical = Path(args.historical_dbc_dir)
        spell_source = source / "Spell.dbc"
        if not spell_source.is_file():
            spell_source = fallback / "Spell.dbc"
        spell_fields, available_spells = dbc_ids(spell_source)
        if spell_fields != 234:
            raise ValueError(f"Spell.dbc must use the 3.3.5 layout, got {spell_fields} fields")

        _, historical_spells = dbc_ids(historical / "Spell.dbc")
        talents, missing_spells, removed_dependencies = convert_tbc_talents(
            historical, available_spells | historical_spells
        )
        write_dbc(output / "Talent.dbc", 23, talents, b"\0")
        tabs, tab_strings = convert_tbc_talent_tabs(historical)
        write_dbc(output / "TalentTab.dbc", 24, tabs, tab_strings)
        restored_spells, imported_spells = restore_tbc_talent_spells(
            historical, spell_source, talents
        )

        report = {
            "retained_talents": len(talents),
            "rejected_talents_missing_spells": {
                str(key): value for key, value in sorted(missing_spells.items())
            },
            "rejected_talents_missing_prerequisite": {
                str(key): value for key, value in sorted(removed_dependencies.items())
            },
            "restored_tbc_talent_spells": restored_spells,
            "imported_tbc_talent_spells": imported_spells,
        }
        if args.compatibility_report:
            report_path = Path(args.compatibility_report)
            report_path.parent.mkdir(parents=True, exist_ok=True)
            report_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
        print(f"native TBC talent conversion: retained {len(talents)} talents; "
              f"rejected {len(missing_spells)} for missing spells and "
              f"{len(removed_dependencies)} for missing prerequisites; "
              f"restored {restored_spells} talent/helper spells "
              f"({imported_spells} historical records imported)")
        talent_summary = f"Talent {len(talents)}"
    else:
        talent_source = source / "Talent.dbc"
        if not talent_source.is_file():
            talent_source = fallback / "Talent.dbc"
        fields, talents, strings = read_dbc(talent_source)
        if fields != 23:
            raise ValueError(f"Talent.dbc must use the 3.3.5 layout, got {fields} fields")
        original = len(talents)
        talents = [row for row in talents if record_field(row, 0) not in blocked_talents]
        write_dbc(output / "Talent.dbc", fields, talents, strings)
        talent_summary = f"Talent {original}->{len(talents)}"

    ability_source = source / "SkillLineAbility.dbc"
    if not ability_source.is_file():
        ability_source = fallback / "SkillLineAbility.dbc"
    fields, abilities, strings = read_dbc(ability_source)
    if fields != 14:
        raise ValueError(f"SkillLineAbility.dbc must use the 3.3.5 layout, got {fields} fields")
    original_abilities = len(abilities)
    abilities = [record for record in abilities if record_field(record, 2) not in blocked_spells]
    write_dbc(output / "SkillLineAbility.dbc", fields, abilities, strings)

    print(f"native DBC filter: {talent_summary}, SkillLineAbility {original_abilities}->{len(abilities)}")


if __name__ == "__main__":
    main()
