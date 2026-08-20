package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestConfig(t *testing.T, name, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadQolClientConfig(t *testing.T) {
	path := writeTestConfig(t, "mod_qol.conf", `
Qol.HearthstoneCooldownMinutes = 20
Qol.OutOfCombatRunSpeed = 1.35
Qol.OutOfCombatRunIndicatorSpell = 82001
Qol.InitiateRidingSpell = 82000
`)
	config, err := loadQolClientConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if config.HearthstoneCooldownMinutes != 20 || config.OutOfCombatRunSpeed != 1.35 ||
		config.OutOfCombatRunSpellID != 82001 || config.InitiateRidingSpellID != 82000 {
		t.Fatalf("unexpected QoL client config: %+v", config)
	}
}

func TestLoadGemstones(t *testing.T) {
	path := writeTestConfig(t, "mod_gemstones.yaml", `
shared_cooldown_minutes: 12
gemstones:
  - key: test
    name: "Gemstone of Test"
    item_id: 920001
    spell_id: 82002
    cooldown_minutes: 45
`)
	gemstones, sharedCooldown, err := loadGemstones(path)
	if err != nil {
		t.Fatal(err)
	}
	if sharedCooldown != 12 || len(gemstones) != 1 || gemstones[0].Name != "Gemstone of Test" ||
		gemstones[0].ItemID != 920001 || gemstones[0].SpellID != 82002 || gemstones[0].CooldownMinutes != 45 {
		t.Fatalf("unexpected gemstone client config: shared=%d gemstones=%+v", sharedCooldown, gemstones)
	}
}

func TestCloneTeleportSpellUsesConfiguredCooldown(t *testing.T) {
	record := make([]byte, 234*4)
	putU32(record, 0, 8690)
	spellDBC := &dbc{fields: 234, recordSize: 234 * 4, records: [][]byte{record}, strings: []byte{0}}
	if err := cloneTeleportSpell(spellDBC, 8690, 80900, "Gemstone of Test", "Test", "Test", 5, 5); err != nil {
		t.Fatal(err)
	}
	gemstone, err := spellDBC.find(80900)
	if err != nil {
		t.Fatal(err)
	}
	if recovery := getU32(gemstone, 29); recovery != 5*60*1000 {
		t.Fatalf("expected a five-minute recovery, got %d ms", recovery)
	}
	if category := getU32(gemstone, 1); category != 0 {
		t.Fatalf("expected no native shared cooldown category, got %d", category)
	}
	if categoryRecovery := getU32(gemstone, 30); categoryRecovery != 0 {
		t.Fatalf("expected no category recovery, got %d ms", categoryRecovery)
	}
}

func TestGemstoneSkillLineAbilityIsNotAutomaticallyAcquired(t *testing.T) {
	record := make([]byte, 14*4)
	putU32(record, 0, 17539)
	putU32(record, 9, 2)
	skillDBC := &dbc{fields: 14, recordSize: 14 * 4, records: [][]byte{record}, strings: []byte{0}}

	if err := cloneGemstoneSkillLineAbility(skillDBC, 17539, firstGemstoneAbilityID, 80900); err != nil {
		t.Fatal(err)
	}
	ability, err := skillDBC.find(firstGemstoneAbilityID)
	if err != nil {
		t.Fatal(err)
	}
	if acquireMethod := getU32(ability, 9); acquireMethod != 0 {
		t.Fatalf("gemstone ability must be item-learned, got acquire method %d", acquireMethod)
	}
}
