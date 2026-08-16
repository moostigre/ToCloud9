# AzerothCore requirements

`mod-multispecs` currently requires `core/mod-multispecs-core.patch`. The
3.3.5 client exposes only two native talent groups, while this module stores up
to four server-side groups. Public script hooks alone cannot safely change the
core-owned group limit and the code paths that persist action bars, learn and
reset talents, and activate a group.

The patch currently changes these AzerothCore areas:

- `SharedDefines.h`: raises `MAX_TALENT_SPECS` from two to four;
- `PlayerUpdates.cpp`: initializes action bars for every newly added group;
- `SkillHandler.cpp`: derives ranks from the authoritative active server group
  for groups that the client aliases to one of its two native buffers;
- `Player.cpp`: resets talents from the authoritative talent mask and removes
  stale talent auras when activating another group.

The `TC9GrpcHandler.cpp` hunk is ToCloud9 compatibility glue for a renamed
money error enum. It is not a multispec requirement and must not be included in
an upstream AzerothCore pull request.

## Upstream path

An AzerothCore PR is preferable to carrying a revision-sensitive patch. It
should propose a configurable server-side spec limit plus generic core APIs or
hooks for initializing a new spec, resolving a talent rank for the active
spec, resetting the active spec, and completing spec activation. The default
must remain two so existing servers retain stock behavior. The action-bar,
talent-mask, and stale-aura corrections can also be proposed separately as
core bug fixes with regression tests.

Until those facilities are accepted upstream, pin the AzerothCore revision,
apply the patch before compiling, and fail the image build if `git apply
--check` does not succeed.
