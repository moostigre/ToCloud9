#!/usr/bin/env python3
"""Build a selectable 3.3.5a MPQ containing the era-correct Spell.dbc."""

import argparse
import pathlib
import shutil
import subprocess
import tempfile


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--profile", required=True, choices=("vanilla", "tbc", "wotlk"))
    parser.add_argument("--wotlk-dbc", required=True, type=pathlib.Path)
    parser.add_argument("--historical-dbc", type=pathlib.Path)
    parser.add_argument("--profile-sql", type=pathlib.Path)
    parser.add_argument("--output", required=True, type=pathlib.Path)
    parser.add_argument("--server-dbc-output", type=pathlib.Path)
    args = parser.parse_args()
    if args.profile != "wotlk" and (not args.historical_dbc or not args.profile_sql):
        parser.error("--historical-dbc and --profile-sql are required for Vanilla/TBC")
    patcher = pathlib.Path(__file__).with_name("patch_spell_dbc.py")
    with tempfile.TemporaryDirectory(prefix="retro-itemization-") as temporary:
        root = pathlib.Path(temporary)
        dbc_dir = root / "DBFilesClient"
        dbc_dir.mkdir()
        command = [
            "python3", str(patcher), "--profile", args.profile,
            "--input", str(args.wotlk_dbc), "--output", str(dbc_dir / "Spell.dbc"),
        ]
        if args.profile != "wotlk":
            command += [
                "--historical-dbc", str(args.historical_dbc),
                "--profile-sql", str(args.profile_sql),
            ]
        subprocess.run(command, check=True)
        if args.server_dbc_output:
            args.server_dbc_output.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(dbc_dir / "Spell.dbc", args.server_dbc_output)
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.unlink(missing_ok=True)
        if not shutil.which("smpq"):
            raise RuntimeError("smpq is required to build the client MPQ")
        subprocess.run(
            ["smpq", "-c", "-M", "1", str(args.output), "DBFilesClient/Spell.dbc"],
            cwd=root, check=True,
        )
    print(args.output)


if __name__ == "__main__":
    main()
