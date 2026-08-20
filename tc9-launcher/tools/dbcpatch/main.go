package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	initiateRidingAbilityID = uint32(60000)
	gemstoneSkillID         = uint32(790)
	firstGemstoneAbilityID  = uint32(60001)
	gemstoneRaceClassID     = uint32(60000)
	gemstoneLearnSpellID    = uint32(80901)
	legacyGemstoneItemID    = uint32(910000)
	firstMountSpellID       = uint32(81000)
	firstMountItemID        = uint32(900101)
	firstMountCreatureID    = uint32(90002)
	blackHorseSourceItem    = uint32(46308)
	blackHorseSpellID       = uint32(80865)
	blackHorseItemID        = uint32(900100)
	blackHorseCreatureID    = uint32(90001)
	legacyHeroicFirstID     = uint32(10000)
	legacyHardcoreFirstID   = uint32(10010)
)

var legacyHeroicMaps = []uint32{33, 43, 389}

type dbc struct {
	fields     uint32
	recordSize uint32
	records    [][]byte
	strings    []byte
}

type mountDefinition struct {
	VendorID   uint32 `json:"vendor_id"`
	VendorName string `json:"vendor_name"`
	ItemID     uint32 `json:"item_id"`
	ItemName   string `json:"item_name"`
	SpellID    uint32 `json:"spell_id"`
}

type generatedMount struct {
	mountDefinition
	ItemTarget     uint32
	SpellTarget    uint32
	CreatureSource uint32
	CreatureTarget uint32
	BaseName       string
	ItemNameTarget string
}

type gemstoneDefinition struct {
	Name            string
	ItemID          uint32
	SpellID         uint32
	CooldownMinutes int
}

type qolClientConfig struct {
	HearthstoneCooldownMinutes int
	OutOfCombatRunSpeed        float64
	OutOfCombatRunSpellID      uint32
	InitiateRidingSpellID      uint32
}

func loadQolClientConfig(path string) (qolClientConfig, error) {
	config := qolClientConfig{15, 1.20, 80861, 80860}
	data, err := os.ReadFile(path)
	if err != nil {
		return config, err
	}
	for lineNumber, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.SplitN(rawLine, "#", 2)[0])
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		field, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		switch field {
		case "Qol.HearthstoneCooldownMinutes":
			config.HearthstoneCooldownMinutes, err = strconv.Atoi(value)
		case "Qol.OutOfCombatRunSpeed":
			config.OutOfCombatRunSpeed, err = strconv.ParseFloat(value, 64)
		case "Qol.OutOfCombatRunIndicatorSpell":
			parsed, parseErr := strconv.ParseUint(value, 10, 32)
			err, config.OutOfCombatRunSpellID = parseErr, uint32(parsed)
		case "Qol.InitiateRidingSpell":
			parsed, parseErr := strconv.ParseUint(value, 10, 32)
			err, config.InitiateRidingSpellID = parseErr, uint32(parsed)
		}
		if err != nil {
			return config, fmt.Errorf("%s:%d: %w", path, lineNumber+1, err)
		}
	}
	if config.HearthstoneCooldownMinutes < 0 || config.HearthstoneCooldownMinutes > 10080 ||
		config.OutOfCombatRunSpeed < 1 || config.OutOfCombatRunSpeed > 6 ||
		config.OutOfCombatRunSpellID == 0 || config.InitiateRidingSpellID == 0 {
		return config, errors.New("QoL client settings are outside supported ranges")
	}
	return config, nil
}

func yamlScalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
		(value[0] == '\'' && value[len(value)-1] == '\'')) {
		return value[1 : len(value)-1]
	}
	return value
}

func loadGemstones(path string) ([]gemstoneDefinition, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	sharedCooldown := 0
	var gemstones []gemstoneDefinition
	var current *gemstoneDefinition
	for lineNumber, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.SplitN(rawLine, "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			gemstones = append(gemstones, gemstoneDefinition{})
			current = &gemstones[len(gemstones)-1]
			line = strings.TrimSpace(strings.TrimPrefix(line, "- "))
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		field, value := strings.TrimSpace(parts[0]), yamlScalar(parts[1])
		if current == nil {
			if field == "shared_cooldown_minutes" {
				sharedCooldown, err = strconv.Atoi(value)
			}
			continue
		}
		switch field {
		case "name":
			current.Name = value
		case "item_id":
			parsed, parseErr := strconv.ParseUint(value, 10, 32)
			err, current.ItemID = parseErr, uint32(parsed)
		case "spell_id":
			parsed, parseErr := strconv.ParseUint(value, 10, 32)
			err, current.SpellID = parseErr, uint32(parsed)
		case "cooldown_minutes":
			current.CooldownMinutes, err = strconv.Atoi(value)
		}
		if err != nil {
			return nil, 0, fmt.Errorf("%s:%d: %w", path, lineNumber+1, err)
		}
	}
	if sharedCooldown < 1 || sharedCooldown > 10080 {
		return nil, 0, errors.New("gemstone shared cooldown must be from 1 to 10080 minutes")
	}
	seenItems, seenSpells := map[uint32]bool{}, map[uint32]bool{}
	for index, gemstone := range gemstones {
		if gemstone.Name == "" || gemstone.ItemID == 0 || gemstone.SpellID == 0 ||
			gemstone.CooldownMinutes < 1 || gemstone.CooldownMinutes > 10080 {
			return nil, 0, fmt.Errorf("gemstone %d has incomplete or invalid client fields", index+1)
		}
		if seenItems[gemstone.ItemID] || seenSpells[gemstone.SpellID] {
			return nil, 0, fmt.Errorf("gemstone %d duplicates an item or spell ID", index+1)
		}
		seenItems[gemstone.ItemID], seenSpells[gemstone.SpellID] = true, true
	}
	if len(gemstones) == 0 {
		return nil, 0, errors.New("gemstone configuration contains no entries")
	}
	return gemstones, sharedCooldown, nil
}

func readDBC(path string) (*dbc, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 20 || string(b[:4]) != "WDBC" {
		return nil, fmt.Errorf("%s is not a WDBC file", path)
	}
	count := binary.LittleEndian.Uint32(b[4:8])
	fields := binary.LittleEndian.Uint32(b[8:12])
	recordSize := binary.LittleEndian.Uint32(b[12:16])
	stringSize := binary.LittleEndian.Uint32(b[16:20])
	recordEnd := 20 + int(count*recordSize)
	if recordSize != fields*4 || recordEnd+int(stringSize) != len(b) {
		return nil, fmt.Errorf("invalid DBC dimensions in %s", path)
	}
	d := &dbc{fields: fields, recordSize: recordSize, strings: append([]byte(nil), b[recordEnd:]...)}
	for i := uint32(0); i < count; i++ {
		start := 20 + int(i*recordSize)
		d.records = append(d.records, append([]byte(nil), b[start:start+int(recordSize)]...))
	}
	return d, nil
}

func (d *dbc) find(id uint32) ([]byte, error) {
	for _, r := range d.records {
		if getU32(r, 0) == id {
			return r, nil
		}
	}
	return nil, fmt.Errorf("DBC record %d not found", id)
}

func (d *dbc) requireFree(id uint32) error {
	if _, err := d.find(id); err == nil {
		return fmt.Errorf("target DBC ID %d is already occupied", id)
	}
	return nil
}

func (d *dbc) removeIDs(ids ...uint32) {
	remove := make(map[uint32]struct{}, len(ids))
	for _, id := range ids {
		remove[id] = struct{}{}
	}
	kept := d.records[:0]
	for _, record := range d.records {
		if _, found := remove[getU32(record, 0)]; !found {
			kept = append(kept, record)
		}
	}
	d.records = kept
}

func (d *dbc) remove(id uint32) {
	for index, record := range d.records {
		if getU32(record, 0) == id {
			d.records = append(d.records[:index], d.records[index+1:]...)
			return
		}
	}
}

func (d *dbc) moveIDsToFront(ids ...uint32) {
	wanted := make(map[uint32]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	front := make([][]byte, 0, len(ids))
	rest := make([][]byte, 0, len(d.records))
	for _, record := range d.records {
		if _, ok := wanted[getU32(record, 0)]; ok {
			front = append(front, record)
		} else {
			rest = append(rest, record)
		}
	}
	d.records = append(front, rest...)
}

func (d *dbc) appendString(s string) uint32 {
	offset := uint32(len(d.strings))
	d.strings = append(d.strings, []byte(s)...)
	d.strings = append(d.strings, 0)
	return offset
}

func (d *dbc) write(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	header := make([]byte, 20)
	copy(header, "WDBC")
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(d.records)))
	binary.LittleEndian.PutUint32(header[8:12], d.fields)
	binary.LittleEndian.PutUint32(header[12:16], d.recordSize)
	binary.LittleEndian.PutUint32(header[16:20], uint32(len(d.strings)))
	if _, err = f.Write(header); err != nil {
		return err
	}
	for _, r := range d.records {
		if _, err = f.Write(r); err != nil {
			return err
		}
	}
	_, err = f.Write(d.strings)
	return err
}

func getU32(r []byte, field int) uint32 {
	return binary.LittleEndian.Uint32(r[field*4 : field*4+4])
}

func putU32(r []byte, field int, value uint32) {
	binary.LittleEndian.PutUint32(r[field*4:field*4+4], value)
}

func setLocalizedSpellStrings(d *dbc, r []byte, name, rank, description, tooltip string) {
	nameOffset := d.appendString(name)
	rankOffset := d.appendString(rank)
	descriptionOffset := d.appendString(description)
	tooltipOffset := d.appendString(tooltip)
	for i := 0; i < 16; i++ {
		putU32(r, 136+i, nameOffset)
		putU32(r, 153+i, rankOffset)
		putU32(r, 170+i, descriptionOffset)
		putU32(r, 187+i, tooltipOffset)
	}
}

func addLegacyMapDifficulties(d *dbc) error {
	if d.fields != 23 {
		return fmt.Errorf("MapDifficulty.dbc has %d fields; expected 23", d.fields)
	}
	source, err := d.find(129) // Hellfire Ramparts, 5-player Heroic.
	if err != nil {
		return err
	}
	for index, mapID := range legacyHeroicMaps {
		heroicID := legacyHeroicFirstID + uint32(index)
		hardcoreID := legacyHardcoreFirstID + uint32(index)
		d.remove(heroicID)
		d.remove(hardcoreID)
		for _, existing := range d.records {
			if getU32(existing, 1) == mapID && (getU32(existing, 2) == 1 || getU32(existing, 2) == 2) {
				return fmt.Errorf("map %d already has a custom MapDifficulty record", mapID)
			}
		}
		heroic := append([]byte(nil), source...)
		putU32(heroic, 0, heroicID)
		putU32(heroic, 1, mapID)
		d.records = append(d.records, heroic)

		hardcore := append([]byte(nil), source...)
		putU32(hardcore, 0, hardcoreID)
		putU32(hardcore, 1, mapID)
		putU32(hardcore, 2, 2)
		d.records = append(d.records, hardcore)
	}
	return nil
}

func cloneGemstoneSkillLine(d *dbc) error {
	source, err := d.find(6) // Frost: a genuine class spellbook-tab skill line.
	if err != nil {
		return err
	}
	d.remove(gemstoneSkillID)
	record := append([]byte(nil), source...)
	putU32(record, 0, gemstoneSkillID)
	putU32(record, 1, 7) // Class-style category: displayed as its own spellbook tab.
	putU32(record, 2, 0)
	// The 3.3.5a client always synthesizes General first, then alphabetizes the
	// remaining spellbook tabs by their localized SkillLine name. DBC record
	// order is ignored. A leading space sorts this tab before every normal skill
	// name and is visually inert in the tab tooltip, keeping Gemstones in slot 2.
	name := d.appendString(" Gemstones")
	description := d.appendString("Teleportation gemstones discovered across Azeroth.")
	for i := 0; i < 16; i++ {
		putU32(record, 3+i, name)
		putU32(record, 20+i, description)
	}
	putU32(record, 19, 0)
	putU32(record, 36, 0)
	putU32(record, 37, 776) // Hearthstone spell icon.
	d.records = append(d.records, record)
	return nil
}

func cloneGemstoneRaceClassInfo(d *dbc) error {
	source, err := d.find(57) // Frost: class spellbook skill available to all races.
	if err != nil {
		return err
	}
	d.remove(gemstoneRaceClassID)
	record := append([]byte(nil), source...)
	putU32(record, 0, gemstoneRaceClassID)
	putU32(record, 1, gemstoneSkillID)
	putU32(record, 2, 0xFFFFFFFF) // Every race.
	putU32(record, 3, 0x7FF)      // Every playable class.
	d.records = append(d.records, record)
	return nil
}

func cloneUtilitySpell(d *dbc, sourceID, targetID uint32, name, rank, description, tooltip string, speedPercent int32, clearEffects bool) error {
	source, err := d.find(sourceID)
	if err != nil {
		return err
	}
	d.remove(targetID)
	r := append([]byte(nil), source...)
	putU32(r, 0, targetID)
	putU32(r, 37, 0)
	putU32(r, 38, 10)
	putU32(r, 39, 10)
	if clearEffects {
		for field := 71; field <= 130; field++ {
			putU32(r, field, 0)
		}
	} else {
		for i := 0; i < 3; i++ {
			if getU32(r, 95+i) == 32 { // SPELL_AURA_MOD_INCREASE_SPEED
				putU32(r, 80+i, uint32(speedPercent-1))
			}
		}
	}
	setLocalizedSpellStrings(d, r, name, rank, description, tooltip)
	d.records = append(d.records, r)
	return nil
}

func cloneTeleportSpell(d *dbc, sourceID, targetID uint32, name, description, tooltip string, cooldownMinutes, sharedCooldownMinutes int) error {
	source, err := d.find(sourceID)
	if err != nil {
		return err
	}
	d.remove(targetID)
	record := append([]byte(nil), source...)
	putU32(record, 0, targetID)
	// Shared cooldowns are managed explicitly by mod-gemstones. A native DBC
	// category timer overwrites older personal timers and can deserialize as a
	// multi-day cooldown in the 3.3.5 client after relogging.
	putU32(record, 1, 0)
	// Keep Hearthstone's cast restrictions, reagent-free cast and icon, but use
	// an independent cooldown and a script-owned destination. A dummy effect
	// prevents the cloned spell from resolving to the character's home bind.
	putU32(record, 29, uint32(cooldownMinutes*60*1000))
	putU32(record, 30, 0)
	for field := 71; field <= 130; field++ {
		putU32(record, field, 0)
	}
	putU32(record, 71, 3) // SPELL_EFFECT_DUMMY
	putU32(record, 86, 1) // TARGET_UNIT_CASTER
	setLocalizedSpellStrings(d, record, name, "Gemstone", description, tooltip)
	d.records = append(d.records, record)
	return nil
}

func cloneGemstoneLearnSpell(d *dbc, sourceID, targetID uint32) error {
	source, err := d.find(sourceID)
	if err != nil {
		return err
	}
	d.remove(targetID)
	record := append([]byte(nil), source...)
	putU32(record, 0, targetID)
	putU32(record, 28, 4) // SpellCastTimes.dbc: 1000 ms
	putU32(record, 29, 0)
	putU32(record, 30, 0)
	for field := 71; field <= 130; field++ {
		putU32(record, field, 0)
	}
	putU32(record, 71, 3) // SPELL_EFFECT_DUMMY
	putU32(record, 86, 1) // TARGET_UNIT_CASTER
	setLocalizedSpellStrings(d, record, "Attune Gemstone", "Gemstone",
		"Attunes a gemstone and permanently teaches its teleport spell.",
		"Attuning takes 1 second.")
	d.records = append(d.records, record)
	return nil
}

func cleanMountName(itemName string) string {
	name := itemName
	for _, prefix := range []string{"Reins of the ", "Horn of the ", "Whistle of the "} {
		name = strings.TrimPrefix(name, prefix)
	}
	name = strings.TrimSuffix(name, " Bridle")
	return name
}

func cloneMountSpell(d *dbc, sourceID, targetID, creatureTarget uint32, baseName string) (uint32, error) {
	source, err := d.find(sourceID)
	if err != nil {
		return 0, err
	}
	d.remove(targetID)
	r := append([]byte(nil), source...)
	putU32(r, 0, targetID)
	putU32(r, 37, 0)
	putU32(r, 38, 10)
	putU32(r, 39, 10)
	mountEffect := -1
	for i := 0; i < 3; i++ {
		aura := getU32(r, 95+i)
		if aura == 78 { // SPELL_AURA_MOUNTED
			mountEffect = i
			putU32(r, 110+i, creatureTarget)
		}
		if aura == 32 { // SPELL_AURA_MOD_INCREASE_MOUNTED_SPEED
			putU32(r, 80+i, 39)
		}
	}
	if mountEffect < 0 {
		return 0, errors.New("source spell has no mounted aura")
	}
	setLocalizedSpellStrings(d, r,
		"Tiny "+baseName,
		"Tiny Mount",
		"Summons and dismisses a tiny "+baseName+". Requires Initiate Riding.",
		"Increases movement speed by 40%.")
	d.records = append(d.records, r)
	return getU32(source, 110+mountEffect), nil
}

func cloneItem(d *dbc, sourceID, targetID uint32) error {
	source, err := d.find(sourceID)
	if err != nil {
		return err
	}
	d.remove(targetID)
	r := append([]byte(nil), source...)
	putU32(r, 0, targetID)
	d.records = append(d.records, r)
	return nil
}

func cloneSkillLineAbility(d *dbc, sourceID, targetID, spellID, supersededBy uint32) error {
	source, err := d.find(sourceID)
	if err != nil {
		return err
	}
	d.remove(targetID)
	record := append([]byte(nil), source...)
	putU32(record, 0, targetID)
	putU32(record, 2, spellID)
	putU32(record, 7, 0)
	putU32(record, 8, supersededBy)
	d.records = append(d.records, record)
	return nil
}

func cloneGemstoneSkillLineAbility(d *dbc, sourceID, targetID, spellID uint32) error {
	if err := cloneSkillLineAbility(d, sourceID, targetID, spellID, 0); err != nil {
		return err
	}
	record, err := d.find(targetID)
	if err != nil {
		return err
	}
	putU32(record, 1, gemstoneSkillID)
	putU32(record, 3, 0)
	putU32(record, 4, 0x7FF)
	putU32(record, 7, 1)
	// Gemstones are unlocked individually by consuming their item.
	// AcquireMethod 2 would teach every ability as soon as the Gemstones skill
	// is added for the first attunement.
	putU32(record, 9, 0)
	return nil
}

func loadMounts(path string) ([]mountDefinition, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var mounts []mountDefinition
	if err = json.Unmarshal(b, &mounts); err != nil {
		return nil, err
	}
	sort.Slice(mounts, func(i, j int) bool {
		if mounts[i].VendorID != mounts[j].VendorID {
			return mounts[i].VendorID < mounts[j].VendorID
		}
		return mounts[i].ItemID < mounts[j].ItemID
	})
	return mounts, nil
}

func sqlQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func writeSQL(path string, mounts []generatedMount, initiateRidingSpellID uint32) error {
	var out strings.Builder
	out.WriteString("-- Generated by tools/dbcpatch. Apply to acore_world with matching Spell.dbc and Item.dbc.\n\n")
	fmt.Fprintf(&out, "DELETE FROM `trainer_spell` WHERE `SpellId` = %d;\n", initiateRidingSpellID)
	out.WriteString("INSERT INTO `trainer_spell` (`TrainerId`,`SpellId`,`MoneyCost`,`ReqSkillLine`,`ReqSkillRank`,`ReqAbility1`,`ReqAbility2`,`ReqAbility3`,`ReqLevel`,`VerifiedBuild`)\n")
	fmt.Fprintf(&out, "SELECT DISTINCT `TrainerId`,%d,5000,0,0,0,0,0,10,12340 FROM `trainer_spell` WHERE `SpellId` = 33388;\n\n", initiateRidingSpellID)
	out.WriteString("DELETE FROM `npc_vendor` WHERE `item` BETWEEN 900100 AND 900164;\n")
	out.WriteString("DELETE FROM `item_template` WHERE `entry` BETWEEN 900100 AND 900164;\n")
	out.WriteString("DELETE FROM `creature_template_model` WHERE `CreatureID` BETWEEN 90001 AND 90065;\n")
	out.WriteString("DELETE FROM `creature_template` WHERE `entry` BETWEEN 90001 AND 90065;\n\n")
	for _, mount := range mounts {
		fmt.Fprintf(&out, "-- %s -> %s (%s)\n", mount.ItemName, mount.ItemNameTarget, mount.VendorName)
		fmt.Fprintf(&out, "DELETE FROM `creature_template_model` WHERE `CreatureID`=%d;\nDELETE FROM `creature_template` WHERE `entry`=%d;\n", mount.CreatureTarget, mount.CreatureTarget)
		out.WriteString("DROP TEMPORARY TABLE IF EXISTS `swp_mount_creature`; CREATE TEMPORARY TABLE `swp_mount_creature` LIKE `creature_template`;\n")
		fmt.Fprintf(&out, "INSERT INTO `swp_mount_creature` SELECT * FROM `creature_template` WHERE `entry`=%d;\n", mount.CreatureSource)
		fmt.Fprintf(&out, "UPDATE `swp_mount_creature` SET `entry`=%d,`name`=%s; REPLACE INTO `creature_template` SELECT * FROM `swp_mount_creature`; DROP TEMPORARY TABLE `swp_mount_creature`;\n", mount.CreatureTarget, sqlQuote("Tiny "+mount.BaseName))
		fmt.Fprintf(&out, "INSERT INTO `creature_template_model` (`CreatureID`,`Idx`,`CreatureDisplayID`,`DisplayScale`,`Probability`,`VerifiedBuild`) SELECT %d,`Idx`,`CreatureDisplayID`,0.45,`Probability`,`VerifiedBuild` FROM `creature_template_model` WHERE `CreatureID`=%d;\n", mount.CreatureTarget, mount.CreatureSource)
		out.WriteString("DROP TEMPORARY TABLE IF EXISTS `swp_mount_item`; CREATE TEMPORARY TABLE `swp_mount_item` LIKE `item_template`;\n")
		fmt.Fprintf(&out, "INSERT INTO `swp_mount_item` SELECT * FROM `item_template` WHERE `entry`=%d;\n", mount.ItemID)
		fmt.Fprintf(&out, "UPDATE `swp_mount_item` SET `entry`=%d,`name`=%s,`description`=%s,`RequiredLevel`=10,`RequiredSkill`=762,`RequiredSkillRank`=50,`spellid_2`=%d; REPLACE INTO `item_template` SELECT * FROM `swp_mount_item`; DROP TEMPORARY TABLE `swp_mount_item`;\n", mount.ItemTarget, sqlQuote(mount.ItemNameTarget), sqlQuote("Teaches this tiny 40% speed mount."), mount.SpellTarget)
		fmt.Fprintf(&out, "DELETE FROM `npc_vendor` WHERE `entry`=%d AND `item`=%d; INSERT INTO `npc_vendor` (`entry`,`slot`,`item`,`maxcount`,`incrtime`,`ExtendedCost`,`VerifiedBuild`) VALUES (%d,0,%d,0,0,0,12340);\n\n", mount.VendorID, mount.ItemTarget, mount.VendorID, mount.ItemTarget)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(out.String()), 0o644)
}

func main() {
	if len(os.Args) != 7 {
		fmt.Fprintln(os.Stderr, "usage: dbcpatch <base-dbc-directory> <output-directory> <racial-mounts.json> <server.sql> <mod-qol.conf> <mod-gemstones.yaml>")
		os.Exit(2)
	}
	base, out := os.Args[1], os.Args[2]
	qol, err := loadQolClientConfig(os.Args[5])
	if err != nil {
		fmt.Fprintln(os.Stderr, "dbcpatch:", err)
		os.Exit(2)
	}
	sprintPercent := int((qol.OutOfCombatRunSpeed-1)*100 + 0.5)
	gemstones, gemstoneSharedCooldown, err := loadGemstones(os.Args[6])
	if err != nil {
		fmt.Fprintln(os.Stderr, "dbcpatch:", err)
		os.Exit(2)
	}
	mounts, err := loadMounts(os.Args[3])
	if err != nil {
		fmt.Fprintln(os.Stderr, "dbcpatch:", err)
		os.Exit(1)
	}
	spellDBC, err := readDBC(filepath.Join(base, "Spell.dbc"))
	if err == nil && spellDBC.fields != 234 {
		err = fmt.Errorf("Spell.dbc has %d fields; expected 234", spellDBC.fields)
	}
	itemDBC, itemErr := readDBC(filepath.Join(base, "Item.dbc"))
	if err == nil {
		err = itemErr
	}
	if err == nil && itemDBC.fields != 8 {
		err = fmt.Errorf("Item.dbc has %d fields; expected 8", itemDBC.fields)
	}
	skillLineAbilityDBC, skillLineErr := readDBC(filepath.Join(base, "SkillLineAbility.dbc"))
	if err == nil {
		err = skillLineErr
	}
	if err == nil && skillLineAbilityDBC.fields != 14 {
		err = fmt.Errorf("SkillLineAbility.dbc has %d fields; expected 14", skillLineAbilityDBC.fields)
	}
	skillLineDBC, skillLineErr := readDBC(filepath.Join(base, "SkillLine.dbc"))
	if err == nil {
		err = skillLineErr
	}
	if err == nil && skillLineDBC.fields != 56 {
		err = fmt.Errorf("SkillLine.dbc has %d fields; expected 56", skillLineDBC.fields)
	}
	skillRaceClassDBC, skillRaceClassErr := readDBC(filepath.Join(base, "SkillRaceClassInfo.dbc"))
	if err == nil {
		err = skillRaceClassErr
	}
	if err == nil && skillRaceClassDBC.fields != 8 {
		err = fmt.Errorf("SkillRaceClassInfo.dbc has %d fields; expected 8", skillRaceClassDBC.fields)
	}
	mapDifficultyDBC, mapDifficultyErr := readDBC(filepath.Join(base, "MapDifficulty.dbc"))
	if err == nil {
		err = mapDifficultyErr
	}
	if err == nil {
		err = addLegacyMapDifficulties(mapDifficultyDBC)
	}
	if err == nil {
		spellTargets := []uint32{qol.InitiateRidingSpellID, qol.OutOfCombatRunSpellID, gemstoneLearnSpellID, blackHorseSpellID}
		itemTargets := []uint32{legacyGemstoneItemID, blackHorseItemID}
		gemstoneAbilityIDs := make([]uint32, 0, len(gemstones))
		for index, gemstone := range gemstones {
			spellTargets = append(spellTargets, gemstone.SpellID)
			itemTargets = append(itemTargets, gemstone.ItemID)
			gemstoneAbilityIDs = append(gemstoneAbilityIDs, firstGemstoneAbilityID+uint32(index))
		}
		for id := uint32(81000); id <= 81064; id++ {
			spellTargets = append(spellTargets, id)
		}
		for id := uint32(900100); id <= 900164; id++ {
			itemTargets = append(itemTargets, id)
		}
		spellDBC.removeIDs(spellTargets...)
		itemDBC.removeIDs(itemTargets...)
		skillLineAbilityDBC.removeIDs(append([]uint32{initiateRidingAbilityID}, gemstoneAbilityIDs...)...)
		skillLineDBC.removeIDs(gemstoneSkillID)
		skillRaceClassDBC.removeIDs(gemstoneRaceClassID)
	}
	if err == nil {
		err = cloneUtilitySpell(spellDBC, 33388, qol.InitiateRidingSpellID, "Initiate Riding", "Initiate", "A basic riding technique for young adventurers.", "Allows the use of tiny mounts. Maximum riding skill: 50.", 0, true)
	}
	if err == nil {
		err = cloneSkillLineAbility(skillLineAbilityDBC, 17539, initiateRidingAbilityID, qol.InitiateRidingSpellID, 33388)
	}
	if err == nil {
		err = cloneGemstoneSkillLine(skillLineDBC)
	}
	if err == nil {
		err = cloneGemstoneRaceClassInfo(skillRaceClassDBC)
	}
	for _, gemstone := range gemstones {
		if err != nil {
			break
		}
		destination := strings.TrimPrefix(gemstone.Name, "Gemstone of ")
		description := "Teleports you to " + destination + "."
		err = cloneTeleportSpell(spellDBC, 8690, gemstone.SpellID,
			gemstone.Name, description, description, gemstone.CooldownMinutes, gemstoneSharedCooldown)
	}
	if err == nil {
		// Spell 483 is WoW's native item-learning presentation. Its visual makes
		// attuning read like learning a mount instead of casting Hearthstone.
		err = cloneGemstoneLearnSpell(spellDBC, 483, gemstoneLearnSpellID)
	}
	for index, gemstone := range gemstones {
		if err != nil {
			break
		}
		err = cloneItem(itemDBC, 6948, gemstone.ItemID)
		if err == nil {
			abilityID := firstGemstoneAbilityID + uint32(index)
			err = cloneGemstoneSkillLineAbility(skillLineAbilityDBC, 17539, abilityID, gemstone.SpellID)
		}
	}
	if err == nil {
		sprintDescription := fmt.Sprintf("You run %d%% faster out of combat, PvP and stealth.", sprintPercent)
		err = cloneUtilitySpell(spellDBC, 51442, qol.OutOfCombatRunSpellID, "Sprint", "", sprintDescription, sprintDescription, int32(sprintPercent), false)
	}
	if err == nil {
		hearthstone, findErr := spellDBC.find(8690)
		if findErr != nil {
			err = findErr
		} else {
			cooldownMilliseconds := uint32(qol.HearthstoneCooldownMinutes * 60 * 1000)
			putU32(hearthstone, 29, cooldownMilliseconds)
			putU32(hearthstone, 30, cooldownMilliseconds)
		}
	}
	generated := make([]generatedMount, 0, len(mounts))
	next := uint32(0)
	for _, mount := range mounts {
		if err != nil {
			break
		}
		generatedMount := generatedMount{mountDefinition: mount, BaseName: cleanMountName(mount.ItemName)}
		if mount.ItemID == blackHorseSourceItem {
			generatedMount.ItemTarget = blackHorseItemID
			generatedMount.SpellTarget = blackHorseSpellID
			generatedMount.CreatureTarget = blackHorseCreatureID
		} else {
			generatedMount.ItemTarget = firstMountItemID + next
			generatedMount.SpellTarget = firstMountSpellID + next
			generatedMount.CreatureTarget = firstMountCreatureID + next
			next++
		}
		generatedMount.ItemNameTarget = "Reins of the Tiny " + generatedMount.BaseName
		generatedMount.CreatureSource, err = cloneMountSpell(spellDBC, mount.SpellID, generatedMount.SpellTarget, generatedMount.CreatureTarget, generatedMount.BaseName)
		if err == nil {
			err = cloneItem(itemDBC, mount.ItemID, generatedMount.ItemTarget)
		}
		generated = append(generated, generatedMount)
	}
	if err == nil {
		// General is synthesized by the client. Put Gemstones first in each
		// backing DBC so it becomes the first real tab immediately below it.
		skillLineDBC.moveIDsToFront(gemstoneSkillID)
		skillRaceClassDBC.moveIDsToFront(gemstoneRaceClassID)
		gemstoneAbilityIDs := make([]uint32, len(gemstones))
		for index := range gemstones {
			gemstoneAbilityIDs[index] = firstGemstoneAbilityID + uint32(index)
		}
		skillLineAbilityDBC.moveIDsToFront(gemstoneAbilityIDs...)
	}
	if err == nil {
		err = spellDBC.write(filepath.Join(out, "DBFilesClient", "Spell.dbc"))
	}
	if err == nil {
		err = itemDBC.write(filepath.Join(out, "DBFilesClient", "Item.dbc"))
	}
	if err == nil {
		err = skillLineAbilityDBC.write(filepath.Join(out, "DBFilesClient", "SkillLineAbility.dbc"))
	}
	if err == nil {
		err = skillLineDBC.write(filepath.Join(out, "DBFilesClient", "SkillLine.dbc"))
	}
	if err == nil {
		err = skillRaceClassDBC.write(filepath.Join(out, "DBFilesClient", "SkillRaceClassInfo.dbc"))
	}
	if err == nil {
		err = mapDifficultyDBC.write(filepath.Join(out, "DBFilesClient", "MapDifficulty.dbc"))
	}
	if err == nil {
		err = writeSQL(os.Args[4], generated, qol.InitiateRidingSpellID)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "dbcpatch:", err)
		os.Exit(1)
	}
	fmt.Printf("created %d tiny racial mounts, Initiate Riding (%d), Sprint (%d), %d configured gemstones, a %d-minute Hearthstone cooldown, and %d legacy Heroic/Hardcore maps\n", len(generated), qol.InitiateRidingSpellID, qol.OutOfCombatRunSpellID, len(gemstones), qol.HearthstoneCooldownMinutes, len(legacyHeroicMaps))
}
