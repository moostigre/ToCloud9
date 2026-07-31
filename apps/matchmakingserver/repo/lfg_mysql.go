package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/walkline/ToCloud9/apps/matchmakingserver/lfg"
)

type lfgMySQLRepository struct{ db *sql.DB }

func NewLFGMySQLRepository(db *sql.DB) LFGRepository { return &lfgMySQLRepository{db: db} }

func (r *lfgMySQLRepository) CreateEntry(ctx context.Context, entry *lfg.Entry) error {
	if err := entry.Validate(); err != nil {
		return err
	}
	dungeons, err := json.Marshal(entry.SelectedDungeons)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state := entry.State
	if state == "" {
		state = lfg.EntryStateQueued
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO lfg_entries
        (realm_id, battlegroup_id, party_id, leader_guid, queue_category, selected_dungeons, state, partition_key)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, entry.RealmID, entry.BattlegroupID, entry.PartyID, entry.LeaderGUID,
		entry.QueueCategory, dungeons, state, entry.PartitionKey)
	if err != nil {
		return err
	}
	entryID, err := res.LastInsertId()
	if err != nil {
		return err
	}
	for _, member := range entry.Members {
		_, err = tx.ExecContext(ctx, `INSERT INTO lfg_entry_members
            (entry_id, realm_id, player_guid, selected_roles, assigned_role, level, class, online)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, entryID, member.RealmID, member.PlayerGUID, member.SelectedRoles,
			member.AssignedRole, member.Level, member.Class, member.Online)
		if isMySQLDuplicate(err) {
			return ErrLFGPlayerAlreadyQueued
		}
		if err != nil {
			return err
		}
	}
	payload, err := json.Marshal(map[string]any{"entry_id": entryID, "state": state, "partition_key": entry.PartitionKey})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO lfg_outbox (aggregate_type, aggregate_id, event_type, payload)
        VALUES ('entry', ?, 'lfg.entry.created', ?)`, entryID, payload); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	entry.ID, entry.State, entry.Version = uint64(entryID), state, 1
	return nil
}

func (r *lfgMySQLRepository) CancelEntry(ctx context.Context, entryID, expectedVersion uint64) (bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE lfg_entries SET state = ?, version = version + 1
        WHERE id = ? AND version = ? AND state IN (?, ?)`, lfg.EntryStateCancelled, entryID, expectedVersion,
		lfg.EntryStateRoleCheck, lfg.EntryStateQueued)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM lfg_entry_members WHERE entry_id = ?`, entryID); err != nil {
		return false, err
	}
	payload, _ := json.Marshal(map[string]any{"entry_id": entryID, "state": lfg.EntryStateCancelled, "version": expectedVersion + 1})
	if _, err = tx.ExecContext(ctx, `INSERT INTO lfg_outbox (aggregate_type, aggregate_id, event_type, payload)
        VALUES ('entry', ?, 'lfg.entry.cancelled', ?)`, entryID, payload); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (r *lfgMySQLRepository) AcquirePartitionLease(ctx context.Context, key, owner string, duration time.Duration) (*lfg.Lease, error) {
	if key == "" || owner == "" || duration <= 0 {
		return nil, ErrLFGLeaseNotAcquired
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var currentOwner string
	var token uint64
	var until time.Time
	var databaseNow time.Time
	err = tx.QueryRowContext(ctx, `SELECT owner_id, fencing_token, lease_until, UTC_TIMESTAMP(6) FROM lfg_partition_leases
        WHERE partition_key = ? FOR UPDATE`, key).Scan(&currentOwner, &token, &until, &databaseNow)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err = tx.QueryRowContext(ctx, `SELECT UTC_TIMESTAMP(6)`).Scan(&databaseNow); err != nil {
			return nil, err
		}
		token = 1
		until = databaseNow.Add(duration)
		_, err = tx.ExecContext(ctx, `INSERT INTO lfg_partition_leases (partition_key, owner_id, fencing_token, lease_until)
            VALUES (?, ?, ?, ?)`, key, owner, token, until)
	case err != nil:
		return nil, err
	case currentOwner != owner && until.After(databaseNow):
		return nil, ErrLFGLeaseNotAcquired
	default:
		// Every acquisition after expiry receives a new token, even if a restarted
		// process accidentally reuses the same owner ID.
		if !until.After(databaseNow) || currentOwner != owner {
			token++
		}
		until = databaseNow.Add(duration)
		_, err = tx.ExecContext(ctx, `UPDATE lfg_partition_leases SET owner_id = ?, fencing_token = ?, lease_until = ?
            WHERE partition_key = ?`, owner, token, until, key)
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &lfg.Lease{PartitionKey: key, OwnerID: owner, FencingToken: token, LeaseUntil: until}, nil
}

func (r *lfgMySQLRepository) RenewPartitionLease(ctx context.Context, lease lfg.Lease, duration time.Duration) (*lfg.Lease, error) {
	if duration <= 0 {
		return nil, ErrLFGLeaseNotAcquired
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE lfg_partition_leases
        SET lease_until = DATE_ADD(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND)
        WHERE partition_key = ? AND owner_id = ? AND fencing_token = ? AND lease_until > UTC_TIMESTAMP(6)`,
		duration.Microseconds(), lease.PartitionKey, lease.OwnerID, lease.FencingToken)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, ErrLFGLeaseNotAcquired
	}
	if err = tx.QueryRowContext(ctx, `SELECT lease_until FROM lfg_partition_leases WHERE partition_key = ?`, lease.PartitionKey).Scan(&lease.LeaseUntil); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &lease, nil
}

func isMySQLDuplicate(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}

var _ LFGRepository = (*lfgMySQLRepository)(nil)
