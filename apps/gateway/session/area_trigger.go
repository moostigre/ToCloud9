package session

import (
	"context"
	"fmt"

	root "github.com/walkline/ToCloud9/apps/gateway"
	"github.com/walkline/ToCloud9/apps/gateway/packet"
	pbServ "github.com/walkline/ToCloud9/gen/servers-registry/pb"
	pbWorld "github.com/walkline/ToCloud9/gen/worldserver/pb"
)

// HandleAreaTrigger routes map-changing triggers before AzerothCore processes
// them. This ensures only the worldserver selected for the destination map can
// create or reuse a dungeon instance.
func (s *GameSession) HandleAreaTrigger(ctx context.Context, p *packet.Packet) error {
	if s.worldSocket == nil || s.gameServerGRPCClient == nil || s.character == nil {
		return nil
	}

	triggerID := p.Reader().Uint32()
	resolved, err := s.gameServerGRPCClient.GetAreaTriggerTeleportDestination(ctx, &pbWorld.GetAreaTriggerTeleportDestinationRequest{
		Api: root.SupportedGameServerVer, TriggerID: triggerID,
	})
	if err != nil {
		return fmt.Errorf("resolve area trigger %d: %w", triggerID, err)
	}
	if !resolved.Found {
		s.worldSocket.SendPacket(p)
		return nil
	}

	selection, err := s.selectGameServerForMap(ctx, s.character.GUID, resolved.DestinationMapID)
	if err != nil {
		return fmt.Errorf("select gameserver for area trigger %d destination map %d: %w", triggerID, resolved.DestinationMapID, err)
	}
	if selection.Status != pbServ.SelectGameServerForPlayerResponse_OK || selection.GameServer == nil {
		return fmt.Errorf("%w, mapID %v", worldConnectErrInstanceNotFound, resolved.DestinationMapID)
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
	s.currentLayerID = target.LayerID
	return nil
}
