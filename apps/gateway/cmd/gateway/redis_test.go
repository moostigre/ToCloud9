package main

import (
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

func TestNewRedisClientSelectsConfiguredTopology(t *testing.T) {
	standalone, err := newRedisClient("redis://localhost:6379/0", false)
	if err != nil {
		t.Fatal(err)
	}
	defer standalone.Close()
	standaloneClient, ok := standalone.(*redis.Client)
	if !ok {
		t.Fatalf("standalone client has type %T, want *redis.Client", standalone)
	}
	// NewClient normalizes -1 (disable retries) to the runtime value 0.
	if !standaloneClient.Options().ContextTimeoutEnabled || standaloneClient.Options().MaxRetries != 0 || standaloneClient.Options().ReadTimeout != time.Second {
		t.Fatalf("standalone ownership client does not have bounded, context-aware I/O: %+v", standaloneClient.Options())
	}

	cluster, err := newRedisClient("redis://localhost:6379?addr=localhost:6380", true)
	if err != nil {
		t.Fatal(err)
	}
	defer cluster.Close()
	clusterClient, ok := cluster.(*redis.ClusterClient)
	if !ok {
		t.Fatalf("cluster client has type %T, want *redis.ClusterClient", cluster)
	}
	if got := len(clusterClient.Options().Addrs); got != 2 {
		t.Fatalf("cluster bootstrap address count = %d, want 2", got)
	}
	if !clusterClient.Options().ContextTimeoutEnabled || clusterClient.Options().MaxRetries != -1 || clusterClient.Options().ReadTimeout != time.Second {
		t.Fatalf("cluster ownership client does not have bounded, context-aware I/O: %+v", clusterClient.Options())
	}
}
