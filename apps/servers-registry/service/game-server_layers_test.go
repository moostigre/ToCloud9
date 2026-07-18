package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/walkline/ToCloud9/apps/servers-registry/repo"
)

func TestApplyLayerAssignmentsUsesOneGameServerPerMapLayer(t *testing.T) {
	servers := []repo.GameServer{
		{ID: "layer-1-a", LayerID: 1, AssignedMapsToHandle: []uint32{1}},
		{ID: "layer-1-b", LayerID: 1, AssignedMapsToHandle: []uint32{1}},
		{ID: "layer-2", LayerID: 2},
		{ID: "legacy", LayerID: 0, AssignedMapsToHandle: []uint32{1}},
	}

	applyLayerAssignments(servers, map[uint32]uint32{1: 2})

	hosts := map[uint32][]string{}
	for _, server := range servers {
		for _, mapID := range server.AssignedMapsToHandle {
			if mapID == 1 {
				hosts[server.LayerID] = append(hosts[server.LayerID], server.ID)
			}
		}
	}
	require.Equal(t, []string{"layer-1-a"}, hosts[1])
	require.Equal(t, []string{"layer-2"}, hosts[2])
	require.Empty(t, hosts[0])
}
