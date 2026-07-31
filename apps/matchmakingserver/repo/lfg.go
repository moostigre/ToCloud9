package repo

import (
	"context"
	"errors"
	"time"

	"github.com/walkline/ToCloud9/apps/matchmakingserver/lfg"
)

var (
	ErrLFGPlayerAlreadyQueued = errors.New("lfg player already queued")
	ErrLFGLeaseNotAcquired    = errors.New("lfg partition lease not acquired")
)

type LFGRepository interface {
	CreateEntry(ctx context.Context, entry *lfg.Entry) error
	CancelEntry(ctx context.Context, entryID uint64, expectedVersion uint64) (bool, error)
	AcquirePartitionLease(ctx context.Context, partitionKey, ownerID string, duration time.Duration) (*lfg.Lease, error)
	RenewPartitionLease(ctx context.Context, lease lfg.Lease, duration time.Duration) (*lfg.Lease, error)
}
