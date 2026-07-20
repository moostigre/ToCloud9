package session

import (
	"testing"

	"github.com/stretchr/testify/require"
	pbServ "github.com/walkline/ToCloud9/gen/servers-registry/pb"
)

func TestContainsLayerMapID(t *testing.T) {
	require.True(t, containsLayerMapID([]uint32{30, 33, 389}, 33))
	require.False(t, containsLayerMapID([]uint32{30, 33, 389}, 1))
}

func TestCurrentInstanceCoreNumberRequiresCurrentInstanceMap(t *testing.T) {
	cores := []*pbServ.GetInstancePoolStatsResponse_Core{
		{GameServerID: "one", MapIDs: []uint32{33, 389}},
		{GameServerID: "two", MapIDs: []uint32{33, 389}},
	}

	require.Equal(t, 2, currentInstanceCoreNumber(cores, "two", 389))
	require.Zero(t, currentInstanceCoreNumber(cores, "two", 1))
}
