# AzerothCore requirements

Most of `mod-qol` uses public script hooks. Only taxi velocity currently
requires `core/mod-qol-core.patch`, because `WaypointMovementGenerator.cpp`
has no module hook for overriding the flight spline speed.

Dual-specialization purchasing does not require a core patch. AzerothCore
already reads its minimum purchase level from the native `MinDualSpecLevel`
configuration option. The stock gossip code reads `gossip_menu_option.BoxMoney`
from the world database and uses it for both the displayed price and the amount
charged. The module's world migration sets that column to its desired default.

Without the patch, the module still compiles, but `Qol.TaxiSpeedMultiplier`
does not control flight spline speed. This should be treated as a build-time
compatibility failure rather than silently shipping a partially working image.

## Upstream path

The durable solution is an AzerothCore PR adding a generic configuration point
or hook for the velocity selected when a taxi flight is initialized. A generic
name and stock default avoid coupling AzerothCore to this module. Once that
facility is released in the pinned core revision, the module can implement it
and the patch can be removed.

Until then, pin the AzerothCore revision, apply the patch before compiling,
and fail the image build if `git apply --check` does not succeed.
