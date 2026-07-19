package service

import (
	"context"

	"github.com/walkline/ToCloud9/apps/servers-registry/repo"
)

type PortalSelectionStatus uint8

const (
	PortalSelectionOK PortalSelectionStatus = iota
	PortalSelectionTriggerNotFound
	PortalSelectionNoServer
)

type PortalSelection struct {
	Status            PortalSelectionStatus
	DestinationMapID  uint32
	Server            repo.GameServer
	InstancePlacement bool
}

type Portal interface {
	Select(context.Context, uint32, uint64, uint32, uint32) (PortalSelection, error)
}

type portalService struct {
	servers   GameServer
	layers    Layer
	instances InstancePool
	store     repo.PortalStore
}

func NewPortal(servers GameServer, layers Layer, instances InstancePool, store repo.PortalStore) Portal {
	return &portalService{servers: servers, layers: layers, instances: instances, store: store}
}

func (p *portalService) Select(ctx context.Context, realmID uint32, characterGUID uint64, groupID, triggerID uint32) (PortalSelection, error) {
	destinationMapID, found, err := p.store.DestinationMap(ctx, triggerID)
	if err != nil {
		return PortalSelection{}, err
	}
	if !found {
		return PortalSelection{Status: PortalSelectionTriggerNotFound}, nil
	}
	if p.instances.IsInstanceMap(destinationMapID) {
		return p.instances.Select(ctx, realmID, characterGUID, groupID, destinationMapID)
	}

	servers, err := p.servers.AvailableForMapAndRealm(ctx, destinationMapID, realmID, false)
	if err != nil {
		return PortalSelection{}, err
	}
	configuration, err := p.layers.Configuration(ctx, realmID)
	if err != nil {
		return PortalSelection{}, err
	}
	if layerCount := configuration[destinationMapID]; layerCount >= 2 {
		eligible := servers[:0]
		for _, server := range servers {
			if server.LayerID > 0 && server.LayerID <= layerCount {
				eligible = append(eligible, server)
			}
		}
		servers = eligible
	}
	if len(servers) == 0 {
		return PortalSelection{Status: PortalSelectionNoServer, DestinationMapID: destinationMapID}, nil
	}

	ownerType, ownerID := "character", characterGUID
	if groupID != 0 {
		ownerType, ownerID = "group", uint64(groupID)
	}
	boundID, err := p.store.Placement(ctx, realmID, ownerType, ownerID, destinationMapID)
	if err != nil {
		return PortalSelection{}, err
	}
	if server := serverByID(servers, boundID); server != nil {
		return PortalSelection{Status: PortalSelectionOK, DestinationMapID: destinationMapID, Server: *server}, nil
	}

	selected := leastLoaded(servers)
	var winner string
	if boundID == "" {
		winner, err = p.store.BindPlacement(ctx, realmID, ownerType, ownerID, destinationMapID, selected.ID)
	} else {
		winner, err = p.store.ReplacePlacement(ctx, realmID, ownerType, ownerID, destinationMapID, boundID, selected.ID)
	}
	if err != nil {
		return PortalSelection{}, err
	}
	if server := serverByID(servers, winner); server != nil {
		return PortalSelection{Status: PortalSelectionOK, DestinationMapID: destinationMapID, Server: *server}, nil
	}
	return PortalSelection{Status: PortalSelectionNoServer, DestinationMapID: destinationMapID}, nil
}
