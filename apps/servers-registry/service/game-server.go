package service

import (
	"context"
	"fmt"
	"math/rand"
	"sort"

	"github.com/rs/zerolog/log"

	"github.com/walkline/ToCloud9/apps/servers-registry/mapbalancing"
	"github.com/walkline/ToCloud9/apps/servers-registry/repo"
	"github.com/walkline/ToCloud9/shared/events"
	"github.com/walkline/ToCloud9/shared/healthandmetrics"
)

type GameServer interface {
	Register(ctx context.Context, server *repo.GameServer) error
	AvailableForMapAndRealm(ctx context.Context, mapID uint32, realmID uint32, isCrossRealm bool) ([]repo.GameServer, error)
	RandomServerForRealm(ctx context.Context, realmID uint32) (*repo.GameServer, error)
	ListForRealm(ctx context.Context, realmID uint32) ([]repo.GameServer, error)
	ListOfCrossRealms(ctx context.Context) ([]repo.GameServer, error)
	ListAll(ctx context.Context) ([]repo.GameServer, error)
	MapsLoadedForServer(ctx context.Context, serverID string, maps []uint32) (*repo.GameServer, error)
	ConfigureLayers(ctx context.Context, realmID uint32, config map[uint32]uint32) error
	ConfigureInstancePool(ctx context.Context, realmID uint32, maps []uint32, replicas uint32) error
}

func (g *gameServerImpl) ConfigureInstancePool(ctx context.Context, realmID uint32, maps []uint32, replicas uint32) error {
	if replicas == 0 {
		return fmt.Errorf("instance pool replicas must be greater than zero")
	}
	g.instanceMaps = append([]uint32(nil), maps...)
	g.instanceReplicas = replicas
	config := map[uint32]uint32{}
	if g.layers != nil {
		var err error
		config, err = g.layers.Configuration(ctx, realmID)
		if err != nil {
			return err
		}
	}
	return g.ConfigureLayers(ctx, realmID, config)
}

func (g *gameServerImpl) ConfigureLayers(ctx context.Context, realmID uint32, config map[uint32]uint32) error {
	if g.layers != nil {
		unlock, err := g.layers.LockRealm(ctx, realmID)
		if err != nil {
			return err
		}
		defer unlock()
	}
	servers, err := g.ListForRealm(ctx, realmID)
	if err != nil {
		return err
	}
	servers, err = g.distributeMapsToServers(ctx, servers)
	if err != nil {
		return err
	}
	before := make(map[string][]uint32, len(servers))
	for i := range servers {
		before[servers[i].ID] = append([]uint32(nil), servers[i].AssignedMapsToHandle...)
	}
	applyLayerAssignments(servers, config)
	applyInstancePoolAssignments(servers, g.instanceMaps, g.instanceReplicas)

	eventServers := make([]events.GameServer, 0, len(servers))
	for i := range servers {
		oldMaps := before[servers[i].ID]
		servers[i].AssignedButPendingMaps = pendingAssignments(servers[i].AssignedButPendingMaps, oldMaps, servers[i].AssignedMapsToHandle)
		assigned := append([]uint32(nil), servers[i].AssignedMapsToHandle...)
		pending := append([]uint32(nil), servers[i].AssignedButPendingMaps...)
		if err := g.r.Update(ctx, servers[i].ID, func(latest *repo.GameServer) *repo.GameServer {
			latest.AssignedMapsToHandle = assigned
			latest.AssignedButPendingMaps = pending
			return latest
		}); err != nil {
			return err
		}
		eventServers = append(eventServers, events.GameServer{
			ID: servers[i].ID, Address: servers[i].Address, RealmID: servers[i].RealmID,
			AvailableMaps: servers[i].AvailableMaps, OldAssignedMapsToHandle: oldMaps,
			NewAssignedMapsToHandle: servers[i].AssignedMapsToHandle,
		})
	}
	if len(eventServers) == 0 {
		return nil
	}
	return g.eProducer.GSMapsReassigned(&events.ServerRegistryEventGSMapsReassignedPayload{Servers: eventServers})
}

func applyInstancePoolAssignments(servers []repo.GameServer, maps []uint32, replicas uint32) {
	for _, mapID := range maps {
		for i := range servers {
			servers[i].AssignedMapsToHandle = removeMap(servers[i].AssignedMapsToHandle, mapID)
		}
		selected := make(map[int]struct{}, replicas)
		for copyIndex := uint32(0); copyIndex < replicas; copyIndex++ {
			candidate := -1
			for i := range servers {
				if _, used := selected[i]; used || !mapAvailable(servers[i], mapID) {
					continue
				}
				if candidate == -1 || instancePoolCandidateLess(servers[i], servers[candidate]) {
					candidate = i
				}
			}
			if candidate == -1 {
				break
			}
			selected[candidate] = struct{}{}
			servers[candidate].AssignedMapsToHandle = append(servers[candidate].AssignedMapsToHandle, mapID)
			sort.Slice(servers[candidate].AssignedMapsToHandle, func(i, j int) bool {
				return servers[candidate].AssignedMapsToHandle[i] < servers[candidate].AssignedMapsToHandle[j]
			})
		}
	}
}

func instancePoolCandidateLess(a, b repo.GameServer) bool {
	// A core with an explicit AvailableMaps list is intended for a specialized
	// workload and is preferred over an all-map outdoor core.
	if a.IsAllMapsAvailable() != b.IsAllMapsAvailable() {
		return !a.IsAllMapsAvailable()
	}
	if len(a.AssignedMapsToHandle) != len(b.AssignedMapsToHandle) {
		return len(a.AssignedMapsToHandle) < len(b.AssignedMapsToHandle)
	}
	return a.ID < b.ID
}

func applyLayerAssignments(servers []repo.GameServer, config map[uint32]uint32) {
	for mapID, count := range config {
		if count < 2 {
			continue
		}
		for i := range servers {
			servers[i].AssignedMapsToHandle = removeMap(servers[i].AssignedMapsToHandle, mapID)
		}
		for layerID := uint32(1); layerID <= count; layerID++ {
			candidate := -1
			for i := range servers {
				if servers[i].LayerID != layerID || !mapAvailable(servers[i], mapID) {
					continue
				}
				if candidate == -1 || len(servers[i].AssignedMapsToHandle) < len(servers[candidate].AssignedMapsToHandle) ||
					(len(servers[i].AssignedMapsToHandle) == len(servers[candidate].AssignedMapsToHandle) && servers[i].ID < servers[candidate].ID) {
					candidate = i
				}
			}
			if candidate >= 0 {
				servers[candidate].AssignedMapsToHandle = append(servers[candidate].AssignedMapsToHandle, mapID)
				sort.Slice(servers[candidate].AssignedMapsToHandle, func(i, j int) bool {
					return servers[candidate].AssignedMapsToHandle[i] < servers[candidate].AssignedMapsToHandle[j]
				})
			}
		}
	}
}

func mapAvailable(server repo.GameServer, mapID uint32) bool {
	if server.IsAllMapsAvailable() {
		return true
	}
	for _, available := range server.AvailableMaps {
		if available == mapID {
			return true
		}
	}
	return false
}

func removeMap(maps []uint32, mapID uint32) []uint32 {
	result := maps[:0]
	for _, assigned := range maps {
		if assigned != mapID {
			result = append(result, assigned)
		}
	}
	return result
}

func newlyAssigned(oldMaps, newMaps []uint32) []uint32 {
	old := make(map[uint32]struct{}, len(oldMaps))
	for _, mapID := range oldMaps {
		old[mapID] = struct{}{}
	}
	var result []uint32
	for _, mapID := range newMaps {
		if _, exists := old[mapID]; !exists {
			result = append(result, mapID)
		}
	}
	return result
}

func pendingAssignments(existing, oldMaps, newMaps []uint32) []uint32 {
	newSet := make(map[uint32]struct{}, len(newMaps))
	for _, mapID := range newMaps {
		newSet[mapID] = struct{}{}
	}
	result := make([]uint32, 0, len(existing)+len(newMaps))
	seen := make(map[uint32]struct{})
	for _, mapID := range existing {
		if _, retained := newSet[mapID]; retained {
			result = append(result, mapID)
			seen[mapID] = struct{}{}
		}
	}
	for _, mapID := range newlyAssigned(oldMaps, newMaps) {
		if _, exists := seen[mapID]; !exists {
			result = append(result, mapID)
		}
	}
	return result
}

type gameServerImpl struct {
	r                repo.GameServerRepo
	checker          healthandmetrics.HealthChecker
	metrics          healthandmetrics.MetricsConsumer
	mapBalancer      mapbalancing.MapDistributor
	eProducer        events.ServerRegistryProducer
	layers           repo.LayerStore
	instanceMaps     []uint32
	instanceReplicas uint32
}

func NewGameServer(
	ctx context.Context,
	r repo.GameServerRepo,
	checker healthandmetrics.HealthChecker,
	metrics healthandmetrics.MetricsConsumer,
	mapBalancer mapbalancing.MapDistributor,
	eProducer events.ServerRegistryProducer,
	layers repo.LayerStore,
	supportedRealmIDs []uint32,
) (GameServer, error) {
	service := &gameServerImpl{
		r:           r,
		checker:     checker,
		metrics:     metrics,
		mapBalancer: mapBalancer,
		eProducer:   eProducer,
		layers:      layers,
	}

	checker.AddFailedObserver(func(object healthandmetrics.HealthCheckObject, err error) {
		if gs, ok := object.(*repo.GameServer); ok {
			service.onServerUnhealthy(gs, err)
		}
	})

	metrics.AddObserver(func(observable healthandmetrics.MetricsObservable, read *healthandmetrics.MetricsRead) {
		if gs, ok := observable.(*repo.GameServer); ok {
			service.onMetricsUpdate(gs, read)
		}
	})

	for _, id := range supportedRealmIDs {
		servers, err := r.ListByRealm(ctx, id)
		if err != nil {
			return nil, err
		}

		for i := range servers {
			if err = checker.AddHealthCheckObject(&servers[i]); err != nil {
				return nil, err
			}

			err = metrics.AddMetricsObservable(&servers[i])
			if err != nil {
				return nil, err
			}
		}
	}

	servers, err := r.ListOfCrossRealms(ctx)
	if err != nil {
		return nil, err
	}

	for i := range servers {
		if err = checker.AddHealthCheckObject(&servers[i]); err != nil {
			return nil, err
		}

		err = metrics.AddMetricsObservable(&servers[i])
		if err != nil {
			return nil, err
		}
	}

	return service, nil
}

func (g *gameServerImpl) Register(ctx context.Context, server *repo.GameServer) error {
	sort.Slice(server.AvailableMaps, func(i, j int) bool {
		return server.AvailableMaps[i] <= server.AvailableMaps[j]
	})

	if err := g.checker.AddHealthCheckObject(server); err != nil {
		return err
	}

	if err := g.metrics.AddMetricsObservable(server); err != nil {
		return err
	}

	if err := g.r.Upsert(ctx, server); err != nil {
		return err
	}

	var wsList []repo.GameServer
	var err error

	if server.IsCrossRealm {
		wsList, err = g.ListOfCrossRealms(ctx)
	} else {
		wsList, err = g.ListForRealm(ctx, server.RealmID)
	}

	if err != nil {
		return err
	}

	res, err := g.distributeMapsToServers(ctx, wsList)
	if err != nil {
		return fmt.Errorf("failed to register game server during maps ditribution, err: %w", err)
	}

	for _, gameServer := range res {
		if gameServer.ID == server.ID {
			server.AssignedMapsToHandle = gameServer.AssignedMapsToHandle
			break
		}
	}

	err = g.eProducer.GSAdded(&events.ServerRegistryEventGSAddedPayload{
		GameServer: events.GameServer{
			ID:                      server.ID,
			Address:                 server.Address,
			RealmID:                 server.RealmID,
			IsCrossRealm:            server.IsCrossRealm,
			AvailableMaps:           server.AvailableMaps,
			OldAssignedMapsToHandle: []uint32{},
			NewAssignedMapsToHandle: server.AssignedMapsToHandle,
		},
	})
	if err != nil {
		log.Error().Err(err).Str("serverID", server.ID).Msg("can't produce game server added event")
	}

	return nil
}

func (g *gameServerImpl) AvailableForMapAndRealm(ctx context.Context, mapID uint32, realmID uint32, isCrossRealm bool) ([]repo.GameServer, error) {
	var (
		servers []repo.GameServer
		err     error
	)

	if isCrossRealm {
		servers, err = g.r.ListOfCrossRealms(ctx)
	} else {
		servers, err = g.r.ListByRealm(ctx, realmID)
	}
	if err != nil {
		return nil, err
	}

	result := []repo.GameServer{}
	for _, server := range servers {
		if server.CanHandleMap(mapID) {
			result = append(result, server)
		}
	}

	return result, nil
}

func (g *gameServerImpl) RandomServerForRealm(ctx context.Context, realmID uint32) (*repo.GameServer, error) {
	servers, err := g.r.ListByRealm(ctx, realmID)
	if err != nil {
		return nil, err
	}

	if len(servers) == 0 {
		return nil, nil
	}

	return &servers[rand.Intn(len(servers))], nil
}

func (g *gameServerImpl) ListForRealm(ctx context.Context, realmID uint32) ([]repo.GameServer, error) {
	servers, err := g.r.ListByRealm(ctx, realmID)
	if err != nil {
		return nil, err
	}

	return servers, nil
}

func (g *gameServerImpl) ListAll(ctx context.Context) ([]repo.GameServer, error) {
	servers, err := g.r.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	return servers, nil
}

func (g *gameServerImpl) ListOfCrossRealms(ctx context.Context) ([]repo.GameServer, error) {
	servers, err := g.r.ListOfCrossRealms(ctx)
	if err != nil {
		return nil, err
	}

	return servers, nil
}

func (g *gameServerImpl) MapsLoadedForServer(ctx context.Context, serverID string, maps []uint32) (*repo.GameServer, error) {
	server, err := g.r.One(ctx, serverID)
	if err != nil {
		return nil, err
	}

	if server == nil {
		return nil, fmt.Errorf("game server not found")
	}

	newPendingMaps := []uint32{}
	for i := range server.AssignedButPendingMaps {
		hasMap := false
		for j := range maps {
			if server.AssignedButPendingMaps[i] == maps[j] {
				hasMap = true
				break
			}
		}
		if !hasMap {
			newPendingMaps = append(newPendingMaps, server.AssignedButPendingMaps[i])
		}
	}

	server.AssignedButPendingMaps = newPendingMaps

	return server, g.r.Upsert(ctx, server)
}

func (g *gameServerImpl) onServerUnhealthy(server *repo.GameServer, err error) {
	log.Warn().
		Err(err).
		Str("address", server.Address).
		Msg("Game Server unhealthy! Removing...")

	err = g.r.Remove(context.Background(), server.ID)
	if err != nil {
		log.Error().Err(err).Msg("can't remove server")
		return
	}

	err = g.eProducer.GSRemoved(&events.ServerRegistryEventGSRemovedPayload{
		GameServer: events.GameServer{
			ID:                      server.ID,
			Address:                 server.Address,
			RealmID:                 server.RealmID,
			IsCrossRealm:            server.IsCrossRealm,
			AvailableMaps:           server.AvailableMaps,
			OldAssignedMapsToHandle: server.AssignedMapsToHandle,
			NewAssignedMapsToHandle: server.AssignedMapsToHandle,
		},
	})
	if err != nil {
		log.Error().Err(err).Str("serverID", server.ID).Msg("can't produce game server removed event")
	}

	err = g.metrics.RemoveMetricsObservable(server)
	if err != nil {
		log.Error().Err(err).Msg("can't remove gameserver from metrics consumer")
	}

	var wsList []repo.GameServer

	if server.IsCrossRealm {
		wsList, err = g.ListOfCrossRealms(context.Background())
	} else {
		wsList, err = g.ListForRealm(context.Background(), server.RealmID)
	}

	if err != nil {
		log.Error().Err(err).Msg("can't list servers")
		return
	}

	_, err = g.distributeMapsToServers(context.Background(), wsList)
	if err != nil {
		log.Error().Err(err).Msg("couldn't distribute maps to servers")
		return
	}
	if !server.IsCrossRealm && g.layers != nil {
		config, configErr := g.layers.Configuration(context.Background(), server.RealmID)
		if configErr != nil {
			log.Error().Err(configErr).Msg("can't read layer configuration")
			return
		}
		if configErr = g.ConfigureLayers(context.Background(), server.RealmID, config); configErr != nil {
			log.Error().Err(configErr).Msg("can't restore layered map distribution")
		}
	}
}

func (g *gameServerImpl) distributeMapsToServers(ctx context.Context, servers []repo.GameServer) ([]repo.GameServer, error) {
	serversBefore := make([]repo.GameServer, len(servers))
	for i, server := range servers {
		serversBefore[i] = server.Copy()
	}

	distributed := g.mapBalancer.Distribute(servers)

	res := make([]events.GameServer, len(distributed))
	for i := range distributed {
		res[i] = events.GameServer{
			ID:                      distributed[i].ID,
			Address:                 distributed[i].Address,
			RealmID:                 distributed[i].RealmID,
			AvailableMaps:           distributed[i].AvailableMaps,
			NewAssignedMapsToHandle: distributed[i].AssignedMapsToHandle,
		}

		for _, server := range serversBefore {
			if server.ID == distributed[i].ID {
				res[i].OldAssignedMapsToHandle = server.AssignedMapsToHandle
				break
			}
		}
	}

	for i := range distributed {
		// Mark new maps as pending.
		for _, server := range res {
			if server.ID == distributed[i].ID {
				// No need to have confirmation for assignment on startup.
				if len(server.OldAssignedMapsToHandle) > 0 {
					distributed[i].AssignedButPendingMaps = server.OnlyNewMaps()
				}
				break
			}
		}

		assigned := append([]uint32(nil), distributed[i].AssignedMapsToHandle...)
		pending := append([]uint32(nil), distributed[i].AssignedButPendingMaps...)
		if err := g.r.Update(ctx, distributed[i].ID, func(latest *repo.GameServer) *repo.GameServer {
			latest.AssignedMapsToHandle = assigned
			latest.AssignedButPendingMaps = pending
			return latest
		}); err != nil {
			return nil, err
		}
	}

	err := g.eProducer.GSMapsReassigned(&events.ServerRegistryEventGSMapsReassignedPayload{
		Servers: res,
	})
	if err != nil {
		return nil, fmt.Errorf("can't send event for maps reaasigned, err %w", err)
	}

	return distributed, nil
}

func (g *gameServerImpl) onMetricsUpdate(server *repo.GameServer, m *healthandmetrics.MetricsRead) {
	err := g.r.Update(context.Background(), server.ID, func(s *repo.GameServer) *repo.GameServer {
		s.ActiveConnections = uint32(m.ActiveConnections)
		s.Diff.Mean = uint32(m.DelayMean)
		s.Diff.Median = uint32(m.DelayMedian)
		s.Diff.Percentile99 = uint32(m.Delay99Percentile)
		s.Diff.Percentile95 = uint32(m.Delay95Percentile)
		s.Diff.Max = uint32(m.DelayMax)
		return s
	})
	if err != nil {
		log.Error().Err(err).Msg("can't update metrics for game server")
	}
}
