package session

import (
	"context"
	"fmt"

	root "github.com/walkline/ToCloud9/apps/gateway"
	"github.com/walkline/ToCloud9/apps/gateway/packet"
	pbServ "github.com/walkline/ToCloud9/gen/servers-registry/pb"
)

type instanceResetHandoff struct {
	request      *packet.Packet
	mapID        uint32
	returnServer *pbServ.Server
	requestSent  bool
}

// HandleResetInstances relays AzerothCore's native reset request through the
// character's authenticated session on the core that owns the instance. This
// keeps instance lifecycle inside AzerothCore without requiring a core patch.
func (s *GameSession) HandleResetInstances(ctx context.Context, p *packet.Packet) error {
	if s.character == nil || s.worldSocket == nil || s.serversRegistryClient == nil {
		return nil
	}
	groupID := s.groupIDForPlayer(ctx, s.character.GUID)
	result, err := s.serversRegistryClient.GetInstanceResetTargets(ctx, &pbServ.GetInstanceResetTargetsRequest{
		Api: root.SupportedServerRegistryVer, RealmID: root.RealmID,
		CharacterGUID: s.character.GUID, GroupID: groupID,
	})
	if err != nil {
		return fmt.Errorf("get instance reset target: %w", err)
	}
	if len(result.Targets) == 0 {
		s.worldSocket.SendPacket(p)
		return nil
	}

	// The native opcode resets every normal dungeon bound on this worldserver.
	// Placements normally converge on one instance-pool core for a character or
	// group. A later extension can sequence distinct owners if needed.
	target := result.Targets[0]
	if target.GameServer == nil {
		return fmt.Errorf("instance reset target for map %d has no gameserver", target.MapID)
	}
	if target.GameServer.ID == s.currentGameServerID || target.GameServer.Address == s.worldSocket.Address() {
		s.worldSocket.SendPacket(p)
		return nil
	}

	returnSelection, err := s.selectGameServerForMap(ctx, s.character.GUID, s.character.Map)
	if err != nil {
		return fmt.Errorf("select return gameserver for instance reset: %w", err)
	}
	if returnSelection.Status != pbServ.SelectGameServerForPlayerResponse_OK || returnSelection.GameServer == nil {
		return fmt.Errorf("no return gameserver is available for map %d", s.character.Map)
	}

	s.pendingInstanceReset = &instanceResetHandoff{request: p, mapID: target.MapID, returnServer: returnSelection.GameServer}
	s.worldEntryPending = true
	if err := s.redirectPlayerToGameServer(ctx, s.character.GUID, target.GameServer.Address); err != nil {
		s.pendingInstanceReset = nil
		s.worldEntryPending = false
		return fmt.Errorf("redirect to instance owner for reset: %w", err)
	}
	s.currentGameServerID = target.GameServer.ID
	s.currentLayerID = target.GameServer.LayerID
	return nil
}

func (s *GameSession) returnFromInstanceReset(ctx context.Context, server *pbServ.Server) error {
	if server == nil || server.ID == s.currentGameServerID {
		return nil
	}
	if err := s.redirectPlayerToGameServer(ctx, s.character.GUID, server.Address); err != nil {
		return fmt.Errorf("return from instance reset owner: %w", err)
	}
	s.currentGameServerID = server.ID
	s.currentLayerID = server.LayerID
	return nil
}
