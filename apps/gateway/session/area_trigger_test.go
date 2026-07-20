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
		Status:            pbServ.SelectGameServerForAreaTriggerResponse_OK,
		DestinationMapID:  389,
		GameServer:        &pbServ.Server{ID: "current"},
		InstancePlacement: true,
	}, nil).Once()
	worldSocket.On("SendPacket", p).Once()

	require.NoError(t, s.HandleAreaTrigger(context.Background(), p))
	registry.AssertExpectations(t)
	worldSocket.AssertExpectations(t)
}

func TestAreaTriggerLeavingInstanceIsHandledByCurrentWorldServer(t *testing.T) {
	worldSocket := &socketMock.Socket{}
	registry := &servMock.ServersRegistryServiceClient{}
	s := &GameSession{
		character:             &LoggedInCharacter{GUID: 1, Map: 389},
		worldSocket:           worldSocket,
		serversRegistryClient: registry,
		currentGameServerID:   "instance-owner",
	}
	p := packet.NewWriterWithSize(packet.CMsgAreaTrigger, 4).Uint32(2226).ToPacket()
	registry.On("SelectGameServerForAreaTrigger", mock.Anything, mock.MatchedBy(func(req *pbServ.SelectGameServerForAreaTriggerRequest) bool {
		return req.AreaTriggerID == 2226 && req.CharacterGUID == 1
	})).Return(&pbServ.SelectGameServerForAreaTriggerResponse{
		Status:            pbServ.SelectGameServerForAreaTriggerResponse_OK,
		DestinationMapID:  1,
		GameServer:        &pbServ.Server{ID: "outdoor-layer", Address: "outdoor:9601"},
		InstancePlacement: false,
	}, nil).Once()
	worldSocket.On("SendPacket", p).Once()

	require.NoError(t, s.HandleAreaTrigger(context.Background(), p))
	require.Nil(t, s.pendingAreaTrigger)
	require.Equal(t, "instance-owner", s.currentGameServerID)
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

func TestSuccessfulInstanceResetFinalizesEveryPartyMemberPlacement(t *testing.T) {
	gameSocket := &socketMock.Socket{}
	registry := &servMock.ServersRegistryServiceClient{}
	s := &GameSession{
		character:             &LoggedInCharacter{GUID: 10},
		gameSocket:            gameSocket,
		serversRegistryClient: registry,
		currentGroupID:        77,
		currentGameServerID:   "current",
		pendingInstanceReset: &instanceResetHandoff{
			returnServer: &pbServ.Server{ID: "current"},
			owners:       []instanceResetOwner{{server: &pbServ.Server{ID: "owner"}, maps: map[uint32]bool{389: false}}},
			memberGUIDs:  []uint64{10, 11},
			groupID:      77,
		},
	}
	p := packet.NewWriterWithSize(packet.SMsgInstanceReset, 4).Uint32(389).ToPacket()
	registry.On("FinalizeInstanceReset", mock.Anything, mock.MatchedBy(func(req *pbServ.FinalizeInstanceResetRequest) bool {
		return req.RealmID == 0 && req.CharacterGUID == 10 && req.GroupID == 77 && req.MapID == 389 &&
			len(req.MemberGUIDs) == 2 && req.MemberGUIDs[1] == 11
	})).Return(&pbServ.FinalizeInstanceResetResponse{}, nil).Once()
	gameSocket.On("SendPacket", p).Once()

	require.NoError(t, s.InterceptInstanceReset(context.Background(), p))
	registry.AssertExpectations(t)
	gameSocket.AssertExpectations(t)
}
