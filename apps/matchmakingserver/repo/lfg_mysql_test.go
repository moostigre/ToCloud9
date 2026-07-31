package repo

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"github.com/walkline/ToCloud9/apps/matchmakingserver/lfg"
)

func validLFGEntry() *lfg.Entry {
	return &lfg.Entry{
		RealmID: 1, BattlegroupID: 2, LeaderGUID: 10, QueueCategory: "random-normal",
		PartitionKey: "2:random-normal:70-80", SelectedDungeons: []uint32{7, 2},
		Members: []lfg.Member{{RealmID: 1, PlayerGUID: 10, SelectedRoles: lfg.RoleTank, Level: 80, Class: 1, Online: true}},
	}
}

func TestLFGCreateEntryCommitsEntryMembersAndOutboxTogether(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO lfg_entries").
		WithArgs(uint32(1), uint32(2), nil, uint64(10), "random-normal", []byte("[2,7]"), lfg.EntryStateQueued, "2:random-normal:70-80").
		WillReturnResult(sqlmock.NewResult(42, 1))
	mock.ExpectExec("INSERT INTO lfg_entry_members").
		WithArgs(int64(42), uint32(1), uint64(10), lfg.RoleTank, lfg.Role(0), uint8(80), uint8(1), true).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO lfg_outbox").
		WithArgs(int64(42), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(9, 1))
	mock.ExpectCommit()

	entry := validLFGEntry()
	if err := NewLFGMySQLRepository(db).CreateEntry(context.Background(), entry); err != nil {
		t.Fatal(err)
	}
	if entry.ID != 42 || entry.Version != 1 || entry.State != lfg.EntryStateQueued {
		t.Fatalf("unexpected persisted entry: %+v", entry)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLFGCreateEntryMapsUniquePlayerConflictAndRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO lfg_entries").WillReturnResult(sqlmock.NewResult(42, 1))
	mock.ExpectExec("INSERT INTO lfg_entry_members").
		WillReturnError(&mysql.MySQLError{Number: 1062, Message: "uq_lfg_active_player"})
	mock.ExpectRollback()

	err = NewLFGMySQLRepository(db).CreateEntry(context.Background(), validLFGEntry())
	if !errors.Is(err, ErrLFGPlayerAlreadyQueued) {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLFGAcquireNewLeaseStartsFencingAtOne(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT owner_id, fencing_token, lease_until, UTC_TIMESTAMP(6) FROM lfg_partition_leases\n        WHERE partition_key = ? FOR UPDATE")).
		WithArgs("p1").WillReturnRows(sqlmock.NewRows([]string{"owner_id", "fencing_token", "lease_until", "database_now"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT UTC_TIMESTAMP(6)")).
		WillReturnRows(sqlmock.NewRows([]string{"database_now"}).AddRow(time.Now().UTC()))
	mock.ExpectExec("INSERT INTO lfg_partition_leases").
		WithArgs("p1", "worker-a", uint64(1), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	lease, err := NewLFGMySQLRepository(db).AcquirePartitionLease(context.Background(), "p1", "worker-a", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if lease.FencingToken != 1 || lease.OwnerID != "worker-a" {
		t.Fatalf("unexpected lease: %+v", lease)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLFGAcquireExpiredLeaseAdvancesFenceForReusedOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	databaseNow := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT owner_id, fencing_token, lease_until, UTC_TIMESTAMP(6) FROM lfg_partition_leases")).
		WithArgs("p1").WillReturnRows(sqlmock.NewRows([]string{"owner_id", "fencing_token", "lease_until", "database_now"}).
		AddRow("worker-a", uint64(7), databaseNow.Add(-time.Second), databaseNow))
	mock.ExpectExec("UPDATE lfg_partition_leases").
		WithArgs("worker-a", uint64(8), sqlmock.AnyArg(), "p1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	lease, err := NewLFGMySQLRepository(db).AcquirePartitionLease(context.Background(), "p1", "worker-a", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if lease.FencingToken != 8 {
		t.Fatalf("expected a new fencing token, got %+v", lease)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLFGRenewLeaseRejectsStaleFencingToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE lfg_partition_leases").
		WithArgs(int64(10000000), "p1", "worker-a", uint64(3)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	_, err = NewLFGMySQLRepository(db).RenewPartitionLease(context.Background(), lfg.Lease{
		PartitionKey: "p1", OwnerID: "worker-a", FencingToken: 3,
	}, 10*time.Second)
	if !errors.Is(err, ErrLFGLeaseNotAcquired) {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
