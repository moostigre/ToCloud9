# mod-multispecs

Configurable purchased dual and character-bound triple specialization for the ToCloud9
AzerothCore 3.3.5a server and its patched-client distribution.

## Features

- Lets each character purchase dual spec at `Multispecs.DualSpecLevel` for
  `Multispecs.DualSpecPriceGold` (level 10 and 50 gold by default).
- Unlocks triple spec at `Multispecs.TripleSpecLevel` when that character owns
  dual spec and has the character-bound website-shop entitlement.
- Persists talents, glyphs, and action bars through AzerothCore's native talent
  group storage.
- Adds `.multispec switch 1|2|3`, `.multispec buydual`,
  `.multispec buytriple`, and `.multispec status` player commands.
- Adds a third native-style specialization tab to Blizzard's talent window,
matching the first two tabs and using the existing activation controls.
- Adds an in-client purchase confirmation for dual spec and a website-shop
  status button for triple spec.
- Grants all three slots to GM/admin accounts when `Multispecs.AdminUnlockAll`
  is enabled, allowing PTR testing without fabricated shop transactions.

Client-owned files are stored under `client/Interface/AddOns/SWPMultispecs`,
following the same layout as `mod-instances-difficulties`. Reserved `dbc/` and
`patch/` directories provide stable discovery points for future launcher
backend support.

The core patch is required because the 3.3.5a server normally caps talent
groups at two. It also generalizes action-bar copying when more than one group
is added at once.

## Installation

Copy this directory to `modules/mod-multispecs`, apply
`core/mod-multispecs-core.patch` from the AzerothCore root, rebuild, and copy
`conf/mod_multispecs.conf.dist` into the installed module configuration folder.
The ToCloud9 Dockerfile and launcher are already wired to do this.

Changing gates or revoking an entitlement never deletes stored talent groups.
An unauthorized active group is switched back to the primary group, and the
stored talents become usable again if the gate is restored.

## Shop entitlement and testing

The website shop grants triple spec to the character selected during checkout
by upserting its AzerothCore character GUID in the characters database:

```sql
INSERT INTO character_multispec_entitlement
    (guid, triple_spec, granted_at, source)
VALUES
    (CHARACTER_GUID_HERE, 1, NOW(), 'website-shop')
ON DUPLICATE KEY UPDATE
    triple_spec = VALUES(triple_spec),
    granted_at = VALUES(granted_at),
    source = VALUES(source);
```

To simulate revocation:

```sql
UPDATE character_multispec_entitlement
SET triple_spec = 0
WHERE guid = CHARACTER_GUID_HERE;
```

Run `.multispec status` (or reopen/log into the client) after changing the row.
Each character must still purchase dual spec before its third group becomes
available. For test setup, a character-specific dual purchase can be simulated
with:

```sql
INSERT INTO character_multispec_unlock (guid, dual_spec, purchased_at)
VALUES (CHARACTER_GUID_HERE, 1, NOW())
ON DUPLICATE KEY UPDATE dual_spec = 1, purchased_at = NOW();
```
