package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/walkline/ToCloud9/apps/gateway/packet"
	socketMock "github.com/walkline/ToCloud9/apps/gateway/sockets/socketmock"
)

func TestInterceptNewWorldTracksCrossCoreDestination(t *testing.T) {
	gameSocket := &socketMock.Socket{}
	s := &GameSession{
		character:  &LoggedInCharacter{},
		gameSocket: gameSocket,
	}
	p := packet.NewWriterWithSize(packet.SMsgNewWorld, 20).
		Uint32(389).
		Float32(1.25).
		Float32(2.5).
		Float32(3.75).
		Float32(4.5).
		ToPacket()
	gameSocket.On("SendPacket", p).Once()

	require.NoError(t, s.InterceptNewWorld(context.Background(), p))
	require.NotNil(t, s.teleportingToNewMap)
	require.Equal(t, uint32(389), *s.teleportingToNewMap)
	require.Equal(t, &worldPortDestination{
		mapID: 389,
		x:     1.25,
		y:     2.5,
		z:     3.75,
		o:     4.5,
	}, s.pendingWorldPort)
	gameSocket.AssertExpectations(t)
}

func TestInterceptNewWorldDoesNotTrackSyntheticLayerTransfer(t *testing.T) {
	gameSocket := &socketMock.Socket{}
	ignoredMap := uint32(1)
	s := &GameSession{
		character: &LoggedInCharacter{
			ignoreNextInterceptToNewMap: &ignoredMap,
		},
		gameSocket: gameSocket,
	}
	p := packet.NewWriterWithSize(packet.SMsgNewWorld, 20).
		Uint32(ignoredMap).
		Float32(1).
		Float32(2).
		Float32(3).
		Float32(4).
		ToPacket()
	gameSocket.On("SendPacket", p).Once()

	require.NoError(t, s.InterceptNewWorld(context.Background(), p))
	require.Nil(t, s.teleportingToNewMap)
	require.Nil(t, s.pendingWorldPort)
	gameSocket.AssertExpectations(t)
}
