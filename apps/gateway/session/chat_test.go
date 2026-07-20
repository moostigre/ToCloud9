package session

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLayerMapIDChunksSortsAndPreservesEveryMap(t *testing.T) {
	chunks := layerMapIDChunks([]uint32{571, 33, 1, 530, 0}, 2)

	require.Equal(t, []string{"0, 1", "33, 530", "571"}, chunks)
}

func TestContainsLayerMapID(t *testing.T) {
	require.True(t, containsLayerMapID([]uint32{30, 33, 389}, 33))
	require.False(t, containsLayerMapID([]uint32{30, 33, 389}, 1))
}
