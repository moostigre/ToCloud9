# mod-retro-itemization

Expansion-specific itemization profiles for an AzerothCore 3.3.5a server.

The module preserves AzerothCore's WotLK `item_template` as its canonical
baseline. At worldserver startup it can replace the compacted base-stat array
and historical passive on-equip spell slots of selected items with an imported
Vanilla or TBC profile. WotLK remains the default and performs no database
queries or template mutation.

The 3.3.5a core and client protocol retain the legacy item stat identifiers:

- `41`: spell healing done
- `42`: spell damage done
- `45`: unified WotLK spell power

The supplied historical databases do not encode their healing and spell-damage
bonuses as stat types 41 and 42. They primarily use passive on-equip spells in
the five `spellid_*` slots. Restoring only the stat columns would therefore
produce incorrect caster items.

## Current implementation

- `RetroItemization.Profile`: `Vanilla`, `TBC`, or `WotLK`
- exact compacted `StatsCount` and stat-slot replacement
- sparse passive on-equip spell replacement, including cooldown data
- complete dataset validation before template mutation
- imported item-count verification against profile metadata
- strict startup failure for incomplete or invalid imported data
- WotLK-safe disabled defaults

The included generator reads numeric item data from isolated reference schemas,
selects the last available VMaNGOS patch row for each Vanilla entry, intersects
the result with AzerothCore's 3.3.5 item IDs, and emits transactional profile
SQL. Later profile layers will cover weapon damage, armor, gems, enchants,
random properties, and set bonuses, with matching launcher-generated client
patches.

## Generate profiles

The development reference container uses these default schemas:

- `retro_ref_vanilla`: VMaNGOS `db_latest`
- `retro_ref_tbc`: TBC-DB
- `retro_ref_wotlk`: the pinned AzerothCore `item_template.sql`

Generate large artifacts on the data partition:

```bash
python3 tools/generate_profile.py \
  --profile vanilla --source-revision e65e48e \
  --wotlk-spell-dbc /data/tc9-gameserver-data/dbc/Spell.dbc \
  --output /data/ToCloud9/retro-itemization/generated/vanilla.sql

python3 tools/generate_profile.py \
  --profile tbc --source-revision a38dbd07f5ee604162507119b6c890dac5d01e05 \
  --wotlk-spell-dbc /data/tc9-gameserver-data/dbc/Spell.dbc \
  --output /data/ToCloud9/retro-itemization/generated/tbc.sql
```

## Installation

Apply `data/sql/db-world/base/mod_retro_itemization.sql` to the world database,
copy the distributed configuration to the server's module configuration
directory, configure the desired profile, compile, and restart worldserver.

Do not enable `Vanilla` or `TBC` until an importer has populated a complete
reviewed profile. With `StrictData = 1`, an empty or incomplete profile stops
startup rather than silently mixing expansion values.

The three profiles coexist in the same world database; configuration selects
which copy is loaded without overwriting AzerothCore's WotLK rows. One
worldserver process can load only one profile. Simultaneous Vanilla and TBC
realms require separate worldserver processes configured for their respective
profiles. Client DBC selection occurs in the launcher before WoW starts.
