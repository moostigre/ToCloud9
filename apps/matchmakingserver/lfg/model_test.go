package lfg

import (
	"errors"
	"testing"
)

func TestEntryValidateRequiresLeaderAndSortsDungeons(t *testing.T) {
	entry := Entry{
		RealmID: 1, LeaderGUID: 10, QueueCategory: "random-normal",
		PartitionKey:     NewPartitionKey(1, "random-normal", "70-80"),
		SelectedDungeons: []uint32{7, 2, 5},
		Members:          []Member{{RealmID: 1, PlayerGUID: 10, SelectedRoles: RoleTank, Level: 80, Online: true}},
	}
	if err := entry.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := entry.SelectedDungeons; got[0] != 2 || got[1] != 5 || got[2] != 7 {
		t.Fatalf("dungeons were not canonicalized: %v", got)
	}

	entry.LeaderGUID = 11
	if err := entry.Validate(); !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("expected invalid entry, got %v", err)
	}
}

func TestEntryValidateRejectsDuplicateRealmPlayer(t *testing.T) {
	entry := Entry{
		RealmID: 1, LeaderGUID: 10, QueueCategory: "specific", PartitionKey: "1:specific:80",
		SelectedDungeons: []uint32{1},
		Members: []Member{
			{RealmID: 1, PlayerGUID: 10, SelectedRoles: RoleTank},
			{RealmID: 1, PlayerGUID: 10, SelectedRoles: RoleDamage},
		},
	}
	if err := entry.Validate(); !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("expected invalid entry, got %v", err)
	}
}
