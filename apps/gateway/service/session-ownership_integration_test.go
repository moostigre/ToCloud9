package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	redis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func TestSessionOwnershipIntegrationTakeover(t *testing.T) {
	redisURL := os.Getenv("TC9_INTEGRATION_REDIS_URL")
	natsURL := os.Getenv("TC9_INTEGRATION_NATS_URL")
	if redisURL == "" || natsURL == "" {
		t.Skip("set TC9_INTEGRATION_REDIS_URL and TC9_INTEGRATION_NATS_URL to run")
	}
	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(redisOptions)
	t.Cleanup(func() { _ = rdb.Close() })
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)

	const (
		realmID   = uint32(4294967294)
		accountID = uint32(987654321)
	)
	logger := zerolog.Nop()
	first := NewSessionOwnershipService(rdb, nc, &logger, "integration-gateway-1", realmID, 3*time.Second)
	second := NewSessionOwnershipService(rdb, nc, &logger, "integration-gateway-2", realmID, 3*time.Second)
	if err = first.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Close)
	if err = second.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer rdb.Del(context.Background(),
		first.accountKey(accountID), first.evictionStreamKey(first.gatewayID),
		second.evictionStreamKey(second.gatewayID),
	)

	evicted := make(chan struct{})
	unregister := first.Register("token-1", func(context.Context) bool { close(evicted); return true })
	defer unregister()
	if err = first.ClaimAccount(ctx, accountID, "token-1"); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	takeoverStarted := time.Now()
	if err = second.ClaimAccount(ctx, accountID, "token-2"); err != nil {
		t.Fatalf("takeover claim: %v", err)
	}
	if elapsed := time.Since(takeoverStarted); elapsed > time.Second {
		t.Fatalf("live gateway takeover took %s", elapsed)
	}
	select {
	case <-evicted:
	case <-ctx.Done():
		t.Fatal("previous owner was not evicted")
	}
	if err = first.ReleaseAccount(ctx, accountID, "token-1"); err != nil {
		t.Fatal(err)
	}
	owner, err := rdb.Get(ctx, second.accountKey(accountID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if want := second.owner("token-2"); owner != want {
		t.Fatalf("owner = %q, want %q", owner, want)
	}
}

func TestSessionOwnershipIntegrationDurableEvictionWithoutNATS(t *testing.T) {
	redisURL := os.Getenv("TC9_INTEGRATION_REDIS_URL")
	natsURL := os.Getenv("TC9_INTEGRATION_NATS_URL")
	if redisURL == "" || natsURL == "" {
		t.Skip("set TC9_INTEGRATION_REDIS_URL and TC9_INTEGRATION_NATS_URL to run")
	}
	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(redisOptions)
	t.Cleanup(func() { _ = rdb.Close() })
	firstNATS, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	secondNATS, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(secondNATS.Close)

	const (
		realmID   = uint32(4294967293)
		accountID = uint32(987654320)
	)
	logger := zerolog.Nop()
	first := NewSessionOwnershipService(rdb, firstNATS, &logger, "stream-gateway-1", realmID, 3*time.Second)
	second := NewSessionOwnershipService(rdb, secondNATS, &logger, "stream-gateway-2", realmID, 3*time.Second)
	if err = first.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Close)
	if err = second.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer rdb.Del(context.Background(),
		first.accountKey(accountID), first.evictionStreamKey(first.gatewayID),
		second.evictionStreamKey(second.gatewayID),
	)

	evicted := make(chan struct{})
	unregister := first.Register("stream-token-1", func(context.Context) bool { close(evicted); return true })
	defer unregister()
	if err = first.ClaimAccount(ctx, accountID, "stream-token-1"); err != nil {
		t.Fatal(err)
	}
	firstNATS.Close() // force the durable Redis Stream path
	if err = second.ClaimAccount(ctx, accountID, "stream-token-2"); err != nil {
		t.Fatalf("durable takeover claim: %v", err)
	}
	select {
	case <-evicted:
	case <-ctx.Done():
		t.Fatal("Redis Stream did not evict the previous owner")
	}
}

func TestSessionOwnershipIntegrationAccountIsGlobalAcrossRealms(t *testing.T) {
	redisURL := os.Getenv("TC9_INTEGRATION_REDIS_URL")
	natsURL := os.Getenv("TC9_INTEGRATION_NATS_URL")
	if redisURL == "" || natsURL == "" {
		t.Skip("set TC9_INTEGRATION_REDIS_URL and TC9_INTEGRATION_NATS_URL to run")
	}
	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(redisOptions)
	t.Cleanup(func() { _ = rdb.Close() })
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)

	const accountID = uint32(987654319)
	logger := zerolog.Nop()
	first := NewSessionOwnershipService(rdb, nc, &logger, "cross-realm-gateway-1", 1, 3*time.Second)
	second := NewSessionOwnershipService(rdb, nc, &logger, "cross-realm-gateway-2", 2, 3*time.Second)
	if err = first.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Close)
	if err = second.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer rdb.Del(context.Background(), first.accountKey(accountID), first.evictionStreamKey(first.gatewayID))

	evicted := make(chan struct{})
	unregister := first.Register("realm-token-1", func(context.Context) bool { close(evicted); return true })
	defer unregister()
	if err = first.ClaimAccount(ctx, accountID, "realm-token-1"); err != nil {
		t.Fatal(err)
	}
	if err = second.ClaimAccount(ctx, accountID, "realm-token-2"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-evicted:
	case <-ctx.Done():
		t.Fatal("login on another realm did not evict the previous account session")
	}
}

func TestSessionOwnershipIntegrationHeartbeatFailureEvictsAndFailsClosed(t *testing.T) {
	redisURL := os.Getenv("TC9_INTEGRATION_REDIS_URL")
	natsURL := os.Getenv("TC9_INTEGRATION_NATS_URL")
	if redisURL == "" || natsURL == "" {
		t.Skip("set TC9_INTEGRATION_REDIS_URL and TC9_INTEGRATION_NATS_URL to run")
	}
	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(redisOptions)
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)

	logger := zerolog.Nop()
	service := NewSessionOwnershipService(rdb, nc, &logger, "heartbeat-failure-gateway", 1, 3*time.Second)
	if err = service.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)

	evicted := make(chan struct{})
	service.Register("heartbeat-token", func(context.Context) bool { close(evicted); return true })
	if err = rdb.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-evicted:
	case <-time.After(5 * time.Second):
		t.Fatal("sessions were not evicted before the gateway heartbeat TTL")
	}
	if err = service.ClaimAccount(context.Background(), 1, "new-token"); !errors.Is(err, ErrSessionOwnershipUnavailable) {
		t.Fatalf("ClaimAccount after heartbeat failure = %v, want %v", err, ErrSessionOwnershipUnavailable)
	}
}

func TestSessionOwnershipIntegrationRedisStateLossIsFenced(t *testing.T) {
	redisURL := os.Getenv("TC9_INTEGRATION_REDIS_URL")
	natsURL := os.Getenv("TC9_INTEGRATION_NATS_URL")
	if redisURL == "" || natsURL == "" {
		t.Skip("set TC9_INTEGRATION_REDIS_URL and TC9_INTEGRATION_NATS_URL to run")
	}
	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(redisOptions)
	t.Cleanup(func() { _ = rdb.Close() })
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)

	logger := zerolog.Nop()
	first := NewSessionOwnershipService(rdb, nc, &logger, "epoch-gateway-1", 1, 3*time.Second)
	if err = first.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = first.ClaimAccount(ctx, 987654318, "epoch-token-1"); err != nil {
		t.Fatal(err)
	}
	evicted := make(chan struct{})
	first.Register("epoch-token-1", func(context.Context) bool { close(evicted); return true })

	if err = rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	second := NewSessionOwnershipService(rdb, nc, &logger, "epoch-gateway-2", 2, 3*time.Second)
	if err = second.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	if err = second.ClaimAccount(ctx, 987654318, "epoch-token-2"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-evicted:
	default:
		t.Fatal("new ownership was admitted before the previous Redis epoch was evicted")
	}
}

func TestSessionOwnershipIntegrationWaitsForConfirmedEviction(t *testing.T) {
	redisURL := os.Getenv("TC9_INTEGRATION_REDIS_URL")
	natsURL := os.Getenv("TC9_INTEGRATION_NATS_URL")
	if redisURL == "" || natsURL == "" {
		t.Skip("set TC9_INTEGRATION_REDIS_URL and TC9_INTEGRATION_NATS_URL to run")
	}
	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(redisOptions)
	t.Cleanup(func() { _ = rdb.Close() })
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)

	logger := zerolog.Nop()
	first := NewSessionOwnershipService(rdb, nc, &logger, "confirmed-gateway-1", 1, 3*time.Second)
	second := NewSessionOwnershipService(rdb, nc, &logger, "confirmed-gateway-2", 1, 3*time.Second)
	if err = first.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(first.Close)
	if err = second.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const accountID = uint32(987654317)
	if err = first.ClaimAccount(ctx, accountID, "confirmed-token-1"); err != nil {
		t.Fatal(err)
	}
	allowTeardown := make(chan struct{})
	first.Register("confirmed-token-1", func(ctx context.Context) bool {
		select {
		case <-allowTeardown:
			return true
		case <-ctx.Done():
			return false
		}
	})
	claimed := make(chan error, 1)
	go func() { claimed <- second.ClaimAccount(ctx, accountID, "confirmed-token-2") }()
	select {
	case err = <-claimed:
		t.Fatalf("takeover completed before teardown confirmation: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(allowTeardown)
	if err = <-claimed; err != nil {
		t.Fatal(err)
	}
}

func TestSessionOwnershipIntegrationStateLossRejectsClaimBeforeHeartbeat(t *testing.T) {
	redisURL := os.Getenv("TC9_INTEGRATION_REDIS_URL")
	natsURL := os.Getenv("TC9_INTEGRATION_NATS_URL")
	if redisURL == "" || natsURL == "" {
		t.Skip("set TC9_INTEGRATION_REDIS_URL and TC9_INTEGRATION_NATS_URL to run")
	}
	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(redisOptions)
	t.Cleanup(func() { _ = rdb.Close() })
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)

	logger := zerolog.Nop()
	service := NewSessionOwnershipService(rdb, nc, &logger, "epoch-window-gateway", 1, 3*time.Second)
	if err = service.Listen(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = service.ClaimAccount(ctx, 987654316, "epoch-window-token-1"); err != nil {
		t.Fatal(err)
	}
	evicted := make(chan struct{})
	service.Register("epoch-window-token-1", func(context.Context) bool { close(evicted); return true })

	if err = rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	err = service.ClaimAccount(ctx, 987654315, "epoch-window-token-2")
	if !errors.Is(err, ErrSessionOwnershipUnavailable) {
		t.Fatalf("claim immediately after Redis state loss = %v, want %v", err, ErrSessionOwnershipUnavailable)
	}
	select {
	case <-evicted:
	case <-ctx.Done():
		t.Fatal("epoch loss did not fail closed and evict existing sessions")
	}
}

func TestSessionOwnershipIntegrationTimedOutTakeoverKeepsPredecessorLineage(t *testing.T) {
	redisURL := os.Getenv("TC9_INTEGRATION_REDIS_URL")
	natsURL := os.Getenv("TC9_INTEGRATION_NATS_URL")
	if redisURL == "" || natsURL == "" {
		t.Skip("set TC9_INTEGRATION_REDIS_URL and TC9_INTEGRATION_NATS_URL to run")
	}
	redisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(redisOptions)
	t.Cleanup(func() { _ = rdb.Close() })
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)

	logger := zerolog.Nop()
	first := NewSessionOwnershipService(rdb, nc, &logger, "lineage-gateway-1", 1, 3*time.Second)
	second := NewSessionOwnershipService(rdb, nc, &logger, "lineage-gateway-2", 1, 3*time.Second)
	third := NewSessionOwnershipService(rdb, nc, &logger, "lineage-gateway-3", 1, 3*time.Second)
	for _, service := range []*SessionOwnershipService{first, second, third} {
		if err = service.Listen(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(service.Close)
	}
	const accountID = uint32(987654314)
	setupCtx, setupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer setupCancel()
	if err = first.ClaimAccount(setupCtx, accountID, "lineage-token-1"); err != nil {
		t.Fatal(err)
	}
	allowTeardown := make(chan struct{})
	first.Register("lineage-token-1", func(context.Context) bool {
		<-allowTeardown
		return true
	})
	defer close(allowTeardown)

	secondCtx, secondCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	err = second.ClaimAccount(secondCtx, accountID, "lineage-token-2")
	secondCancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed-out takeover = %v, want deadline exceeded", err)
	}
	owner, err := rdb.Get(context.Background(), first.accountKey(accountID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if want := first.owner("lineage-token-1"); owner != want {
		t.Fatalf("owner after timed-out takeover = %q, want predecessor %q", owner, want)
	}

	thirdCtx, thirdCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	err = third.ClaimAccount(thirdCtx, accountID, "lineage-token-3")
	thirdCancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("later takeover bypassed unconfirmed predecessor: %v", err)
	}
	owner, err = rdb.Get(context.Background(), first.accountKey(accountID)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if want := first.owner("lineage-token-1"); owner != want {
		t.Fatalf("owner after later timed-out takeover = %q, want predecessor %q", owner, want)
	}
}
