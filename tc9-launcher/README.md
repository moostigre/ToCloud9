# SWP Launcher

Windows launcher for a WoW 3.3.5a (build 12340) private-server client. It
validates a user-selected client, checks PTR authentication-server connectivity,
downloads a signed content manifest, safely installs its files, and starts
`Wow.exe`.

The Addons tab browses the dedicated Maddons Manager Lich King catalogue and
can search GitHub for maintained 3.3.5a backports. Results identify the addon
name, advertised version or source revision, and repository. The launcher only
installs ZIP archives containing a `## Interface: 30300` TOC beneath the
selected client's `Interface/AddOns` directory.

Launcher 0.4 and later also update themselves from launcher metadata in that
same signed manifest. A verified replacement executable waits for the running
launcher to exit, preserves it as a timestamped `.previous-*` backup, installs
the new version, and restarts automatically.

## Safety properties

- The launcher never requests account credentials.
- It requests no administrator privileges.
- It validates the Windows version resource as 3.3.5.12340.
- The signed manifest is verified with an Ed25519 public key embedded in the
  launcher, and every downloaded file is checked by SHA-256 and size.
- Managed SWP updates only write `Data/patch-T.MPQ` and the `SWP` addon.
  User-confirmed third-party addon installs are restricted to
  `Interface/AddOns` beneath a validated client folder.
- Different existing managed files are moved into a timestamped
  `.swp-backup` directory before installation.
- The newest rollback copy is retained; older managed-client backups,
  superseded launcher executables, and stale update-cache versions are pruned
  automatically at startup so update history cannot grow without bounds.
- The staged archive is SHA-256 verified before the atomic rename.
- Third-party addon ZIPs are accepted only from the configured HTTPS
  repositories. Traversal paths, links, native executables, oversized files,
  ZIP bombs, and addons without a 3.3.5a (`30300`) TOC are rejected. Existing
  addon directories are backed up before replacement.

Third-party addons are not covered by the signed SWP content manifest. Their
Lua executes inside the game, so the launcher always shows the selected source
and asks for confirmation before downloading and installing one.

## Build the launcher

From Linux with Go installed:

```bash
go install github.com/akavel/rsrc@latest
rsrc -manifest cmd/launcher/app.manifest -o cmd/launcher/rsrc_windows_amd64.syso
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build \
  -trimpath -ldflags="-H windowsgui -s -w" \
  -o dist/SWPLauncher.exe ./cmd/launcher
```

## Rebuild the MPQ

The reproducible DBC generator requires unmodified server-side 3.3.5a DBC
files and the `smpq` StormLib command-line utility:

```bash
./scripts/build-patch.sh /path/to/server/data/dbc /path/to/mod_qol.conf
```

The builder reads the installed `mod-qol` and `mod-gemstones` configurations,
validates their `client/dbc/requirements.json` declarations, generates all
record-level changes into one `patch-T.MPQ`, and then adds any MPQ-ready files
found below an installed module's `client/patch/` directory. Duplicate paths
fail the build instead of silently replacing another module's asset.

Modules may also expose selectable trees below
`client/features/<feature>/patch/`. `mod-retro-client` provides selectable
Vanilla, TBC, and stock WotLK login presentation. Configure it in
`modules/mod-retro-client/conf/mod_retro_client.conf` before rebuilding:

```ini
RetroClient.Enable = 1
RetroClient.Expansion = TBC
RetroClient.LoginScreen = 1
RetroClient.Logo = 1
RetroClient.RedButtons = 1
RetroClient.TbcTalentUI = 0
RetroClient.TbcNativeTalentTrees = 0
```

The native talent-tree option must only be enabled when the matching generated
`Spell.dbc`, `Talent.dbc`, and `TalentTab.dbc` are deployed to the worldserver.
Its compatibility report records the historical talent and helper-spell data
ported into the 3.3.5 layouts.

Set `RetroClient.Expansion = Vanilla` for the original Dark Portal and logo,
or `RetroClient.Expansion = WotLK` to omit the retro login overrides.

Changing a feature to `0`, rebuilding, and publishing replaces the managed
`Data/patch-T.MPQ` with an archive that omits that override, thereby unpatching
the feature on the next launcher content update.

The launcher deliberately manages one global `Data/patch-T.MPQ`. The selected
server itemization profile is the source of truth; clients do not choose a
profile per realm. Generate reviewable artifacts for every supported profile
with:

```bash
./scripts/build-profile-patches.sh /path/to/server/data/dbc \
  /data/ToCloud9/retro-itemization/client-patches
```

Before a global profile rollout, install the matching generated artifact as
`assets/patch-T.MPQ`, publish a signed content version, and deploy the same
profile in `mod_retro_itemization.conf`. The launcher replaces the one managed
MPQ for every client. Item stat lines are supplied by the server item-query
response, while the launcher clears `itemcache.wdb` before launch so native
tooltips cannot retain values from the previous profile.

Before publishing, `scripts/stage-client.sh` discovers module
`client/merge-into-swp.txt` declarations and merges their listed interface
files into the single `Interface/AddOns/SWP` addon. The signed manifest is
generated from this staged tree, so newly declared module client files are
included automatically rather than requiring another hardcoded manifest entry.

It clones existing records and reuses the existing model and icon assets.
`patch-T.MPQ` contains modified `Spell.dbc`, `Item.dbc`, `SkillLine.dbc`,
`SkillLineAbility.dbc`, `SkillRaceClassInfo.dbc`, and `MapDifficulty.dbc`.
The latter adds client Heroic records for RFC, WC, and SFK so their portals
use the stock Heroic skull presentation. It intentionally does not replace Blizzard FrameXML
files. The reduced mount scale is configured by the matching server
`creature_template_model` row.
Rebuild and publish the patch whenever `Qol.OutOfCombatRunSpeed` changes so the
client tooltip remains synchronized with the server value.

## Generate and publish a launcher update

The launcher does not discover files by browsing the download server. It polls
the signed `manifest.json` at `ManifestURL`, compares every managed file's size
and SHA-256 digest with the local client, downloads mismatches, verifies them,
and then installs them. The manifest is therefore the release switch: copying a
file into the download directory without generating a new manifest will not
update clients.

The release pipeline is:

1. Update the source assets. Base addon files live in `client/SWP`; module-owned
   addon files are declared by the module's `client/merge-into-swp.txt` file.
2. Rebuild `assets/patch-T.MPQ` with `scripts/build-patch.sh` if DBC or MPQ
   content changed. Lua-only addon changes do not require rebuilding the MPQ.
3. Build `dist/SWPLauncher.exe` if launcher code or `LauncherVersion` changed.
   Content-only releases may reuse the existing executable.
4. Choose a monotonically increasing content version and run
   `scripts/publish-content.sh`. It calls `scripts/stage-client.sh`, merges all
   declared module Lua files into `Interface/AddOns/SWP`, copies the MPQ and
   launcher executable, calculates file metadata, and signs the manifest with
   Ed25519.
5. Deploy the generated directory to the path served at
   `/downloads/swp`. Keep the previous directory as a rollback copy.
6. Publish directly into `/srv/tc9-launcher-downloads/swp`. Nginx serves this
   allowlisted directory as static files; the disabled account-services
   application is not part of the launcher release path.
7. Fetch the public manifest and each changed public URL. Confirm the manifest
   version, file size, and SHA-256 all match before announcing the release.

Generate a signed release with an Ed25519 private key kept outside the
repository:

```bash
./scripts/publish-content.sh 2026.08.09.18 \
  /secure/path/launcher-signing-key.pem \
  /tmp/swp-release-2026.08.09.18
```

Production example on the current server (replace the version every time):

```bash
./scripts/publish-content.sh 2026.08.09.18 \
  /root/.tc9-launcher-signing-key.pem \
  /srv/tc9-launcher-downloads/swp.release-20260809-18

# After inspecting the generated manifest and retaining the old `swp`
# directory as a backup, atomically rename the release to
# /srv/tc9-launcher-downloads/swp. No application restart is required.
```

Do not commit the signing key. The public key embedded in
`internal/client/updater.go` must correspond to it. A launcher executable
update additionally requires increasing `LauncherVersion` in
`internal/client/selfupdate.go`; the content version and launcher version are
independent.

Useful release checks:

```bash
# Decode the signed payload for inspection (verification still happens in the launcher).
curl -fsS https://launcher.expanded.space/downloads/swp/manifest.json \
  | jq -r .payload | base64 -d | jq .

# Compare this result with the SHA-256 stored for the same path in the payload.
curl -fsS https://launcher.expanded.space/downloads/swp/files/SWPMultispecs.lua \
  | sha256sum
```

## Required server installation

The MPQ only teaches the client how to display the custom records. The server
must use the exact generated DBC files as well. Extract the DBC files from
`assets/patch-T.MPQ` into the worldserver DBC directory, apply
[`server/generated_tiny_mounts.sql`](server/generated_tiny_mounts.sql) to
`acore_world`, then restart the server.

The demonstration IDs are:

| Record | ID |
|---|---:|
| Initiate Riding spell | 80860 |
| Sprint spell | 80861 |
| Gemstones skill line | 790 |
| Gemstone of Orgrimmar spell | 80900 |
| Gemstone of Orgrimmar item | 910001 |
| Tiny mount spells | 80865, 81000-81033 |
| Tiny mount creatures | 90001-90035 |
| Tiny mount items | 900100-900134 |

The server part is intentionally not installed automatically by the launcher.
