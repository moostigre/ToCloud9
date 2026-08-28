package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestSessionOwnershipFailsClosedBeforeListen(t *testing.T) {
	service := NewSessionOwnershipService(nil, nil, nil, "gateway", 7, 15*time.Second)
	err := service.ClaimAccount(context.Background(), 12, "token")
	if !errors.Is(err, ErrSessionOwnershipUnavailable) {
		t.Fatalf("ClaimAccount error = %v, want %v", err, ErrSessionOwnershipUnavailable)
	}
}

func TestParseSessionOwner(t *testing.T) {
	gatewayID, token, ok := parseSessionOwner("gateway-1|token-1")
	if !ok || gatewayID != "gateway-1" || token != "token-1" {
		t.Fatalf("unexpected parsed owner: gateway=%q token=%q ok=%v", gatewayID, token, ok)
	}
	for _, invalid := range []string{"", "gateway", "|token", "gateway|"} {
		if _, _, ok := parseSessionOwner(invalid); ok {
			t.Fatalf("expected %q to be rejected", invalid)
		}
	}
}

func TestSessionOwnershipKeysUseGlobalAccountsAndRealmQualifiedCharacters(t *testing.T) {
	service := NewSessionOwnershipService(nil, nil, nil, "gateway", 7, 15*time.Second)
	if got, want := service.accountKey(12), "{gateway-session:global}:owner:account:12"; got != want {
		t.Fatalf("account key = %q, want %q", got, want)
	}
	if got, want := service.characterKey(34), "{gateway-session:global}:owner:character:7:34"; got != want {
		t.Fatalf("character key = %q, want %q", got, want)
	}
	for _, key := range []string{
		service.accountKey(12), service.characterKey(34), service.evictionStreamKey("other"),
		service.gatewayLivenessKey("other"), service.evictionAckKey("token", 0),
		service.redisEpochKey(), service.recoveryFenceKey(),
	} {
		if !strings.HasPrefix(key, "{gateway-session:global}:") {
			t.Fatalf("key %q does not share the global Redis Cluster hash tag", key)
		}
	}
}

func TestScriptInt64(t *testing.T) {
	for _, test := range []struct {
		value any
		want  int64
	}{{int64(1), 1}, {"0", 0}} {
		got, err := scriptInt64([]any{test.value}, 0)
		if err != nil || got != test.want {
			t.Fatalf("scriptInt64(%v) = %d, %v; want %d", test.value, got, err, test.want)
		}
	}
	if _, err := scriptInt64(nil, 0); err == nil {
		t.Fatal("scriptInt64 accepted an empty response")
	}
}

func TestHeartbeatWatchdogFailsClosedWithoutWaitingForRedisIO(t *testing.T) {
	logger := zerolog.Nop()
	service := NewSessionOwnershipService(nil, nil, &logger, "watchdog-gateway", 1, 100*time.Millisecond)
	service.healthy.Store(true)
	service.heartbeat.Store(time.Now().UnixNano())
	evicted := make(chan struct{})
	service.Register("watchdog-token", func(context.Context) bool {
		close(evicted)
		return true
	})
	service.wg.Add(1)
	go service.runHeartbeatWatchdog()

	select {
	case <-evicted:
	case <-time.After(time.Second):
		t.Fatal("independent heartbeat watchdog did not start session teardown")
	}
	select {
	case <-service.Done():
	case <-time.After(time.Second):
		t.Fatal("heartbeat watchdog did not fail the gateway closed")
	}
	service.wg.Wait()
}
