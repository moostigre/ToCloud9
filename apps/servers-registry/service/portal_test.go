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
	replicaA, replicaB := NewPortal(servers, layers, portalStore), NewPortal(servers, layers, portalStore)

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
	portal := NewPortal(servers, NewLayer(servers, layerStore), portalStore)

	first, err := portal.Select(context.Background(), 1, 10, 0, 2230)
	require.NoError(t, err)
	servers.servers[0].ActiveConnections = 9
	servers.servers[1].ActiveConnections = 0
	second, err := portal.Select(context.Background(), 1, 10, 0, 2230)
	require.NoError(t, err)

	require.Equal(t, "instance-a", first.Server.ID)
	require.Equal(t, first.Server.ID, second.Server.ID)
}

func TestPortalSelectionForUnknownTriggerDoesNotChooseServer(t *testing.T) {
	store := newPortalStoreStub()
	servers := &layerServersStub{}
	selection, err := NewPortal(servers, NewLayer(servers, newLayerStoreStub()), store).
		Select(context.Background(), 1, 10, 0, 999)
	require.NoError(t, err)
	require.Equal(t, PortalSelectionTriggerNotFound, selection.Status)
}
