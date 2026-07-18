package session

import (
	"context"
	"fmt"

	root "github.com/walkline/ToCloud9/apps/gateway"
	"github.com/walkline/ToCloud9/apps/gateway/packet"
	pbServ "github.com/walkline/ToCloud9/gen/servers-registry/pb"
)

func (s *GameSession) selectLayerGameServer(ctx context.Context, groupID, forcedLayerID uint32) (*pbServ.SelectGameServerForPlayerResponse, error) {
	if s.character == nil {
		return nil, nil
	}
	return s.serversRegistryClient.SelectGameServerForPlayer(ctx, &pbServ.SelectGameServerForPlayerRequest{
		Api: root.SupportedServerRegistryVer, RealmID: root.RealmID, MapID: s.character.Map,
		GroupID: groupID, ForcedLayerID: forcedLayerID,
	})
}

func (s *GameSession) applyGroupLayer(ctx context.Context, groupID uint32) error {
	selection, err := s.selectLayerGameServer(ctx, groupID, 0)
	if err != nil || selection == nil {
		return err
	}
	if selection.Status != pbServ.SelectGameServerForPlayerResponse_OK || selection.GameServer == nil {
		return nil
	}
	if selection.GameServer.ID == s.currentGameServerID {
		s.currentLayerID = selection.LayerID
		return nil
	}
	s.SendSysMessage(fmt.Sprintf("Switching to layer %d.", selection.LayerID))
	if err := s.redirectToSelectedLayer(ctx, selection.GameServer); err != nil {
		return err
	}
	return nil
}

func (s *GameSession) redirectToSelectedLayer(ctx context.Context, server *pbServ.Server) error {
	if server == nil || s.character == nil {
		return nil
	}
	s.gameServerGRPCConnMgr.AddAddressMapping(server.Address, server.GrpcAddress)
	client, err := s.gameServerGRPCConnMgr.GRPCConnByGameServerAddress(server.Address)
	if err != nil {
		return fmt.Errorf("connect to layer gameserver gRPC: %w", err)
	}
	// The worldserver handoff is transparent to the game client. Wrap it in the
	// regular world-transfer packets so the client unloads the previous layer's
	// objects before it starts processing updates from the selected layer.
	transferPending, newWorld := layerWorldTransferPackets(
		s.character.Map,
		s.character.PositionX,
		s.character.PositionY,
		s.character.PositionZ,
		s.character.PositionO,
	)
	s.gameSocket.Send(transferPending)

	if err := s.redirectPlayerToGameServerBeforeAttach(ctx, s.character.GUID, server.Address, func() {
		s.gameSocket.Send(newWorld)
	}); err != nil {
		return err
	}

	s.gameServerGRPCClient = client
	s.currentGameServerID = server.ID
	s.currentLayerID = server.LayerID
	return nil
}

func layerWorldTransferPackets(mapID uint32, x, y, z, orientation float32) (*packet.Writer, *packet.Writer) {
	transferPending := packet.NewWriterWithSize(packet.SMsgTransferPending, 4)
	transferPending.Uint32(mapID)

	newWorld := packet.NewWriterWithSize(packet.SMsgNewWorld, 20)
	newWorld.Uint32(mapID)
	newWorld.Float32(x)
	newWorld.Float32(y)
	newWorld.Float32(z)
	newWorld.Float32(orientation)

	return transferPending, newWorld
}
