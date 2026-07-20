package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"sync"

	redis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

type gameServerRedisRepo struct {
	rdb redis.UniversalClient
}

func NewGameServerRedisRepo(rdb redis.UniversalClient) GameServerRepo {
	return &gameServerRedisRepo{rdb: rdb}
}

func (g *gameServerRedisRepo) Upsert(ctx context.Context, server *GameServer) error {
	server.Address = strings.ToLower(server.Address)
	if server.ID == "" {
		server.ID = g.generateID(server.Address)
	}

	d, err := json.Marshal(server)
	if err != nil {
		return err
	}

	key := g.key(server.ID)
	status := g.rdb.Set(ctx, key, d, 0)
	if status.Err() != nil {
		return status.Err()
	}

	res := g.rdb.SAdd(ctx, g.realmIndexKey(server.RealmID, server.IsCrossRealm), key)
	if res.Err() != nil {
		g.rdb.Del(ctx, key)
		return res.Err()
	}

	return nil
}

func (g *gameServerRedisRepo) Update(ctx context.Context, id string, f func(*GameServer) *GameServer) error {
	key := g.key(id)
	for attempt := 0; attempt < 5; attempt++ {
		err := g.rdb.Watch(ctx, func(tx *redis.Tx) error {
			value, err := tx.Get(ctx, key).Bytes()
			if err != nil {
				return err
			}
			server := &GameServer{}
			if err := json.Unmarshal(value, server); err != nil {
				return err
			}
			updated := f(server)
			data, err := json.Marshal(updated)
			if err != nil {
				return err
			}
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, g.key(updated.ID), data, 0)
				return nil
			})
			return err
		}, key)
		if !errors.Is(err, redis.TxFailedErr) {
			return err
		}
	}
	return redis.TxFailedErr
}

func (g *gameServerRedisRepo) Remove(ctx context.Context, id string) error {
	key := g.key(id)
	res := g.rdb.Get(ctx, key)
	if res.Err() != nil {
		if errors.Is(res.Err(), redis.Nil) {
			return nil
		}

		return res.Err()
	}

	v := &GameServer{}
	err := json.Unmarshal([]byte(res.Val()), v)
	if err != nil {
		return err
	}

	delRes := g.rdb.SRem(ctx, g.realmIndexKey(v.RealmID, v.IsCrossRealm), key)
	if delRes.Err() != nil {
		return delRes.Err()
	}

	delRes = g.rdb.Del(ctx, key)
	return delRes.Err()
}

func (g *gameServerRedisRepo) ListByRealm(ctx context.Context, realmID uint32) ([]GameServer, error) {
	return g.listForRealmOrCrossRealm(ctx, realmID, false)
}

func (g *gameServerRedisRepo) ListOfCrossRealms(ctx context.Context) ([]GameServer, error) {
	return g.listForRealmOrCrossRealm(ctx, 0, true)
}

func (g *gameServerRedisRepo) ListAll(ctx context.Context) ([]GameServer, error) {
	pattern := "ws:*"
	var keys []string
	if cluster, ok := g.rdb.(*redis.ClusterClient); ok {
		var mu sync.Mutex
		err := cluster.ForEachMaster(ctx, func(ctx context.Context, client *redis.Client) error {
			masterKeys, err := scanGameServerKeys(ctx, client, pattern)
			if err != nil {
				return err
			}
			mu.Lock()
			keys = append(keys, masterKeys...)
			mu.Unlock()
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		keys, err = scanGameServerKeys(ctx, g.rdb, pattern)
		if err != nil {
			return nil, err
		}
	}
	return g.gameServersForKeys(ctx, keys)
}

type redisScanner interface {
	Scan(context.Context, uint64, string, int64) *redis.ScanCmd
}

func scanGameServerKeys(ctx context.Context, client redisScanner, pattern string) ([]string, error) {
	var cursor uint64
	var keys []string
	for {
		newKeys, next, err := client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, newKeys...)
		cursor = next
		if cursor == 0 {
			return keys, nil
		}
	}
}

func (g *gameServerRedisRepo) listForRealmOrCrossRealm(ctx context.Context, realmID uint32, isCrossRealm bool) ([]GameServer, error) {
	res := g.rdb.SMembers(ctx, g.realmIndexKey(realmID, isCrossRealm))
	if res.Err() != nil {
		return nil, res.Err()
	}

	if len(res.Val()) == 0 {
		return []GameServer{}, nil
	}

	return g.gameServersForKeys(ctx, res.Val())
}

func (g *gameServerRedisRepo) gameServersForKeys(ctx context.Context, keys []string) ([]GameServer, error) {
	pipe := g.rdb.Pipeline()
	commands := make([]*redis.StringCmd, len(keys))
	for i, key := range keys {
		commands[i] = pipe.Get(ctx, key)
	}
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	result := make([]GameServer, 0, len(commands))
	for i, command := range commands {
		value, commandErr := command.Result()
		if errors.Is(commandErr, redis.Nil) {
			log.Warn().Str("key", keys[i]).Msg("fetched nil game server from set")
			continue
		}
		if commandErr != nil {
			return nil, commandErr
		}
		obj := &GameServer{}
		if err := json.Unmarshal([]byte(value), obj); err != nil {
			return nil, err
		}
		result = append(result, *obj)
	}

	return result, nil
}

func (g *gameServerRedisRepo) One(ctx context.Context, id string) (*GameServer, error) {
	getRes := g.rdb.Get(ctx, g.key(id))
	if getRes.Err() != nil {
		if errors.Is(getRes.Err(), redis.Nil) {
			return nil, nil
		}
		return nil, getRes.Err()
	}

	resBytes, err := getRes.Bytes()
	if err != nil {
		return nil, err
	}

	obj := &GameServer{}
	if err = json.Unmarshal(resBytes, obj); err != nil {
		return nil, err
	}

	return obj, nil
}

func (g *gameServerRedisRepo) realmIndexKey(realmID uint32, isCrossRealm bool) string {
	if isCrossRealm {
		return "crossrealm:wss"
	}
	return fmt.Sprintf("realm:%d:wss", realmID)
}

func (g *gameServerRedisRepo) key(id string) string {
	return fmt.Sprintf("ws:%s", id)
}

func (g *gameServerRedisRepo) generateID(address string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(address))
	return strconv.FormatUint(uint64(h.Sum32()), 10)
}
