package session

import (
	"context"
	"fmt"

	root "github.com/walkline/ToCloud9/apps/gateway"
	"github.com/walkline/ToCloud9/apps/gateway/packet"
	pbServ "github.com/walkline/ToCloud9/gen/servers-registry/pb"
)

// HandleAreaTrigger routes map-changing triggers before AzerothCore processes
// them. This ensures only the worldserver selected for the destination map can
// create or reuse a dungeon instance.
func (s *GameSession) HandleAreaTrigger(ctx context.Context, p *packet.Packet) error {
	if s.worldSocket == nil || s.serversRegistryClient == nil || s.character == nil {
		return nil
	}

	triggerID := p.Reader().Uint32()
	selection, err := s.serversRegistryClient.SelectGameServerForAreaTrigger(ctx, &pbServ.SelectGameServerForAreaTriggerRequest{
		Api:           root.SupportedServerRegistryVer,
		RealmID:       root.RealmID,
		CharacterGUID: s.character.GUID,
		GroupID:       s.groupIDForPlayer(ctx, s.character.GUID),
		AreaTriggerID: triggerID,
	})
	if err != nil {
		return fmt.Errorf("select gameserver for area trigger %d: %w", triggerID, err)
	}
	if selection.Status == pbServ.SelectGameServerForAreaTriggerResponse_TRIGGER_NOT_FOUND {
		s.worldSocket.SendPacket(p)
		return nil
	}
	if selection.Status != pbServ.SelectGameServerForAreaTriggerResponse_OK || selection.GameServer == nil {
		return fmt.Errorf("%w, mapID %v", worldConnectErrInstanceNotFound, selection.DestinationMapID)
	}
	// Only instance entries need pre-routing. An instance exit must first be
	// processed by the core that owns the live instance; otherwise logging the
	// character into an outdoor core while its saved map is still the dungeon
	// briefly loads a second copy of that instance. The normal world-port path
	// performs outdoor layer placement after AzerothCore completes the exit.
	if !selection.InstancePlacement {
		s.worldSocket.SendPacket(p)
		return nil
	}

	target := selection.GameServer
	if target.ID == s.currentGameServerID || target.Address == s.worldSocket.Address() {
		s.worldSocket.SendPacket(p)
		return nil
	}

	s.gameServerGRPCConnMgr.AddAddressMapping(target.Address, target.GrpcAddress)
	targetClient, err := s.gameServerGRPCConnMgr.GRPCConnByGameServerAddress(target.Address)
	if err != nil {
		return fmt.Errorf("connect to area-trigger destination gRPC: %w", err)
	}

	// This packet belongs to this client session and is replayed only after the
	// destination core has added the player to its world. No cross-gateway state
	// or coordination is required.
	s.pendingAreaTrigger = p
	s.worldEntryPending = true
	if err := s.redirectPlayerToGameServer(ctx, s.character.GUID, target.Address); err != nil {
		s.pendingAreaTrigger = nil
		s.worldEntryPending = false
		return fmt.Errorf("pre-route area trigger %d: %w", triggerID, err)
	}

	s.gameServerGRPCClient = targetClient
	s.currentGameServerID = target.ID
	if selection.InstancePlacement {
		s.currentLayerID = 0
	} else {
		s.currentLayerID = target.LayerID
	}
	return nil
}
