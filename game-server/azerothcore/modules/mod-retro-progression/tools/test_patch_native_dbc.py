import struct
import tempfile
import unittest
from pathlib import Path

from patch_native_dbc import (
    convert_tbc_talents,
    convert_tbc_talent_tabs,
    restore_tbc_talent_spells,
    write_dbc,
)


class ConvertTbcTalentTabsTest(unittest.TestCase):
    def test_maps_page_and_background_after_wotlk_pet_mask(self):
        # TBC: id, 8 locale offsets, flags, icon, race, class, page, background.
        source = (283, 1, 0, 0, 0, 0, 0, 0, 0, 7, 62, 2047, 1024, 1, 41)

        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            write_dbc(directory / "TalentTab.dbc", 15,
                      [struct.pack("<15I", *source)], b"\0FeralCombat\0")

            rows, strings = convert_tbc_talent_tabs(directory)
            result = struct.unpack("<24I", rows[0])

        self.assertEqual(result[20], 1024)  # class mask
        self.assertEqual(result[21], 0)     # added WotLK pet-talent mask
        self.assertEqual(result[22], 1)     # tree page
        self.assertEqual(result[23], 41)    # background filename offset
        self.assertEqual(strings, b"\0FeralCombat\0")


class ConvertTbcTalentsTest(unittest.TestCase):
    def test_preserves_source_tree_order(self):
        # Talent IDs are deliberately not ordered. The source order represents
        # the order returned by GetTalentInfo(tree, index) in the client.
        source = [
            (762, 283, 0, 0, 16814, 0, 0, 0, 0, *([0] * 12)),
            (761, 283, 0, 1, 16689, 0, 0, 0, 0, *([0] * 12)),
            (1822, 283, 1, 1, 35363, 0, 0, 0, 0, *([0] * 12)),
        ]

        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            write_dbc(directory / "Talent.dbc", 21,
                      [struct.pack("<21I", *row) for row in source], b"\0")
            rows, missing, dependencies = convert_tbc_talents(
                directory, {16814, 16689, 35363})

        result = [struct.unpack("<23I", row) for row in rows]
        self.assertEqual([row[0] for row in result], [762, 761, 1822])
        self.assertEqual(missing, {})
        self.assertEqual(dependencies, {})

    def test_imports_complete_historical_talent_spell(self):
        talent = struct.pack(
            "<23I", 1823, 381, 8, 1, 35395, 0, 0, 0, 0, *([0] * 14)
        )
        historical_spell = [0] * 179
        historical_spell[0] = 35395
        historical_spell[120] = 2272
        historical_spell[121] = 17
        wrath_spell = [0] * 234
        wrath_spell[0] = 35395
        wrath_spell[133] = 2309
        wrath_spell[134] = 42

        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            write_dbc(directory / "Spell.dbc", 179,
                      [struct.pack("<179I", *historical_spell)], b"\0")
            target = directory / "WrathSpell.dbc"
            write_dbc(target, 234, [struct.pack("<234I", *wrath_spell)], b"\0")

            converted, imported = restore_tbc_talent_spells(directory, target, [talent])
            data = target.read_bytes()
            result = struct.unpack_from("<234I", data, 20)

        self.assertEqual(converted, 1)
        self.assertEqual(imported, 0)
        self.assertEqual(result[133], 2272)
        self.assertEqual(result[134], 17)


if __name__ == "__main__":
    unittest.main()
