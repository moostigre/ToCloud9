package service

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/walkline/ToCloud9/apps/servers-registry/repo"
)

type portalStoreStub struct {
	mu           sync.Mutex
	destinations map[uint32]uint32
	placements   map[string]string
}

func (s *portalStoreStub) InstanceMaps(_ context.Context) ([]uint32, error)        { return nil, nil }
func (s *portalStoreStub) ReplaceInstanceMaps(_ context.Context, _ []uint32) error { return nil }

func newPortalStoreStub() *portalStoreStub {
	return &portalStoreStub{destinations: map[uint32]uint32{}, placements: map[string]string{}}
}
func (s *portalStoreStub) DestinationMap(_ context.Context, triggerID uint32) (uint32, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mapID, found := s.destinations[triggerID]
	return mapID, found, nil
}
func (s *portalStoreStub) ReplaceDestinations(_ context.Context, value map[uint32]uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.destinations = value
	return nil
}
func (s *portalStoreStub) Placement(_ context.Context, realm uint32, ownerType string, ownerID uint64, mapID uint32) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.placements[portalPlacementTestKey(realm, ownerType, ownerID, mapID)], nil
}
func (s *portalStoreStub) Placements(ctx context.Context, realm uint32, ownerType string, ownerID uint64, mapIDs []uint32) (map[uint32]string, error) {
	result := make(map[uint32]string, len(mapIDs))
	for _, mapID := range mapIDs {
		value, err := s.Placement(ctx, realm, ownerType, ownerID, mapID)
		if err != nil {
			return nil, err
		}
		if value != "" {
			result[mapID] = value
		}
	}
	return result, nil
}
func (s *portalStoreStub) BindPlacement(_ context.Context, realm uint32, ownerType string, ownerID uint64, mapID uint32, server string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := portalPlacementTestKey(realm, ownerType, ownerID, mapID)
	if s.placements[key] == "" {
		s.placements[key] = server
	}
	return s.placements[key], nil
}
func (s *portalStoreStub) ReplacePlacement(_ context.Context, realm uint32, ownerType string, ownerID uint64, mapID uint32, stale, server string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := portalPlacementTestKey(realm, ownerType, ownerID, mapID)
	if s.placements[key] == stale {
		s.placements[key] = server
	}
	return s.placements[key], nil
}
func (s *portalStoreStub) SetPlacement(_ context.Context, realm uint32, ownerType string, ownerID uint64, mapID uint32, server string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.placements[portalPlacementTestKey(realm, ownerType, ownerID, mapID)] = server
	return nil
}
func (s *portalStoreStub) DeletePlacement(_ context.Context, realm uint32, ownerType string, ownerID uint64, mapID uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.placements, portalPlacementTestKey(realm, ownerType, ownerID, mapID))
	return nil
}
func (s *portalStoreStub) DeletePlacements(ctx context.Context, realm uint32, ownerType string, ownerIDs []uint64, mapID uint32) error {
	for _, ownerID := range ownerIDs {
		if err := s.DeletePlacement(ctx, realm, ownerType, ownerID, mapID); err != nil {
			return err
		}
	}
	return nil
}
func portalPlacementTestKey(realm uint32, ownerType string, ownerID uint64, mapID uint32) string {
	return fmt.Sprintf("%d:%s:%d:%d", realm, ownerType, ownerID, mapID)
}

func TestPortalSelectionIsSharedAcrossRegistryReplicas(t *testing.T) {
	portalStore := newPortalStoreStub()
	portalStore.destinations[2230] = 389
	layerStore := newLayerStoreStub()
	servers := &layerServersStub{servers: []repo.GameServer{
		{ID: "instance-a", ActiveConnections: 3},
		{ID: "instance-b", ActiveConnections: 1},
	}}
	layers := NewLayer(servers, layerStore)
	instances := NewInstancePool(servers, portalStore, []uint32{389})
	replicaA, replicaB := NewPortal(servers, layers, instances, portalStore), NewPortal(servers, layers, instances, portalStore)

	var first, second PortalSelection
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); first, _ = replicaA.Select(context.Background(), 1, 10, 77, 2230) }()
	go func() { defer wg.Done(); second, _ = replicaB.Select(context.Background(), 1, 11, 77, 2230) }()
	wg.Wait()

	require.Equal(t, PortalSelectionOK, first.Status)
	require.Equal(t, first.Server.ID, second.Server.ID)
	require.Equal(t, "instance-b", first.Server.ID)
}

func TestPortalSelectionKeepsSoloCharacterOnOwningCore(t *testing.T) {
	portalStore := newPortalStoreStub()
	portalStore.destinations[2230] = 389
	layerStore := newLayerStoreStub()
	servers := &layerServersStub{servers: []repo.GameServer{
		{ID: "instance-a", ActiveConnections: 0},
		{ID: "instance-b", ActiveConnections: 2},
	}}
	instances := NewInstancePool(servers, portalStore, []uint32{389})
	portal := NewPortal(servers, NewLayer(servers, layerStore), instances, portalStore)

	first, err := portal.Select(context.Background(), 1, 10, 0, 2230)
	require.NoError(t, err)
	servers.servers[0].ActiveConnections = 9
	servers.servers[1].ActiveConnections = 0
	second, err := portal.Select(context.Background(), 1, 10, 0, 2230)
	require.NoError(t, err)

	require.Equal(t, "instance-a", first.Server.ID)
	require.Equal(t, first.Server.ID, second.Server.ID)
}

func TestInstanceGroupSelectionAlsoPreservesCharacterAffinity(t *testing.T) {
	store := newPortalStoreStub()
	servers := &layerServersStub{servers: []repo.GameServer{{ID: "instance-a"}, {ID: "instance-b", ActiveConnections: 2}}}
	pool := NewInstancePool(servers, store, []uint32{389})

	grouped, err := pool.Select(context.Background(), 1, 10, 77, 389)
	require.NoError(t, err)
	solo, err := pool.Select(context.Background(), 1, 10, 0, 389)
	require.NoError(t, err)

	require.Equal(t, grouped.Server.ID, solo.Server.ID)
	require.Equal(t, grouped.Server.ID, store.placements[portalPlacementTestKey(1, "character", 10, 389)])
}

func TestNewInstanceGroupInheritsExistingCharacterOwner(t *testing.T) {
	store := newPortalStoreStub()
	store.placements[portalPlacementTestKey(1, "character", 10, 389)] = "instance-b"
	servers := &layerServersStub{servers: []repo.GameServer{{ID: "instance-a"}, {ID: "instance-b", ActiveConnections: 9}}}
	pool := NewInstancePool(servers, store, []uint32{389})

	selection, err := pool.Select(context.Background(), 1, 10, 77, 389)
	require.NoError(t, err)

	require.Equal(t, "instance-b", selection.Server.ID)
	require.Equal(t, "instance-b", store.placements[portalPlacementTestKey(1, "group", 77, 389)])
}

func TestInstanceResetClearsPlacementForEveryPartyMember(t *testing.T) {
	store := newPortalStoreStub()
	store.placements[portalPlacementTestKey(1, "group", 77, 389)] = "instance-a"
	store.placements[portalPlacementTestKey(1, "character", 10, 389)] = "instance-a"
	servers := &layerServersStub{servers: []repo.GameServer{
		{ID: "instance-a", ActiveConnections: 0},
		{ID: "instance-b", ActiveConnections: 4},
	}}
	pool := NewInstancePool(servers, store, []uint32{389})

	store.placements[portalPlacementTestKey(1, "character", 11, 389)] = "instance-a"
	require.NoError(t, pool.FinalizeReset(context.Background(), 1, 10, 77, 389, []uint64{10, 11}))
	require.Empty(t, store.placements[portalPlacementTestKey(1, "group", 77, 389)])
	require.Empty(t, store.placements[portalPlacementTestKey(1, "character", 10, 389)])
	require.Empty(t, store.placements[portalPlacementTestKey(1, "character", 11, 389)])
}

func TestInstanceResetTargetsResolveSharedRedisOwner(t *testing.T) {
	store := newPortalStoreStub()
	store.placements[portalPlacementTestKey(1, "character", 10, 389)] = "instance-b"
	servers := &layerServersStub{servers: []repo.GameServer{{ID: "instance-a"}, {ID: "instance-b", Address: "b:9601"}}}
	pool := NewInstancePool(servers, store, []uint32{389})

	targets, err := pool.ResetTargets(context.Background(), 1, 10, 0)
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, uint32(389), targets[0].MapID)
	require.Equal(t, "instance-b", targets[0].Server.ID)
}

func TestInstanceResetTargetsIncludeEveryOwnedInstanceMap(t *testing.T) {
	store := newPortalStoreStub()
	store.placements[portalPlacementTestKey(1, "group", 77, 33)] = "instance-a"
	store.placements[portalPlacementTestKey(1, "group", 77, 389)] = "instance-b"
	servers := &layerServersStub{servers: []repo.GameServer{
		{ID: "instance-a", Address: "a:9601"},
		{ID: "instance-b", Address: "b:9601"},
	}}
	pool := NewInstancePool(servers, store, []uint32{33, 389})

	targets, err := pool.ResetTargets(context.Background(), 1, 10, 77)
	require.NoError(t, err)
	require.Len(t, targets, 2)
	owners := map[uint32]string{targets[0].MapID: targets[0].Server.ID, targets[1].MapID: targets[1].Server.ID}
	require.Equal(t, "instance-a", owners[33])
	require.Equal(t, "instance-b", owners[389])
}

func TestPortalSelectionForUnknownTriggerDoesNotChooseServer(t *testing.T) {
	store := newPortalStoreStub()
	servers := &layerServersStub{}
	selection, err := NewPortal(servers, NewLayer(servers, newLayerStoreStub()), NewInstancePool(servers, store, []uint32{389}), store).
		Select(context.Background(), 1, 10, 0, 999)
	require.NoError(t, err)
	require.Equal(t, PortalSelectionTriggerNotFound, selection.Status)
}
