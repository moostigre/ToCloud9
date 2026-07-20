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
	layers    Layer
	instances InstancePool
	store     repo.PortalStore
}

func NewPortal(layers Layer, instances InstancePool, store repo.PortalStore) Portal {
	return &portalService{layers: layers, instances: instances, store: store}
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

	selection, err := p.layers.Select(ctx, realmID, destinationMapID, groupID, 0)
	if err != nil {
		return PortalSelection{}, err
	}
	if selection.Status != LayerSelectionOK || selection.Server == nil {
		return PortalSelection{Status: PortalSelectionNoServer, DestinationMapID: destinationMapID}, nil
	}
	return PortalSelection{Status: PortalSelectionOK, DestinationMapID: destinationMapID, Server: *selection.Server}, nil
}
