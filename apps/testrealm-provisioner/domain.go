package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type RealmState string

const (
	RealmProvisioning RealmState = "provisioning"
	RealmRunning      RealmState = "running"
	RealmOffline      RealmState = "offline"
	RealmDeleting     RealmState = "deleting"
	RealmFailed       RealmState = "failed"
	RealmSimulated    RealmState = "simulated"
	RealmDeleted      RealmState = "deleted"
)

type ItemRequest struct {
	ID    uint32 `json:"id"`
	Count uint16 `json:"count"`
}

type CharacterRequest struct {
	Name            string        `json:"name"`
	Race            string        `json:"race"`
	Class           string        `json:"class"`
	Level           uint8         `json:"level"`
	MoneyGold       uint32        `json:"money_gold"`
	Items           []ItemRequest `json:"items"`
	ActiveQuests    []uint32      `json:"active_quests"`
	CompletedQuests []uint32      `json:"completed_quests"`
}

type ProvisioningSpec struct {
	Summary    string             `json:"summary"`
	Complete   bool               `json:"complete"`
	RealmOnly  bool               `json:"realm_only"`
	Question   string             `json:"question"`
	Characters []CharacterRequest `json:"characters"`
}

type PullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	HTMLURL   string    `json:"html_url"`
	HeadSHA   string    `json:"head_sha"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ChatSession struct {
	ID        string           `json:"id"`
	PR        PullRequest      `json:"pr"`
	Draft     ProvisioningSpec `json:"draft"`
	CreatedAt time.Time        `json:"created_at"`
	ExpiresAt time.Time        `json:"expires_at"`
}

type Realm struct {
	ID             string           `json:"id"`
	Namespace      string           `json:"namespace"`
	PRNumber       int              `json:"pr_number"`
	CommitSHA      string           `json:"commit_sha"`
	Image          string           `json:"image"`
	ImporterImage  string           `json:"importer_image"`
	State          RealmState       `json:"state"`
	Spec           ProvisioningSpec `json:"spec"`
	TokenHash      string           `json:"token_hash"`
	CreatedAt      time.Time        `json:"created_at"`
	LastPlayerSeen time.Time        `json:"last_player_seen"`
	OfflineAt      *time.Time       `json:"offline_at,omitempty"`
	DeletedAt      *time.Time       `json:"deleted_at,omitempty"`
	Progress       string           `json:"progress"`
	ProgressPhase  string           `json:"progress_phase,omitempty"`
	ProgressPct    int              `json:"progress_percent,omitempty"`
	ProgressDetail string           `json:"progress_detail,omitempty"`
	ProgressAt     *time.Time       `json:"progress_updated_at,omitempty"`
	Failure        string           `json:"failure,omitempty"`
	Simulated      bool             `json:"simulated,omitempty"`
	RealmName      string           `json:"realm_name,omitempty"`
	Address        string           `json:"address,omitempty"`
	Port           uint16           `json:"port,omitempty"`
	RealmListID    uint32           `json:"realmlist_id,omitempty"`
}

var characterNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z]{1,11}$`)

var allowedRaces = map[string]bool{"human": true, "orc": true, "dwarf": true, "night elf": true, "undead": true, "tauren": true, "gnome": true, "troll": true, "blood elf": true, "draenei": true}
var allowedClasses = map[string]bool{"warrior": true, "paladin": true, "hunter": true, "rogue": true, "priest": true, "death knight": true, "shaman": true, "mage": true, "warlock": true, "druid": true}

func validateSpec(spec ProvisioningSpec) error {
	if !spec.Complete {
		return errors.New("requirements are not complete")
	}
	if len(spec.Summary) == 0 || len(spec.Summary) > 1200 {
		return errors.New("summary must contain 1-1200 characters")
	}
	if strings.TrimSpace(spec.Summary) != spec.Summary || strings.ContainsAny(spec.Summary, "\x00\r\n") {
		return errors.New("summary contains unsupported characters")
	}
	if spec.RealmOnly && len(spec.Characters) == 0 {
		return nil
	}
	if spec.RealmOnly {
		return errors.New("realm-only requests cannot contain character fixtures")
	}
	if len(spec.Characters) < 1 || len(spec.Characters) > 3 {
		return errors.New("between one and three characters are required")
	}
	for i, char := range spec.Characters {
		if !characterNamePattern.MatchString(char.Name) {
			return fmt.Errorf("character %d has an invalid name", i+1)
		}
		if char.Level < 1 || char.Level > 80 {
			return fmt.Errorf("character %s has an invalid level", char.Name)
		}
		if char.MoneyGold > 10000 {
			return fmt.Errorf("character %s exceeds the money limit", char.Name)
		}
		if len(char.Items) > 40 || len(char.ActiveQuests) > 25 || len(char.CompletedQuests) > 100 {
			return fmt.Errorf("character %s exceeds fixture limits", char.Name)
		}
		seenItems, seenQuests := map[uint32]bool{}, map[uint32]bool{}
		for _, item := range char.Items {
			if item.ID == 0 || item.Count == 0 || item.Count > 200 {
				return fmt.Errorf("character %s contains an invalid item", char.Name)
			}
			if seenItems[item.ID] {
				return fmt.Errorf("character %s contains duplicate item IDs", char.Name)
			}
			seenItems[item.ID] = true
		}
		if !allowedRaces[strings.ToLower(strings.TrimSpace(char.Race))] || !allowedClasses[strings.ToLower(strings.TrimSpace(char.Class))] {
			return fmt.Errorf("character %s has an unsupported race or class", char.Name)
		}
		for _, quest := range append(append([]uint32{}, char.ActiveQuests...), char.CompletedQuests...) {
			if quest == 0 || seenQuests[quest] {
				return fmt.Errorf("character %s contains invalid or duplicate quest IDs", char.Name)
			}
			seenQuests[quest] = true
		}
	}
	return nil
}
