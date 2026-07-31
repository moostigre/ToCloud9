package lfg

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type EntryState string

const (
	EntryStateRoleCheck EntryState = "role_check"
	EntryStateQueued    EntryState = "queued"
	EntryStateProposed  EntryState = "proposed"
	EntryStateAssigned  EntryState = "assigned"
	EntryStateInDungeon EntryState = "in_dungeon"
	EntryStateCancelled EntryState = "cancelled"
	EntryStateExpired   EntryState = "expired"
)

type Role uint8

const (
	RoleTank Role = 1 << iota
	RoleHealer
	RoleDamage
)

type Member struct {
	RealmID       uint32
	PlayerGUID    uint64
	SelectedRoles Role
	AssignedRole  Role
	Level         uint8
	Class         uint8
	Online        bool
}

type Entry struct {
	ID               uint64
	RealmID          uint32
	BattlegroupID    uint32
	PartyID          *uint64
	LeaderGUID       uint64
	QueueCategory    string
	SelectedDungeons []uint32
	State            EntryState
	PartitionKey     string
	Version          uint64
	CreatedAt        time.Time
	Members          []Member
}

var ErrInvalidEntry = errors.New("invalid lfg entry")

func NewPartitionKey(battlegroupID uint32, category, bracket string) string {
	return fmt.Sprintf("%d:%s:%s", battlegroupID, strings.TrimSpace(category), strings.TrimSpace(bracket))
}

func (e *Entry) Validate() error {
	if e.RealmID == 0 || e.LeaderGUID == 0 || strings.TrimSpace(e.QueueCategory) == "" || strings.TrimSpace(e.PartitionKey) == "" {
		return ErrInvalidEntry
	}
	if len(e.Members) == 0 || len(e.SelectedDungeons) == 0 {
		return ErrInvalidEntry
	}
	foundLeader := false
	type playerKey struct {
		realmID uint32
		guid    uint64
	}
	seen := make(map[playerKey]struct{}, len(e.Members))
	for _, member := range e.Members {
		if member.RealmID == 0 || member.PlayerGUID == 0 || member.SelectedRoles == 0 {
			return ErrInvalidEntry
		}
		key := playerKey{realmID: member.RealmID, guid: member.PlayerGUID}
		if _, ok := seen[key]; ok {
			return ErrInvalidEntry
		}
		seen[key] = struct{}{}
		foundLeader = foundLeader || (member.RealmID == e.RealmID && member.PlayerGUID == e.LeaderGUID)
	}
	if !foundLeader {
		return ErrInvalidEntry
	}
	sort.Slice(e.SelectedDungeons, func(i, j int) bool { return e.SelectedDungeons[i] < e.SelectedDungeons[j] })
	return nil
}

type Lease struct {
	PartitionKey string
	OwnerID      string
	FencingToken uint64
	LeaseUntil   time.Time
}
