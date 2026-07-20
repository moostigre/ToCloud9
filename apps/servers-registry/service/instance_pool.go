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
	FinalizeReset(context.Context, uint32, uint64, uint32, uint32, []uint64) error
	ResetTargets(context.Context, uint32, uint64, uint32) ([]InstanceResetTarget, error)
	GroupPlacementCounts(context.Context, uint32) (map[string]uint32, error)
}

func (p *instancePoolService) GroupPlacementCounts(ctx context.Context, realmID uint32) (map[string]uint32, error) {
	return p.store.GroupPlacementCounts(ctx, realmID)
}

type InstanceResetTarget struct {
	MapID  uint32
	Server repo.GameServer
}

func (p *instancePoolService) FinalizeReset(ctx context.Context, realmID uint32, characterGUID uint64, groupID, mapID uint32, memberGUIDs []uint64) error {
	if characterGUID == 0 || !p.IsInstanceMap(mapID) {
		return fmt.Errorf("valid character and instance map are required")
	}
	if groupID != 0 {
		if err := p.store.DeletePlacement(ctx, realmID, "group", uint64(groupID), mapID); err != nil {
			return err
		}
	}
	if len(memberGUIDs) == 0 {
		memberGUIDs = []uint64{characterGUID}
	}
	return p.store.DeletePlacements(ctx, realmID, "character", memberGUIDs, mapID)
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
	mapIDs := make([]uint32, 0, len(p.maps))
	for mapID := range p.maps {
		mapIDs = append(mapIDs, mapID)
	}
	ownerType, ownerID := "character", characterGUID
	if groupID != 0 {
		ownerType, ownerID = "group", uint64(groupID)
	}
	placements, err := p.store.Placements(ctx, realmID, ownerType, ownerID, mapIDs)
	if err != nil {
		return nil, err
	}
	if groupID != 0 {
		characterPlacements, placementErr := p.store.Placements(ctx, realmID, "character", characterGUID, mapIDs)
		if placementErr != nil {
			return nil, placementErr
		}
		for mapID, serverID := range characterPlacements {
			if placements[mapID] == "" {
				placements[mapID] = serverID
			}
		}
	}
	targets := make([]InstanceResetTarget, 0, len(placements))
	for _, mapID := range mapIDs {
		serverID := placements[mapID]
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
