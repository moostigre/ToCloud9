package session

import (
	"context"
	"fmt"

	root "github.com/walkline/ToCloud9/apps/gateway"
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
	if err := s.redirectPlayerToGameServer(ctx, s.character.GUID, server.Address); err != nil {
		return err
	}
	s.gameServerGRPCClient = client
	s.currentGameServerID = server.ID
	s.currentLayerID = server.LayerID
	return nil
}
