package session

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContainsLayerMapID(t *testing.T) {
	require.True(t, containsLayerMapID([]uint32{30, 33, 389}, 33))
	require.False(t, containsLayerMapID([]uint32{30, 33, 389}, 1))
}
