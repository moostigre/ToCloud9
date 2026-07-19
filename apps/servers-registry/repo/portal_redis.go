package repo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	redis "github.com/redis/go-redis/v9"
)

const portalPlacementTTL = 24 * time.Hour

type portalRedisStore struct {
	rdb            redis.UniversalClient
	catalogVersion string
}

func NewPortalRedisStore(rdb redis.UniversalClient, catalogVersion string) PortalStore {
	return &portalRedisStore{rdb: rdb, catalogVersion: catalogVersion}
}

func (s *portalRedisStore) DestinationMap(ctx context.Context, triggerID uint32) (uint32, bool, error) {
	value, err := s.rdb.HGet(ctx, s.catalogKey(), strconv.FormatUint(uint64(triggerID), 10)).Uint64()
	if errors.Is(err, redis.Nil) {
		exists, existsErr := s.rdb.Exists(ctx, s.catalogKey()).Result()
		if existsErr != nil {
			return 0, false, existsErr
		}
		if exists == 0 {
			return 0, false, errors.New("area-trigger destination catalog is unavailable")
		}
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return uint32(value), true, nil
}

func (s *portalRedisStore) ReplaceDestinations(ctx context.Context, destinations map[uint32]uint32) error {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return err
	}
	temporaryKey := s.catalogKey() + ":tmp:" + hex.EncodeToString(tokenBytes)
	values := make(map[string]any, len(destinations))
	for triggerID, mapID := range destinations {
		values[strconv.FormatUint(uint64(triggerID), 10)] = mapID
	}
	if len(values) == 0 {
		return errors.New("area-trigger destination catalog is empty")
	}
	if err := s.rdb.HSet(ctx, temporaryKey, values).Err(); err != nil {
		return err
	}
	if err := s.rdb.Rename(ctx, temporaryKey, s.catalogKey()).Err(); err != nil {
		_ = s.rdb.Del(context.Background(), temporaryKey).Err()
		return err
	}
	return nil
}

func (s *portalRedisStore) InstanceMaps(ctx context.Context) ([]uint32, error) {
	values, err := s.rdb.SMembers(ctx, s.instanceMapsKey()).Result()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, errors.New("instance map catalog is unavailable")
	}
	maps := make([]uint32, 0, len(values))
	for _, value := range values {
		mapID, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return nil, err
		}
		maps = append(maps, uint32(mapID))
	}
	return maps, nil
}

func (s *portalRedisStore) ReplaceInstanceMaps(ctx context.Context, maps []uint32) error {
	if len(maps) == 0 {
		return errors.New("instance map catalog is empty")
	}
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return err
	}
	temporaryKey := s.instanceMapsKey() + ":tmp:" + hex.EncodeToString(tokenBytes)
	values := make([]any, 0, len(maps))
	for _, mapID := range maps {
		values = append(values, mapID)
	}
	if err := s.rdb.SAdd(ctx, temporaryKey, values...).Err(); err != nil {
		return err
	}
	if err := s.rdb.Rename(ctx, temporaryKey, s.instanceMapsKey()).Err(); err != nil {
		_ = s.rdb.Del(context.Background(), temporaryKey).Err()
		return err
	}
	return nil
}

func (s *portalRedisStore) Placement(ctx context.Context, realmID uint32, ownerType string, ownerID uint64, mapID uint32) (string, error) {
	key := s.placementKey(realmID, ownerType, ownerID, mapID)
	value, err := s.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err == nil {
		_ = s.rdb.Expire(ctx, key, portalPlacementTTL).Err()
	}
	return value, err
}

func (s *portalRedisStore) BindPlacement(ctx context.Context, realmID uint32, ownerType string, ownerID uint64, mapID uint32, serverID string) (string, error) {
	key := s.placementKey(realmID, ownerType, ownerID, mapID)
	created, err := s.rdb.SetNX(ctx, key, serverID, portalPlacementTTL).Result()
	if err != nil {
		return "", err
	}
	if created {
		return serverID, nil
	}
	return s.rdb.Get(ctx, key).Result()
}

func (s *portalRedisStore) ReplacePlacement(ctx context.Context, realmID uint32, ownerType string, ownerID uint64, mapID uint32, staleServerID, serverID string) (string, error) {
	const compareAndSet = `
local current = redis.call('GET', KEYS[1])
if current == ARGV[1] then
  redis.call('SET', KEYS[1], ARGV[2], 'PX', ARGV[3])
  return ARGV[2]
end
return current or ''`
	return s.rdb.Eval(ctx, compareAndSet, []string{s.placementKey(realmID, ownerType, ownerID, mapID)}, staleServerID, serverID, portalPlacementTTL.Milliseconds()).Text()
}

func (s *portalRedisStore) SetPlacement(ctx context.Context, realmID uint32, ownerType string, ownerID uint64, mapID uint32, serverID string) error {
	return s.rdb.Set(ctx, s.placementKey(realmID, ownerType, ownerID, mapID), serverID, portalPlacementTTL).Err()
}

func (s *portalRedisStore) catalogKey() string {
	// The hash tag keeps temporary and final catalog keys in one Redis Cluster slot.
	return fmt.Sprintf("tc9:portal:{catalog:%s}:destinations", s.catalogVersion)
}

func (s *portalRedisStore) instanceMapsKey() string {
	return fmt.Sprintf("tc9:portal:{catalog:%s}:instance-maps", s.catalogVersion)
}

func (*portalRedisStore) placementKey(realmID uint32, ownerType string, ownerID uint64, mapID uint32) string {
	return fmt.Sprintf("tc9:portal:{placement:%d:%s:%d:%d}:server", realmID, ownerType, ownerID, mapID)
}
