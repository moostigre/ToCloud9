package session

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/walkline/ToCloud9/apps/gateway/packet"
)

func TestLayerWorldTransferPackets(t *testing.T) {
	pending, newWorld := layerWorldTransferPackets(1, 10.5, -20.25, 30.75, 1.5)

	pendingPacket := pending.ToPacket()
	require.Equal(t, packet.SMsgTransferPending, pendingPacket.Opcode)
	require.Len(t, pendingPacket.Data, 4)
	require.Equal(t, uint32(1), binary.LittleEndian.Uint32(pendingPacket.Data))

	newWorldPacket := newWorld.ToPacket()
	require.Equal(t, packet.SMsgNewWorld, newWorldPacket.Opcode)
	require.Len(t, newWorldPacket.Data, 20)
	require.Equal(t, uint32(1), binary.LittleEndian.Uint32(newWorldPacket.Data[0:4]))
	require.Equal(t, float32(10.5), math.Float32frombits(binary.LittleEndian.Uint32(newWorldPacket.Data[4:8])))
	require.Equal(t, float32(-20.25), math.Float32frombits(binary.LittleEndian.Uint32(newWorldPacket.Data[8:12])))
	require.Equal(t, float32(30.75), math.Float32frombits(binary.LittleEndian.Uint32(newWorldPacket.Data[12:16])))
	require.Equal(t, float32(1.5), math.Float32frombits(binary.LittleEndian.Uint32(newWorldPacket.Data[16:20])))
}
