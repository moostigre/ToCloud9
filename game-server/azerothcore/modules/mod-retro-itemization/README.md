# mod-retro-itemization

Expansion-specific itemization profiles for an AzerothCore 3.3.5a server.

The module stores complete Vanilla, TBC, and WotLK snapshots side by side. At
worldserver startup it replaces the compacted base-stat array and all five item
spell slots. Complete slots prevent values from a previously selected expansion
from surviving an era switch.

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
- complete item-spell replacement, including empty slots and cooldown data
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
  --output /data/ToCloud9/retro-itemization/generated/vanilla.sql

python3 tools/generate_profile.py \
  --profile tbc --source-revision a38dbd07f5ee604162507119b6c890dac5d01e05 \
  --output /data/ToCloud9/retro-itemization/generated/tbc.sql

python3 tools/generate_profile.py \
  --profile wotlk --source-revision pinned \
  --output /data/ToCloud9/retro-itemization/generated/wotlk.sql
```

Import all three SQL files. Switching `RetroItemization.Profile` and restarting
then supports `Vanilla -> TBC -> WotLK` without stale values.

## Patch item_template directly

For physical `item_template` changes, import all three profiles and run:

```bash
python3 tools/apply_profile.py --container ac-database --database acore_world --profile tbc
python3 tools/apply_profile.py --container ac-database --database acore_world --profile vanilla
python3 tools/apply_profile.py --container ac-database --database acore_world --profile wotlk
```

Each operation is transactional. The WotLK profile is the rollback snapshot;
restart worldserver after applying a profile.

## Generate reversible client fixes

```bash
python3 tools/build_client_patch.py --profile tbc \
  --historical-dbc /reference/tbc/DBFilesClient/Spell.dbc \
  --profile-sql /generated/tbc.sql \
  --wotlk-dbc /reference/wotlk/DBFilesClient/Spell.dbc \
  --server-dbc-output /generated/server-dbc/tbc/Spell.dbc \
  --output /generated/patch-T-tbc.MPQ

python3 tools/build_client_patch.py --profile wotlk \
  --wotlk-dbc /reference/wotlk/DBFilesClient/Spell.dbc \
  --output /generated/patch-T-wotlk.MPQ
```

Use the same command with `--profile vanilla` and its 1.12.1 Spell.dbc. The
WotLK patch is a clean baseline, so the launcher can select exactly one era.
Legacy healing auras are translated to WotLK's separate healing and damage
effects; spell 18033 becomes 46 healing / 16 damage and receives the combined
historical tooltip used by Band of Halos (item 29373).

Install the file written by `--server-dbc-output` in the selected world's DBC
directory as well as selecting the matching client MPQ. This keeps gameplay
effects and client tooltips on the same profile.

## Installation

Apply `data/sql/db-world/base/mod_retro_itemization.sql` to the world database,
copy the distributed configuration to the server's module configuration
directory, configure the desired profile, compile, and restart worldserver.

Do not enable `Vanilla` or `TBC` until an importer has populated a complete
reviewed profile. With `StrictData = 1`, an empty or incomplete profile stops
startup rather than silently mixing expansion values.

The three profiles coexist in the same world database. One
worldserver process can load only one profile. Simultaneous Vanilla and TBC
realms require separate worldserver processes configured for their respective
profiles. Client DBC selection occurs in the launcher before WoW starts.
