# Retro itemization feasibility snapshot

Reference versions used for the initial importer:

| Profile | Source | Source rows | Entries imported into 3.3.5 |
| --- | --- | ---: | ---: |
| Vanilla | VMaNGOS `db_latest`, revision `e65e48e` | 20,034 versioned rows / 17,707 entries | 17,707 |
| TBC | TBC-DB revision `a38dbd07f5ee604162507119b6c890dac5d01e05` | 24,221 | 24,221 |
| WotLK baseline | pinned AzerothCore `item_template.sql` | 46,096 | 46,096 |

All selected historical entries exist in the WotLK baseline. Vanilla selection
uses the greatest `patch` row per entry, representing the final state supplied
by the database rather than an arbitrary early patch.

## Confirmed differences

Against the WotLK rows with matching IDs:

| Difference | Vanilla items | TBC items |
| --- | ---: | ---: |
| Any raw base-stat slot differs | 776 | 1,585 |
| Any item spell ID differs | 2,922 | 830 |
| Any spell-slot field differs | 15,688 | 1,306 |
Generated normalized data contains 11,318
Vanilla and 23,668 TBC non-zero stat rows. Passive on-equip spells are imported
sparsely; unrelated consumable, teaching, and on-hit spell slots remain on the
validated WotLK baseline. The generator also rejects historical spell IDs that
do not exist in the deployed 3.3.5 `Spell.dbc`, preventing invalid server/client
references until a later spell-porting layer supplies compatible records.

## Server and client boundary

The world database can safely hold Vanilla, TBC, and WotLK values side by side
in profile-keyed tables. On startup, a worldserver process selects one profile
and mutates its in-memory `ItemTemplate` objects. The canonical WotLK
`item_template` rows stay untouched, so changing the configured profile and
restarting restores or selects another era.

Item stats themselves are sent by the server in the item query response; they
are not sourced from Vanilla/TBC `Item.dbc`. Client work is still required for
legacy spell descriptions and any historical spells absent from, or changed in,
the 3.3.5 `Spell.dbc`. Older DBC files cannot simply replace 3.3.5 files because
their record layouts differ. The launcher generator must create 3.3.5-layout
patch records and select a matching patch set before launching the client.

## Remaining fidelity layers

The first implemented layer covers base stats and compatible passive item spell slots. Exact
era fidelity additionally requires profile handling for weapon damage, armor,
resistances, sockets and gems, random properties/suffixes, enchants, item sets,
requirements, and any supporting spell definitions. These can use the same
profile-keyed design, but should be added and validated as separate layers.
