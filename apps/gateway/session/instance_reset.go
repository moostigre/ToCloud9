package session

import (
	"context"
	"fmt"
	"sort"
	"time"

	root "github.com/walkline/ToCloud9/apps/gateway"
	"github.com/walkline/ToCloud9/apps/gateway/packet"
	pbGroup "github.com/walkline/ToCloud9/gen/group/pb"
	pbServ "github.com/walkline/ToCloud9/gen/servers-registry/pb"
)

const instanceResetOwnerTimeout = 5 * time.Second

type instanceResetOwner struct {
	server *pbServ.Server
	maps   map[uint32]bool
}

type instanceResetHandoff struct {
	request      *packet.Packet
	returnServer *pbServ.Server
	owners       []instanceResetOwner
	ownerIndex   int
	requestSent  bool
	generation   uint64
	memberGUIDs  []uint64
	groupID      uint32
}

// HandleResetInstances relays AzerothCore's native reset request through the
// authenticated character session on every core that owns one of the party's
// instances. AzerothCore remains the instance lifecycle authority.
func (s *GameSession) HandleResetInstances(ctx context.Context, p *packet.Packet) error {
	if s.character == nil || s.worldSocket == nil || s.serversRegistryClient == nil {
		return nil
	}
	if s.pendingInstanceReset != nil {
		return nil
	}
	groupID := s.groupIDForPlayer(ctx, s.character.GUID)
	members, leader, err := s.instanceResetMembers(ctx, groupID)
	if err != nil {
		return err
	}
	if !leader {
		// Preserve AzerothCore's native non-leader behavior.
		s.worldSocket.SendPacket(p)
		return nil
	}
	result, err := s.serversRegistryClient.GetInstanceResetTargets(ctx, &pbServ.GetInstanceResetTargetsRequest{
		Api: root.SupportedServerRegistryVer, RealmID: root.RealmID,
		CharacterGUID: s.character.GUID, GroupID: groupID,
	})
	if err != nil {
		return fmt.Errorf("get instance reset targets: %w", err)
	}
	if len(result.Targets) == 0 {
		s.worldSocket.SendPacket(p)
		return nil
	}

	returnSelection, err := s.selectGameServerForMap(ctx, s.character.GUID, s.character.Map)
	if err != nil {
		return fmt.Errorf("select return gameserver for instance reset: %w", err)
	}
	if returnSelection.Status != pbServ.SelectGameServerForPlayerResponse_OK || returnSelection.GameServer == nil {
		return fmt.Errorf("no return gameserver is available for map %d", s.character.Map)
	}

	ownersByID := make(map[string]*instanceResetOwner)
	for _, target := range result.Targets {
		if target.GameServer == nil {
			continue
		}
		owner := ownersByID[target.GameServer.ID]
		if owner == nil {
			owner = &instanceResetOwner{server: target.GameServer, maps: make(map[uint32]bool)}
			ownersByID[target.GameServer.ID] = owner
		}
		owner.maps[target.MapID] = false
	}
	owners := make([]instanceResetOwner, 0, len(ownersByID))
	for _, owner := range ownersByID {
		owners = append(owners, *owner)
	}
	sort.Slice(owners, func(i, j int) bool { return owners[i].server.ID < owners[j].server.ID })
	if len(owners) == 0 {
		s.worldSocket.SendPacket(p)
		return nil
	}

	s.pendingInstanceReset = &instanceResetHandoff{
		request: p, returnServer: returnSelection.GameServer, owners: owners, memberGUIDs: members, groupID: groupID,
	}
	return s.advanceInstanceReset(ctx)
}

func (s *GameSession) instanceResetMembers(ctx context.Context, groupID uint32) ([]uint64, bool, error) {
	if groupID == 0 {
		return []uint64{s.character.GUID}, true, nil
	}
	response, err := s.groupServiceClient.GetGroup(ctx, &pbGroup.GetGroupRequest{
		Api: root.SupportedGroupServiceVer, RealmID: root.RealmID, GroupID: groupID,
	})
	if err != nil {
		return nil, false, NewGroupServiceUnavailableErr(err)
	}
	if response.Group == nil || response.Group.Leader != s.character.GUID {
		return nil, false, nil
	}
	members := make([]uint64, 0, len(response.Group.Members)+1)
	seen := make(map[uint64]struct{}, len(response.Group.Members)+1)
	members = append(members, s.character.GUID)
	seen[s.character.GUID] = struct{}{}
	for _, member := range response.Group.Members {
		if member.Guid == 0 {
			continue
		}
		if _, exists := seen[member.Guid]; exists {
			continue
		}
		seen[member.Guid] = struct{}{}
		members = append(members, member.Guid)
	}
	return members, true, nil
}

func (s *GameSession) advanceInstanceReset(ctx context.Context) error {
	handoff := s.pendingInstanceReset
	if handoff == nil {
		return nil
	}
	if handoff.ownerIndex >= len(handoff.owners) {
		s.pendingInstanceReset = nil
		return s.returnFromInstanceReset(ctx, handoff.returnServer)
	}
	owner := &handoff.owners[handoff.ownerIndex]
	handoff.requestSent = false
	handoff.generation++
	for mapID := range owner.maps {
		owner.maps[mapID] = false
	}
	if owner.server.ID != s.currentGameServerID && owner.server.Address != s.worldSocket.Address() {
		s.worldEntryPending = true
		if err := s.redirectPlayerToGameServer(ctx, s.character.GUID, owner.server.Address); err != nil {
			s.pendingInstanceReset = nil
			s.worldEntryPending = false
			if returnErr := s.returnFromInstanceReset(ctx, handoff.returnServer); returnErr != nil {
				return fmt.Errorf("redirect to instance owner for reset: %w (return also failed: %v)", err, returnErr)
			}
			return fmt.Errorf("redirect to instance owner for reset: %w", err)
		}
		s.currentGameServerID = owner.server.ID
		s.currentLayerID = owner.server.LayerID
		return nil
	}
	s.sendPendingInstanceReset()
	return nil
}

func (s *GameSession) sendPendingInstanceReset() {
	handoff := s.pendingInstanceReset
	if handoff == nil || handoff.requestSent || s.worldSocket == nil {
		return
	}
	handoff.requestSent = true
	generation, ownerIndex := handoff.generation, handoff.ownerIndex
	s.worldSocket.SendPacket(handoff.request)
	time.AfterFunc(instanceResetOwnerTimeout, func() {
		resume := func(session *GameSession) {
			pending := session.pendingInstanceReset
			if pending == nil || pending.generation != generation || pending.ownerIndex != ownerIndex {
				return
			}
			session.logger.Warn().Int("ownerIndex", ownerIndex).Msg("instance reset owner timed out; aborting handoff")
			session.SendSysMessage("Instance reset timed out; please try again.")
			session.pendingInstanceReset = nil
			ctx, cancel := context.WithTimeout(session.ctx, 10*time.Second)
			defer cancel()
			if returnErr := session.returnFromInstanceReset(ctx, pending.returnServer); returnErr != nil {
				session.logger.Error().Err(returnErr).Msg("failed to return after instance reset handoff timeout")
			}
		}
		select {
		case <-s.ctx.Done():
		case s.sessionSafeFuChan <- resume:
		}
	})
}

func (s *GameSession) completeInstanceResetMap(ctx context.Context, mapID uint32, succeeded bool) error {
	handoff := s.pendingInstanceReset
	if handoff == nil || handoff.ownerIndex >= len(handoff.owners) {
		return nil
	}
	owner := &handoff.owners[handoff.ownerIndex]
	if _, expected := owner.maps[mapID]; !expected {
		return nil
	}
	if succeeded {
		request := &pbServ.FinalizeInstanceResetRequest{
			Api: root.SupportedServerRegistryVer, RealmID: root.RealmID, CharacterGUID: s.character.GUID,
			GroupID: handoff.groupID, MapID: mapID, MemberGUIDs: handoff.memberGUIDs,
		}
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			_, err = s.serversRegistryClient.FinalizeInstanceReset(ctx, request)
			if err == nil {
				break
			}
			if attempt < 2 {
				time.Sleep(100 * time.Millisecond)
			}
		}
		if err != nil {
			return fmt.Errorf("finalize instance reset placement: %w", err)
		}
	}
	owner.maps[mapID] = true
	for _, complete := range owner.maps {
		if !complete {
			return nil
		}
	}
	handoff.ownerIndex++
	return s.advanceInstanceReset(ctx)
}

func (s *GameSession) returnFromInstanceReset(ctx context.Context, server *pbServ.Server) error {
	if server == nil || server.ID == s.currentGameServerID {
		return nil
	}
	if err := s.redirectPlayerToGameServer(ctx, s.character.GUID, server.Address); err != nil {
		return fmt.Errorf("return from instance reset owner: %w", err)
	}
	s.currentGameServerID = server.ID
	s.currentLayerID = server.LayerID
	return nil
}
