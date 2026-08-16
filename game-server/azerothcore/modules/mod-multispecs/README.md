# mod-multispecs

Configurable automatic dual and triple specialization for the ToCloud9
AzerothCore 3.3.5a server and its patched-client distribution.

## Features

- Leaves dual spec as the stock paid trainer purchase, available at the native
  `MinDualSpecLevel`.
- Automatically unlocks triple spec at `Multispecs.TripleSpecLevel`.
- Persists talents, glyphs, and action bars through AzerothCore's native talent
  group storage.
- Adds `.multispec switch 1|2|3` and `.multispec status` player commands.
- Adds a third native-style specialization tab to Blizzard's talent window,
  matching the first two tabs and using the existing activation controls.

Client-owned files are stored under `client/Interface/AddOns/SWPMultispecs`,
following the same layout as `mod-instances-difficulties`. Reserved `dbc/` and
`patch/` directories provide stable discovery points for future launcher
backend support.

The QoL world migration controls the dual-spec trainer price, while the native
`MinDualSpecLevel` setting controls when it can be purchased. The core patch is
required because the 3.3.5a server normally caps talent groups at two. It also
generalizes action-bar copying when more than one group is added at once.

## Installation

Copy this directory to `modules/mod-multispecs`, apply
`core/mod-multispecs-core.patch` from the AzerothCore root, rebuild, and copy
`conf/mod_multispecs.conf.dist` into the installed module configuration folder.
The ToCloud9 Dockerfile and launcher are already wired to do this.

Changing the configured levels never deletes an existing character's talent
groups. This prevents accidental talent, glyph, or action-bar loss after a
configuration reload.
