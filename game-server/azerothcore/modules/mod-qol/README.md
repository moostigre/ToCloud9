# mod-qol

A configurable quality-of-life module for AzerothCore WotLK.

## Features

- 10x taxi flight speed by default
- Reduced hearthstone cooldown
- Larger stacks for items that already stack
- Bags enlarged by 25%, rounded up, up to the WotLK limit of 36 slots
- Instant mail
- 20% out-of-combat running-speed increase with a permanent custom Sprint indicator
- Initiate Riding (50/50 Riding) automatically learned at level 10
- Cheaper mount items
- Configurable food-buff and scroll-buff durations
- Dual specialization for 5 gold from level 10

Every feature can be enabled, disabled, and configured independently in
`mod_qol.conf`.

## Compatibility

The module was developed for
[`moostigre/azerothcore-wotlk`](https://github.com/moostigre/azerothcore-wotlk)
at commit `9c80943df96e191c53dc5320d55f405bfd90d2e1`.

The C++ module is portable to closely related AzerothCore forks. The core patch
touches only taxi movement and may need to be rebased when used with a different
core revision. Dual-specialization level and pricing use the stock
`MinDualSpecLevel` setting and world database `BoxMoney` respectively. Always
compile the module against the same core revision and database schema used by
the target server.

## Installation

Place this repository at `modules/mod-qol` in the AzerothCore source tree:

```bash
cd /path/to/azerothcore-wotlk
git clone <repository-url> modules/mod-qol
git apply modules/mod-qol/core/mod-qol-core.patch
```

Configure and compile AzerothCore normally. Install the module configuration
where the server loads module configuration files:

```bash
cp modules/mod-qol/conf/mod_qol.conf.dist \
  /path/to/server/etc/modules/mod_qol.conf
```

Apply the world database migration:

```bash
mysql -u <user> -p <world_database> \
  < modules/mod-qol/data/sql/db-world/base/mod_qol.sql
```

Restart `worldserver` after installing or changing database-backed features.
Startup logs include a `mod-qol` line showing whether the module loaded and its
configured taxi and running-speed multipliers.

## Configuration

The distributed configuration documents every option. Important defaults are:

| Option | Default |
| --- | ---: |
| Taxi flight multiplier | 10x |
| Hearthstone cooldown | 15 minutes |
| Out-of-combat run multiplier | 1.20x |
| Run bonus indicator | Sprint (custom spell 80861) |
| Initiate Riding | Level 10, skill value 50 |
| PvP run-bonus lockout | 30 seconds |
| Bag slot increase | 25% |
| Food buff duration | 60 minutes |
| Scroll buff duration | 60 minutes |
| Dual specialization level (`MinDualSpecLevel`) | 10 |
| Dual specialization price (`gossip_menu_option.BoxMoney`) | 5 gold |

The stack-size change deliberately affects only items that already stack.
Making weapons, armor, quest uniques, or containers stack would violate client
and gameplay assumptions.

`Qol.FoodBuffDurationMinutes` and `Qol.ScrollBuffDurationMinutes` set the
corresponding consumable aura durations. Use `60` for one hour or `120` for two
hours. Setting either option to `0` preserves the original duration for that
category. Food and scroll spells are discovered from item data, including
their triggered spells, so the setting applies across consumable ranks without
a manually maintained spell-ID list.

Bag capacity is calculated as
`ceil(original slots × (1 + Qol.BagSlotIncreasePercent / 100))` and capped at
36 slots. For example, an 11-slot bag becomes a 14-slot bag with the default
25% increase.

The movement aura has no displayed duration and provides the configured movement
bonus through the core's normal aura system, so class abilities such as Rogue
Sprint are no longer overwritten by a periodic forced-speed update. It
disappears in combat, while mounted, on a taxi, while dead, in dungeons, raids,
battlegrounds or arenas, and during duels. PvP damage locks it for 30 seconds
for both players. Combat entry removes it immediately, and it cannot return
until the core's leave-combat event fires and the player is otherwise
eligible. Aura changes are suspended during teleports, logout, and clustered
map transfers so they cannot race player-object cleanup.

`Qol.InitiateRidingSpell` and `Qol.InitiateRidingSkillValue` control the
level-10 riding step. The default custom spell is 80860 and the default skill
value is 50. The generated tiny racial mounts require Riding 50 and are sold by
their matching racial mount vendors. Their client-visible records and the
level-10 notification are distributed by the companion launcher content patch.

## License

MIT
