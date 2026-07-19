package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/walkline/ToCloud9/apps/gateway/packet"
	socketMock "github.com/walkline/ToCloud9/apps/gateway/sockets/socketmock"
	pbServ "github.com/walkline/ToCloud9/gen/servers-registry/pb"
	servMock "github.com/walkline/ToCloud9/gen/servers-registry/pb/mocks"
	pbWorld "github.com/walkline/ToCloud9/gen/worldserver/pb"
	worldMock "github.com/walkline/ToCloud9/gen/worldserver/pb/mocks"
)

func TestAreaTriggerWithoutTeleportIsForwarded(t *testing.T) {
	worldSocket := &socketMock.Socket{}
	worldClient := &worldMock.WorldServerServiceClient{}
	s := &GameSession{
		character:            &LoggedInCharacter{GUID: 1},
		worldSocket:          worldSocket,
		gameServerGRPCClient: worldClient,
	}
	p := packet.NewWriterWithSize(packet.CMsgAreaTrigger, 4).Uint32(123).ToPacket()
	worldClient.On("GetAreaTriggerTeleportDestination", mock.Anything, mock.MatchedBy(func(req *pbWorld.GetAreaTriggerTeleportDestinationRequest) bool {
		return req.TriggerID == 123
	})).Return(&pbWorld.GetAreaTriggerTeleportDestinationResponse{Found: false}, nil).Once()
	worldSocket.On("SendPacket", p).Once()

	require.NoError(t, s.HandleAreaTrigger(context.Background(), p))
	worldClient.AssertExpectations(t)
	worldSocket.AssertExpectations(t)
}

func TestAreaTriggerAlreadyOnSelectedServerIsForwarded(t *testing.T) {
	worldSocket := &socketMock.Socket{}
	worldClient := &worldMock.WorldServerServiceClient{}
	registry := &servMock.ServersRegistryServiceClient{}
	s := &GameSession{
		character:             &LoggedInCharacter{GUID: 1},
		worldSocket:           worldSocket,
		gameServerGRPCClient:  worldClient,
		serversRegistryClient: registry,
		currentGameServerID:   "current",
	}
	p := packet.NewWriterWithSize(packet.CMsgAreaTrigger, 4).Uint32(2230).ToPacket()
	worldClient.On("GetAreaTriggerTeleportDestination", mock.Anything, mock.Anything).
		Return(&pbWorld.GetAreaTriggerTeleportDestinationResponse{Found: true, DestinationMapID: 389}, nil).Once()
	registry.On("SelectGameServerForPlayer", mock.Anything, mock.MatchedBy(func(req *pbServ.SelectGameServerForPlayerRequest) bool {
		return req.MapID == 389
	})).Return(&pbServ.SelectGameServerForPlayerResponse{
		Status:     pbServ.SelectGameServerForPlayerResponse_OK,
		GameServer: &pbServ.Server{ID: "current"},
	}, nil).Once()
	worldSocket.On("SendPacket", p).Once()

	require.NoError(t, s.HandleAreaTrigger(context.Background(), p))
	registry.AssertExpectations(t)
	worldClient.AssertExpectations(t)
	worldSocket.AssertExpectations(t)
}

func TestTimeSyncReplaysPendingAreaTrigger(t *testing.T) {
	gameSocket := &socketMock.Socket{}
	worldSocket := &socketMock.Socket{}
	pending := packet.NewWriterWithSize(packet.CMsgAreaTrigger, 4).Uint32(2230).ToPacket()
	timeSync := packet.NewWriterWithSize(packet.SMsgTimeSyncReq, 4).Uint32(1).ToPacket()
	s := &GameSession{
		gameSocket:         gameSocket,
		worldSocket:        worldSocket,
		worldEntryPending:  true,
		pendingAreaTrigger: pending,
	}
	gameSocket.On("SendPacket", timeSync).Once()
	worldSocket.On("SendPacket", pending).Once()

	require.NoError(t, s.InterceptSMsgTimeSyncReq(context.Background(), timeSync))
	require.False(t, s.worldEntryPending)
	require.Nil(t, s.pendingAreaTrigger)
	gameSocket.AssertExpectations(t)
	worldSocket.AssertExpectations(t)
}
