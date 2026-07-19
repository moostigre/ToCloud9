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
)

func TestAreaTriggerWithoutTeleportIsForwarded(t *testing.T) {
	worldSocket := &socketMock.Socket{}
	registry := &servMock.ServersRegistryServiceClient{}
	s := &GameSession{
		character:             &LoggedInCharacter{GUID: 1},
		worldSocket:           worldSocket,
		serversRegistryClient: registry,
	}
	p := packet.NewWriterWithSize(packet.CMsgAreaTrigger, 4).Uint32(123).ToPacket()
	registry.On("SelectGameServerForAreaTrigger", mock.Anything, mock.MatchedBy(func(req *pbServ.SelectGameServerForAreaTriggerRequest) bool {
		return req.AreaTriggerID == 123 && req.CharacterGUID == 1
	})).Return(&pbServ.SelectGameServerForAreaTriggerResponse{Status: pbServ.SelectGameServerForAreaTriggerResponse_TRIGGER_NOT_FOUND}, nil).Once()
	worldSocket.On("SendPacket", p).Once()

	require.NoError(t, s.HandleAreaTrigger(context.Background(), p))
	registry.AssertExpectations(t)
	worldSocket.AssertExpectations(t)
}

func TestAreaTriggerAlreadyOnSelectedServerIsForwarded(t *testing.T) {
	worldSocket := &socketMock.Socket{}
	registry := &servMock.ServersRegistryServiceClient{}
	s := &GameSession{
		character:             &LoggedInCharacter{GUID: 1},
		worldSocket:           worldSocket,
		serversRegistryClient: registry,
		currentGameServerID:   "current",
	}
	p := packet.NewWriterWithSize(packet.CMsgAreaTrigger, 4).Uint32(2230).ToPacket()
	registry.On("SelectGameServerForAreaTrigger", mock.Anything, mock.MatchedBy(func(req *pbServ.SelectGameServerForAreaTriggerRequest) bool {
		return req.AreaTriggerID == 2230
	})).Return(&pbServ.SelectGameServerForAreaTriggerResponse{
		Status:           pbServ.SelectGameServerForAreaTriggerResponse_OK,
		DestinationMapID: 389,
		GameServer:       &pbServ.Server{ID: "current"},
	}, nil).Once()
	worldSocket.On("SendPacket", p).Once()

	require.NoError(t, s.HandleAreaTrigger(context.Background(), p))
	registry.AssertExpectations(t)
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
