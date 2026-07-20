package server

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/peer"

	"github.com/walkline/ToCloud9/apps/servers-registry/repo"
	"github.com/walkline/ToCloud9/apps/servers-registry/service"
	"github.com/walkline/ToCloud9/gen/servers-registry/pb"
)

const ver = "0.0.1"

type serversRegistry struct {
	pb.UnimplementedServersRegistryServiceServer
	gService  service.GameServer
	lbService service.Gateway
	layer     service.Layer
	instances service.InstancePool
	portal    service.Portal
}

func NewServersRegistry(gService service.GameServer, lbService service.Gateway, layer service.Layer, instances service.InstancePool, portal service.Portal) pb.ServersRegistryServiceServer {
	return &serversRegistry{
		gService:  gService,
		lbService: lbService,
		layer:     layer,
		instances: instances,
		portal:    portal,
	}
}

func (s *serversRegistry) SelectGameServerForAreaTrigger(ctx context.Context, request *pb.SelectGameServerForAreaTriggerRequest) (*pb.SelectGameServerForAreaTriggerResponse, error) {
	selection, err := s.portal.Select(ctx, request.RealmID, request.CharacterGUID, request.GroupID, request.AreaTriggerID)
	if err != nil {
		return nil, err
	}
	response := &pb.SelectGameServerForAreaTriggerResponse{Api: ver, DestinationMapID: selection.DestinationMapID}
	switch selection.Status {
	case service.PortalSelectionTriggerNotFound:
		response.Status = pb.SelectGameServerForAreaTriggerResponse_TRIGGER_NOT_FOUND
	case service.PortalSelectionNoServer:
		response.Status = pb.SelectGameServerForAreaTriggerResponse_NO_SERVER
	default:
		response.Status = pb.SelectGameServerForAreaTriggerResponse_OK
		response.InstancePlacement = selection.InstancePlacement
		response.LayerID = selection.Server.LayerID
		response.GameServer = &pb.Server{ID: selection.Server.ID, Address: selection.Server.Address, RealmID: selection.Server.RealmID, GrpcAddress: selection.Server.GRPCAddress, LayerID: selection.Server.LayerID}
	}
	return response, nil
}

func (s *serversRegistry) FinalizeInstanceReset(ctx context.Context, request *pb.FinalizeInstanceResetRequest) (*pb.FinalizeInstanceResetResponse, error) {
	if err := s.instances.FinalizeReset(ctx, request.RealmID, request.CharacterGUID, request.GroupID, request.MapID, request.MemberGUIDs); err != nil {
		return nil, err
	}
	return &pb.FinalizeInstanceResetResponse{Api: ver}, nil
}

func (s *serversRegistry) GetInstanceResetTargets(ctx context.Context, request *pb.GetInstanceResetTargetsRequest) (*pb.GetInstanceResetTargetsResponse, error) {
	targets, err := s.instances.ResetTargets(ctx, request.RealmID, request.CharacterGUID, request.GroupID)
	if err != nil {
		return nil, err
	}
	response := &pb.GetInstanceResetTargetsResponse{Api: ver, Targets: make([]*pb.InstanceResetTarget, 0, len(targets))}
	for _, target := range targets {
		server := target.Server
		response.Targets = append(response.Targets, &pb.InstanceResetTarget{
			MapID:      target.MapID,
			GameServer: &pb.Server{ID: server.ID, Address: server.Address, RealmID: server.RealmID, GrpcAddress: server.GRPCAddress, LayerID: server.LayerID},
		})
	}
	return response, nil
}

func (s *serversRegistry) RegisterGameServer(ctx context.Context, request *pb.RegisterGameServerRequest) (*pb.RegisterGameServerResponse, error) {
	p, _ := peer.FromContext(ctx)

	log.Info().Interface("request", request).Msg("New request to add game server")

	host := removePortFromAddress(p.Addr.String())
	if request.PreferredHostName != "" {
		host = request.PreferredHostName
	}

	gameServer := &repo.GameServer{
		Address:         fmt.Sprintf("%s:%d", host, request.GamePort),
		HealthCheckAddr: fmt.Sprintf("%s:%d", host, request.HealthPort),
		GRPCAddress:     fmt.Sprintf("%s:%d", host, request.GrpcPort),
		RealmID:         request.RealmID,
		IsCrossRealm:    request.IsCrossRealm,
		AvailableMaps:   stringToAvailableMaps(request.AvailableMaps),
		LayerID:         request.LayerID,
	}

	err := s.gService.Register(ctx, gameServer)
	if err != nil {
		return nil, err
	}
	config, err := s.layer.Configuration(ctx, request.RealmID)
	if err != nil {
		return nil, err
	}
	if err := s.gService.ConfigureLayers(ctx, request.RealmID, config); err != nil {
		return nil, err
	}
	registered, err := s.gService.ListForRealm(ctx, request.RealmID)
	if err != nil {
		return nil, err
	}
	for i := range registered {
		if registered[i].ID == gameServer.ID {
			gameServer.AssignedMapsToHandle = registered[i].AssignedMapsToHandle
			break
		}
	}

	return &pb.RegisterGameServerResponse{
		Api:          ver,
		Id:           gameServer.ID,
		AssignedMaps: gameServer.AssignedMapsToHandle,
		LayerID:      gameServer.LayerID,
	}, nil
}

func (s *serversRegistry) AvailableGameServersForMapAndRealm(ctx context.Context, request *pb.AvailableGameServersForMapAndRealmRequest) (*pb.AvailableGameServersForMapAndRealmResponse, error) {
	servers, err := s.gService.AvailableForMapAndRealm(ctx, request.MapID, request.RealmID, request.IsCrossRealm)
	if err != nil {
		return nil, err
	}

	resultServers := make([]*pb.Server, 0, len(servers))
	for i := range servers {
		resultServers = append(resultServers, &pb.Server{
			ID:           servers[i].ID,
			Address:      servers[i].Address,
			RealmID:      servers[i].RealmID,
			IsCrossRealm: servers[i].IsCrossRealm,
			GrpcAddress:  servers[i].GRPCAddress,
			LayerID:      servers[i].LayerID,
		})
	}

	return &pb.AvailableGameServersForMapAndRealmResponse{
		Api:         ver,
		GameServers: resultServers,
	}, nil
}

func (s *serversRegistry) ListGameServersForRealm(ctx context.Context, request *pb.ListGameServersForRealmRequest) (*pb.ListGameServersResponse, error) {
	var (
		servers []repo.GameServer
		err     error
	)

	if request.IsCrossRealm {
		servers, err = s.gService.ListOfCrossRealms(ctx)
	} else {
		servers, err = s.gService.ListForRealm(ctx, request.RealmID)
	}
	if err != nil {
		return nil, err
	}

	respServers := make([]*pb.GameServerDetailed, len(servers))
	for i := range servers {
		respServers[i] = &pb.GameServerDetailed{
			ID:                servers[i].ID,
			Address:           servers[i].Address,
			HealthAddress:     servers[i].HealthCheckAddr,
			GrpcAddress:       servers[i].GRPCAddress,
			RealmID:           servers[i].RealmID,
			IsCrossRealm:      servers[i].IsCrossRealm,
			ActiveConnections: servers[i].ActiveConnections,
			AvailableMaps:     servers[i].AvailableMaps,
			AssignedMaps:      servers[i].AssignedMapsToHandle,
			LayerID:           servers[i].LayerID,
			Diff: &pb.GameServerDetailed_Diff{
				Mean:         servers[i].Diff.Mean,
				Median:       servers[i].Diff.Median,
				Percentile95: servers[i].Diff.Percentile95,
				Percentile99: servers[i].Diff.Percentile99,
				Max:          servers[i].Diff.Max,
			},
		}
	}

	return &pb.ListGameServersResponse{
		Api:         ver,
		GameServers: respServers,
	}, nil
}
func (s *serversRegistry) ListAllGameServers(ctx context.Context, request *pb.ListAllGameServersRequest) (*pb.ListGameServersResponse, error) {
	servers, err := s.gService.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	respServers := make([]*pb.GameServerDetailed, len(servers))
	for i := range servers {
		respServers[i] = &pb.GameServerDetailed{
			ID:                servers[i].ID,
			Address:           servers[i].Address,
			HealthAddress:     servers[i].HealthCheckAddr,
			GrpcAddress:       servers[i].GRPCAddress,
			RealmID:           servers[i].RealmID,
			IsCrossRealm:      servers[i].IsCrossRealm,
			ActiveConnections: servers[i].ActiveConnections,
			AvailableMaps:     servers[i].AvailableMaps,
			AssignedMaps:      servers[i].AssignedMapsToHandle,
			LayerID:           servers[i].LayerID,
			Diff: &pb.GameServerDetailed_Diff{
				Mean:         servers[i].Diff.Mean,
				Median:       servers[i].Diff.Median,
				Percentile95: servers[i].Diff.Percentile95,
				Percentile99: servers[i].Diff.Percentile99,
				Max:          servers[i].Diff.Max,
			},
		}
	}

	return &pb.ListGameServersResponse{
		Api:         ver,
		GameServers: respServers,
	}, nil
}

func (s *serversRegistry) RandomGameServerForRealm(ctx context.Context, request *pb.RandomGameServerForRealmRequest) (*pb.RandomGameServerForRealmResponse, error) {
	server, err := s.gService.RandomServerForRealm(ctx, request.RealmID)
	if err != nil {
		return nil, err
	}

	if server == nil {
		return &pb.RandomGameServerForRealmResponse{
			Api:        ver,
			GameServer: nil,
		}, nil
	}

	return &pb.RandomGameServerForRealmResponse{
		Api: ver,
		GameServer: &pb.Server{
			Address: server.Address,
			RealmID: server.RealmID,
		},
	}, nil
}

func (s *serversRegistry) GameServerMapsLoaded(ctx context.Context, request *pb.GameServerMapsLoadedRequest) (*pb.GameServerMapsLoadedResponse, error) {
	_, err := s.gService.MapsLoadedForServer(ctx, request.GameServerID, request.MapsLoaded)
	if err != nil {
		return nil, err
	}

	return &pb.GameServerMapsLoadedResponse{
		Api: ver,
	}, nil
}

func (s *serversRegistry) RegisterGateway(ctx context.Context, request *pb.RegisterGatewayRequest) (*pb.RegisterGatewayResponse, error) {
	p, _ := peer.FromContext(ctx)

	log.Info().Interface("request", request).Msg("New request to add gateway")

	ip := removePortFromAddress(p.Addr.String())
	lbServer := &repo.GatewayServer{
		Address:         fmt.Sprintf("%s:%d", request.PreferredHostName, request.GamePort),
		HealthCheckAddr: fmt.Sprintf("%s:%d", ip, request.HealthPort),
		RealmID:         request.RealmID,
	}

	server, err := s.lbService.Register(ctx, lbServer)
	if err != nil {
		return nil, err
	}

	return &pb.RegisterGatewayResponse{
		Api: ver,
		Id:  server.ID,
	}, nil
}

func (s *serversRegistry) GatewaysForRealms(ctx context.Context, request *pb.GatewaysForRealmsRequest) (*pb.GatewaysForRealmsResponse, error) {
	servers := make([]*pb.Server, 0, len(request.RealmIDs))

	for _, realmID := range request.RealmIDs {
		server, err := s.lbService.GatewayForRealm(ctx, realmID)
		if err != nil {
			return nil, err
		}
		if server == nil {
			continue
		}

		servers = append(servers, &pb.Server{
			Address: server.Address,
			RealmID: server.RealmID,
		})
	}

	return &pb.GatewaysForRealmsResponse{
		Api:      ver,
		Gateways: servers,
	}, nil
}

func (s *serversRegistry) ListGatewaysForRealm(ctx context.Context, request *pb.ListGatewaysForRealmRequest) (*pb.ListGatewaysForRealmResponse, error) {
	servers, err := s.lbService.GatewaysForRealm(ctx, request.RealmID)
	if err != nil {
		return nil, err
	}

	result := make([]*pb.GatewayServerDetailed, len(servers))
	for i := range servers {
		result[i] = &pb.GatewayServerDetailed{
			Id:                servers[i].ID,
			Address:           servers[i].Address,
			HealthAddress:     servers[i].HealthCheckAddr,
			RealmID:           servers[i].RealmID,
			ActiveConnections: uint32(servers[i].ActiveConnections),
		}
	}

	return &pb.ListGatewaysForRealmResponse{
		Api:      ver,
		Gateways: result,
	}, nil
}

func (s *serversRegistry) SelectGameServerForPlayer(ctx context.Context, request *pb.SelectGameServerForPlayerRequest) (*pb.SelectGameServerForPlayerResponse, error) {
	if s.instances.IsInstanceMap(request.MapID) {
		if request.ForcedLayerID != 0 {
			return &pb.SelectGameServerForPlayerResponse{Api: ver, Status: pb.SelectGameServerForPlayerResponse_LAYER_NOT_FOUND}, nil
		}
		selection, err := s.instances.Select(ctx, request.RealmID, request.CharacterGUID, request.GroupID, request.MapID)
		if err != nil {
			return nil, err
		}
		response := &pb.SelectGameServerForPlayerResponse{Api: ver}
		if selection.Status != service.PortalSelectionOK {
			response.Status = pb.SelectGameServerForPlayerResponse_NO_SERVER
			return response, nil
		}
		response.Status = pb.SelectGameServerForPlayerResponse_OK
		response.InstancePlacement = true
		response.GameServer = &pb.Server{ID: selection.Server.ID, Address: selection.Server.Address, RealmID: selection.Server.RealmID, GrpcAddress: selection.Server.GRPCAddress, LayerID: selection.Server.LayerID}
		return response, nil
	}
	selection, err := s.layer.Select(ctx, request.RealmID, request.MapID, request.GroupID, request.ForcedLayerID)
	if err != nil {
		return nil, err
	}
	response := &pb.SelectGameServerForPlayerResponse{Api: ver}
	switch selection.Status {
	case service.LayerSelectionNoServer:
		response.Status = pb.SelectGameServerForPlayerResponse_NO_SERVER
	case service.LayerSelectionNotFound:
		response.Status = pb.SelectGameServerForPlayerResponse_LAYER_NOT_FOUND
	default:
		response.Status = pb.SelectGameServerForPlayerResponse_OK
		response.LayerID = selection.Server.LayerID
		response.GameServer = &pb.Server{ID: selection.Server.ID, Address: selection.Server.Address, RealmID: selection.Server.RealmID, GrpcAddress: selection.Server.GRPCAddress, LayerID: selection.Server.LayerID}
	}
	return response, nil
}

func (s *serversRegistry) BindGroupToGameServer(ctx context.Context, request *pb.BindGroupToGameServerRequest) (*pb.BindGroupToGameServerResponse, error) {
	var err error
	if s.instances.IsInstanceMap(request.MapID) {
		err = s.instances.BindGroup(ctx, request.RealmID, request.GroupID, request.MapID, request.GameServerID)
	} else {
		err = s.layer.BindGroup(ctx, request.RealmID, request.GroupID, request.MapID, request.GameServerID)
	}
	if err != nil {
		return nil, err
	}
	return &pb.BindGroupToGameServerResponse{Api: ver}, nil
}

func (s *serversRegistry) GetMapLayerConfiguration(ctx context.Context, request *pb.GetMapLayerConfigurationRequest) (*pb.GetMapLayerConfigurationResponse, error) {
	config, err := s.layer.Configuration(ctx, request.RealmID)
	if err != nil {
		return nil, err
	}
	mapIDs := make([]uint32, 0, len(config))
	for mapID := range config {
		mapIDs = append(mapIDs, mapID)
	}
	sort.Slice(mapIDs, func(i, j int) bool { return mapIDs[i] < mapIDs[j] })
	response := &pb.GetMapLayerConfigurationResponse{Api: ver}
	for _, mapID := range mapIDs {
		response.Maps = append(response.Maps, &pb.MapLayerConfiguration{MapID: mapID, LayerCount: config[mapID]})
	}
	return response, nil
}

func (s *serversRegistry) UpdateMapLayerConfiguration(ctx context.Context, request *pb.UpdateMapLayerConfigurationRequest) (*pb.UpdateMapLayerConfigurationResponse, error) {
	config := make(map[uint32]uint32, len(request.Maps))
	for _, item := range request.Maps {
		config[item.MapID] = item.LayerCount
	}
	if err := s.layer.UpdateConfiguration(ctx, request.RealmID, config); err != nil {
		return nil, err
	}
	return &pb.UpdateMapLayerConfigurationResponse{Api: ver}, nil
}

func (s *serversRegistry) GetLayerStats(ctx context.Context, request *pb.GetLayerStatsRequest) (*pb.GetLayerStatsResponse, error) {
	if s.instances.IsInstanceMap(request.MapID) {
		return &pb.GetLayerStatsResponse{Api: ver}, nil
	}
	configured, stats, err := s.layer.Stats(ctx, request.RealmID, request.MapID)
	if err != nil {
		return nil, err
	}
	response := &pb.GetLayerStatsResponse{Api: ver, ConfiguredLayers: configured}
	for _, stat := range stats {
		response.Layers = append(response.Layers, &pb.GetLayerStatsResponse_Layer{LayerID: stat.LayerID, Players: stat.Players, GameServerID: stat.Server.ID, Address: stat.Server.Address})
	}
	return response, nil
}

func (s *serversRegistry) GetInstancePoolStats(ctx context.Context, request *pb.GetInstancePoolStatsRequest) (*pb.GetInstancePoolStatsResponse, error) {
	servers, err := s.gService.ListForRealm(ctx, request.RealmID)
	if err != nil {
		return nil, err
	}
	groupPlacements, err := s.instances.GroupPlacementCounts(ctx, request.RealmID)
	if err != nil {
		return nil, err
	}
	response := &pb.GetInstancePoolStatsResponse{Api: ver}
	for _, gameServer := range servers {
		mapIDs := make([]uint32, 0)
		for _, mapID := range gameServer.AssignedMapsToHandle {
			if s.instances.IsInstanceMap(mapID) {
				mapIDs = append(mapIDs, mapID)
			}
		}
		if len(mapIDs) == 0 {
			continue
		}
		sort.Slice(mapIDs, func(i, j int) bool { return mapIDs[i] < mapIDs[j] })
		response.Cores = append(response.Cores, &pb.GetInstancePoolStatsResponse_Core{
			GameServerID: gameServer.ID, Address: gameServer.Address,
			Players: gameServer.ActiveConnections, MapIDs: mapIDs, GroupPlacements: groupPlacements[gameServer.ID],
		})
	}
	sort.Slice(response.Cores, func(i, j int) bool { return response.Cores[i].GameServerID < response.Cores[j].GameServerID })
	return response, nil
}

func removePortFromAddress(address string) string {
	for i := len(address) - 1; i >= 0; i-- {
		if address[i] == ':' {
			return address[:i]
		}
	}

	return address
}

func stringToAvailableMaps(s string) []uint32 {
	v := strings.Split(s, ",")
	if len(v) == 0 {
		return []uint32{}
	}

	result := make([]uint32, 0, len(v))
	for i := range v {
		r, err := strconv.Atoi(v[i])
		if err != nil {
			continue
		}

		result = append(result, uint32(r))
	}

	return result
}
