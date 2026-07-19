package service

import (
	"context"
	"fmt"

	"github.com/walkline/ToCloud9/apps/servers-registry/repo"
)

// InstancePool distributes instance maps across a pool of gameservers while
// keeping each logical instance route pinned to one owning process.
type InstancePool interface {
	IsInstanceMap(uint32) bool
	Select(context.Context, uint32, uint64, uint32, uint32) (PortalSelection, error)
	BindGroup(context.Context, uint32, uint32, uint32, string) error
	ReassignAfterReset(context.Context, uint32, uint64, uint32, uint32) error
	ResetTargets(context.Context, uint32, uint64, uint32) ([]InstanceResetTarget, error)
}

type InstanceResetTarget struct {
	MapID  uint32
	Server repo.GameServer
}

func (p *instancePoolService) ReassignAfterReset(ctx context.Context, realmID uint32, characterGUID uint64, groupID, mapID uint32) error {
	if characterGUID == 0 || !p.IsInstanceMap(mapID) {
		return fmt.Errorf("valid character and instance map are required")
	}
	servers, err := p.servers.AvailableForMapAndRealm(ctx, mapID, realmID, false)
	if err != nil {
		return err
	}
	if len(servers) == 0 {
		return fmt.Errorf("no gameserver is available for instance map %d", mapID)
	}

	ownerType, ownerID := "character", characterGUID
	if groupID != 0 {
		ownerType, ownerID = "group", uint64(groupID)
	}
	previous, err := p.store.Placement(ctx, realmID, ownerType, ownerID, mapID)
	if err != nil {
		return err
	}
	if previous == "" && groupID != 0 {
		previous, err = p.store.Placement(ctx, realmID, "character", characterGUID, mapID)
		if err != nil {
			return err
		}
	}

	candidates := make([]repo.GameServer, 0, len(servers))
	for _, server := range servers {
		if len(servers) == 1 || server.ID != previous {
			candidates = append(candidates, server)
		}
	}
	selected := leastLoaded(candidates)
	if err := p.store.SetPlacement(ctx, realmID, ownerType, ownerID, mapID, selected.ID); err != nil {
		return err
	}
	return p.store.SetPlacement(ctx, realmID, "character", characterGUID, mapID, selected.ID)
}

type instancePoolService struct {
	servers GameServer
	store   repo.PortalStore
	maps    map[uint32]struct{}
}

func NewInstancePool(servers GameServer, store repo.PortalStore, maps []uint32) InstancePool {
	instanceMaps := make(map[uint32]struct{}, len(maps))
	for _, mapID := range maps {
		instanceMaps[mapID] = struct{}{}
	}
	return &instancePoolService{servers: servers, store: store, maps: instanceMaps}
}

func (p *instancePoolService) IsInstanceMap(mapID uint32) bool {
	_, ok := p.maps[mapID]
	return ok
}

func (p *instancePoolService) ResetTargets(ctx context.Context, realmID uint32, characterGUID uint64, groupID uint32) ([]InstanceResetTarget, error) {
	ownerType, ownerID := "character", characterGUID
	if groupID != 0 {
		ownerType, ownerID = "group", uint64(groupID)
	}
	targets := make([]InstanceResetTarget, 0)
	for mapID := range p.maps {
		serverID, err := p.store.Placement(ctx, realmID, ownerType, ownerID, mapID)
		if err != nil {
			return nil, err
		}
		if serverID == "" && groupID != 0 {
			serverID, err = p.store.Placement(ctx, realmID, "character", characterGUID, mapID)
			if err != nil {
				return nil, err
			}
		}
		if serverID == "" {
			continue
		}
		servers, err := p.servers.AvailableForMapAndRealm(ctx, mapID, realmID, false)
		if err != nil {
			return nil, err
		}
		if server := serverByID(servers, serverID); server != nil {
			targets = append(targets, InstanceResetTarget{MapID: mapID, Server: *server})
		}
	}
	return targets, nil
}

func (p *instancePoolService) Select(ctx context.Context, realmID uint32, characterGUID uint64, groupID, mapID uint32) (PortalSelection, error) {
	servers, err := p.servers.AvailableForMapAndRealm(ctx, mapID, realmID, false)
	if err != nil {
		return PortalSelection{}, err
	}
	if len(servers) == 0 {
		return PortalSelection{Status: PortalSelectionNoServer, DestinationMapID: mapID}, nil
	}

	// A group owner is canonical while grouped. Group creation binds this key
	// to the leader's current instance core. Every successful grouped lookup
	// also updates the character key, preserving affinity after leaving.
	ownerType, ownerID := "character", characterGUID
	if groupID != 0 {
		ownerType, ownerID = "group", uint64(groupID)
	}
	boundID, err := p.store.Placement(ctx, realmID, ownerType, ownerID, mapID)
	if err != nil {
		return PortalSelection{}, err
	}
	// When an existing solo instance becomes a group instance, let the first
	// entrant carry its established owner into the new group binding. Group
	// creation inside the instance normally pre-binds this explicitly; this
	// fallback covers groups created after the leader has stepped outside.
	if groupID != 0 && boundID == "" && characterGUID != 0 {
		characterOwner, placementErr := p.store.Placement(ctx, realmID, "character", characterGUID, mapID)
		if placementErr != nil {
			return PortalSelection{}, placementErr
		}
		if serverByID(servers, characterOwner) != nil {
			boundID, err = p.store.BindPlacement(ctx, realmID, ownerType, ownerID, mapID, characterOwner)
			if err != nil {
				return PortalSelection{}, err
			}
		}
	}
	selected := serverByID(servers, boundID)
	if selected == nil {
		candidate := leastLoaded(servers)
		var winner string
		if boundID == "" {
			winner, err = p.store.BindPlacement(ctx, realmID, ownerType, ownerID, mapID, candidate.ID)
		} else {
			winner, err = p.store.ReplacePlacement(ctx, realmID, ownerType, ownerID, mapID, boundID, candidate.ID)
		}
		if err != nil {
			return PortalSelection{}, err
		}
		selected = serverByID(servers, winner)
	}
	if selected == nil {
		return PortalSelection{Status: PortalSelectionNoServer, DestinationMapID: mapID}, nil
	}
	if characterGUID != 0 {
		if err := p.store.SetPlacement(ctx, realmID, "character", characterGUID, mapID, selected.ID); err != nil {
			return PortalSelection{}, err
		}
	}
	return PortalSelection{Status: PortalSelectionOK, DestinationMapID: mapID, Server: *selected, InstancePlacement: true}, nil
}

func (p *instancePoolService) BindGroup(ctx context.Context, realmID, groupID, mapID uint32, serverID string) error {
	if groupID == 0 || serverID == "" || !p.IsInstanceMap(mapID) {
		return fmt.Errorf("valid instance map, group ID and gameserver ID are required")
	}
	servers, err := p.servers.AvailableForMapAndRealm(ctx, mapID, realmID, false)
	if err != nil {
		return err
	}
	if serverByID(servers, serverID) == nil {
		return fmt.Errorf("gameserver %s is not available for instance map %d", serverID, mapID)
	}
	return p.store.SetPlacement(ctx, realmID, "group", uint64(groupID), mapID, serverID)
}
