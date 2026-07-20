package session

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	root "github.com/walkline/ToCloud9/apps/gateway"
	eBroadcaster "github.com/walkline/ToCloud9/apps/gateway/events-broadcaster"
	"github.com/walkline/ToCloud9/apps/gateway/packet"
	pbChat "github.com/walkline/ToCloud9/gen/chat/pb"
	pbGroup "github.com/walkline/ToCloud9/gen/group/pb"
	pbGuild "github.com/walkline/ToCloud9/gen/guilds/pb"
	pbServ "github.com/walkline/ToCloud9/gen/servers-registry/pb"
)

type ChatType uint8

const (
	ChatTypeSystem ChatType = iota
	ChatTypeSay
	ChatTypeParty
	ChatTypeRaid
	ChatTypeGuild
	ChatTypeOfficer
	ChatTypeYell
	ChatTypeWhisper
	ChatTypeWhisperForeign
	ChatTypeWhisperInform
	ChatTypeChannel     = 0x11
	ChatTypeRaidLeader  = 0x27
	ChatTypePartyLeader = 0x33
)

func (s *GameSession) SendSysMessage(msg string) {
	resp := packet.NewWriterWithSize(packet.SMsgMessageChat, 0)
	resp.Uint8(uint8(ChatTypeSystem)) // chatType
	resp.Uint32(0)                    // language
	resp.Uint64(0)                    // sender
	resp.Uint32(0)                    // some flags
	resp.Uint64(0)                    // receiver
	resp.Uint32(uint32(len(msg) + 1))
	resp.String(msg)
	resp.Uint8(0) // chat tag
	s.gameSocket.Send(resp)
}

func (s *GameSession) HandleChatMessage(ctx context.Context, p *packet.Packet) error {
	r := p.Reader()
	msgType := r.Uint32()
	lang := r.Uint32()

	s.logger.Debug().
		Uint32("msgType", msgType).
		Uint32("language", lang).
		Msg("HandleChatMessage received")

	to := ""
	msg := ""
	switch ChatType(msgType) {
	case ChatTypeWhisper:
		to = r.String()
		msg = r.String()
		res, err := s.chatServiceClient.SendWhisperMessage(ctx, &pbChat.SendWhisperMessageRequest{
			Api:          root.Ver,
			RealmID:      root.RealmID,
			SenderGUID:   s.character.GUID,
			SenderName:   s.character.Name,
			SenderRace:   uint32(s.character.Race),
			Language:     lang,
			ReceiverName: to,
			Msg:          msg,
		})

		// TODO: handle response

		if err != nil {
			return err
		}

		resp := packet.NewWriterWithSize(packet.SMsgMessageChat, 0)
		resp.Uint8(uint8(ChatTypeWhisperInform))
		resp.Uint32(lang)
		resp.Uint64(res.ReceiverGUID)
		resp.Uint32(0) // some flags
		resp.Uint64(res.ReceiverGUID)
		resp.Uint32(uint32(len(msg) + 1))
		resp.String(msg)
		resp.Uint8(0) // chat tag
		s.gameSocket.Send(resp)
	case ChatTypeGuild:
		msg = r.String()

		handled, err := s.handleCommandMsgIfNeeded(ctx, msg)
		if err != nil {
			return err
		}

		if handled {
			return nil
		}

		_, err = s.guildServiceClient.SendGuildMessage(ctx, &pbGuild.SendGuildMessageParams{
			Api:              root.Ver,
			RealmID:          root.RealmID,
			SenderGUID:       s.character.GUID,
			Language:         lang,
			Message:          msg,
			IsOfficerMessage: false,
		})

		if err != nil {
			return err
		}

		resp := packet.NewWriterWithSize(packet.SMsgMessageChat, 0)
		resp.Uint8(uint8(ChatTypeGuild))
		resp.Uint32(lang)
		resp.Uint64(s.character.GUID)
		resp.Uint32(0) // some flags
		resp.Uint64(s.character.GUID)
		resp.Uint32(uint32(len(msg) + 1))
		resp.String(msg)
		resp.Uint8(0) // chat tag
		s.gameSocket.Send(resp)
	case ChatTypeParty, ChatTypePartyLeader, ChatTypeRaid, ChatTypeRaidLeader:
		msg = r.String()

		handled, err := s.handleCommandMsgIfNeeded(ctx, msg)
		if err != nil {
			return err
		}

		if handled {
			return nil
		}

		_, err = s.groupServiceClient.SendMessage(ctx, &pbGroup.SendGroupMessageParams{
			Api:         root.Ver,
			RealmID:     root.RealmID,
			SenderGUID:  s.character.GUID,
			Language:    lang,
			Message:     msg,
			MessageType: msgType,
		})

		if err != nil {
			return err
		}

		resp := packet.NewWriterWithSize(packet.SMsgMessageChat, 0)
		resp.Uint8(uint8(msgType))
		resp.Uint32(lang)
		resp.Uint64(s.character.GUID)
		resp.Uint32(0) // some flags
		resp.Uint64(s.character.GUID)
		resp.Uint32(uint32(len(msg) + 1))
		resp.String(msg)
		resp.Uint8(0) // chat tag
		s.gameSocket.Send(resp)

	case ChatTypeChannel:
		channelName := r.String()
		msg = r.String()

		handled, err := s.handleCommandMsgIfNeeded(ctx, msg)
		if err != nil {
			return err
		}

		if handled {
			return nil
		}

		// Send channel message through chat service
		return s.SendChannelMessageToChat(ctx, channelName, msg, lang)

	case ChatTypeSay:
		msg = r.String()

		handled, err := s.handleCommandMsgIfNeeded(ctx, msg)
		if err != nil {
			return err
		}

		if handled {
			return nil
		}

		if s.worldSocket != nil {
			s.worldSocket.WriteChannel() <- p
		}
	default:
		s.logger.Debug().
			Uint32("msgType", msgType).
			Uint32("language", lang).
			Msg("HandleChatMessage - default case (msgType decimal), forwarding to worldserver")
		if s.worldSocket != nil {
			s.worldSocket.WriteChannel() <- p
		}
	}

	return nil
}

func (s *GameSession) HandleEventIncomingWhisperMessage(ctx context.Context, e *eBroadcaster.Event) error {
	eventData := e.Payload.(*eBroadcaster.IncomingWhisperPayload)

	resp := packet.NewWriterWithSize(packet.SMsgMessageChat, 0)
	resp.Uint8(uint8(ChatTypeWhisper))
	resp.Uint32(eventData.Language)
	resp.Uint64(eventData.SenderGUID)
	resp.Uint32(0) // some flags
	resp.Uint64(eventData.SenderGUID)
	resp.Uint32(uint32(len(eventData.Msg) + 1))
	resp.String(eventData.Msg)
	resp.Uint8(0) // chat tag
	s.gameSocket.Send(resp)

	return nil
}

// TODO: rewrite commands handler with some better and more manageable constructions.
func (s *GameSession) handleCommandMsgIfNeeded(ctx context.Context, msg string) ( /* isHandled */ bool, error) {
	if msg == ".layer" || strings.HasPrefix(msg, ".layer ") {
		return true, s.handleLayerCommand(ctx, strings.Fields(msg))
	}
	const TC9CommandPrefix = ".tc9 "
	if !strings.HasPrefix(msg, TC9CommandPrefix) {
		return false, nil
	}

	args := strings.Split(msg[len(TC9CommandPrefix):], " ")
	if len(args) == 0 {
		return true, nil
	}

	switch strings.ToLower(args[0]) {
	case "worldservers", "ws", "gameservers", "gs":
		if len(args) < 2 {
			s.SendSysMessage("not enough args")
			return true, nil
		}

		switch strings.ToLower(args[1]) {
		case "list", "ls":
			return true, s.handleCommandMsgListGameServers(ctx)
		default:
			s.SendSysMessage("unk subcommand")
		}
	case "gateways", "gw":
		if len(args) < 2 {
			s.SendSysMessage("not enough args")
			return true, nil
		}

		switch strings.ToLower(args[1]) {
		case "list", "ls":
			return true, s.handleCommandMsgListGateways(ctx)
		default:
			s.SendSysMessage("unk subcommand")
		}

	default:
		s.SendSysMessage("unk command")
	}
	return true, nil
}

func (s *GameSession) handleLayerCommand(ctx context.Context, args []string) error {
	if s.character == nil {
		return nil
	}
	if len(args) == 1 {
		return s.handleLayerStatus(ctx)
	}
	if len(args) == 2 {
		switch strings.ToLower(args[1]) {
		case "config":
			return s.handleLayerConfigOrSwitch(ctx, []string{".layer"})
		case "help":
			s.sendLayerHelp()
			return nil
		}
	}
	return s.handleLayerConfigOrSwitch(ctx, args)
}

func (s *GameSession) handleLayerStatus(ctx context.Context) error {
	instanceStats, err := s.serversRegistryClient.GetInstancePoolStats(ctx, &pbServ.GetInstancePoolStatsRequest{
		Api: root.SupportedServerRegistryVer, RealmID: root.RealmID,
	})
	if err != nil {
		return err
	}
	if coreNumber := currentInstanceCoreNumber(instanceStats.Cores, s.currentGameServerID, s.character.Map); coreNumber != 0 {
		s.SendSysMessage(fmt.Sprintf("You are on instance core %d.", coreNumber))
		return nil
	}
	if s.currentLayerID != 0 {
		s.SendSysMessage(fmt.Sprintf("You are on layer %d.", s.currentLayerID))
		return nil
	}
	s.SendSysMessage("You are on a map that is not currently layered.")
	return nil
}

func (s *GameSession) sendLayerHelp() {
	s.SendSysMessage("Layer command help:")
	s.SendSysMessage("  .layer - show your current layer or instance core")
	s.SendSysMessage("  .layer config - show the complete layer and instance-pool configuration")
	s.SendSysMessage("  .layer switch <number> - switch to a layer available for your current map")
	s.SendSysMessage("  .layer help - show this command reference")
}

func (s *GameSession) handleLayerConfigOrSwitch(ctx context.Context, args []string) error {
	if len(args) == 1 {
		configuration, err := s.serversRegistryClient.GetMapLayerConfiguration(ctx, &pbServ.GetMapLayerConfigurationRequest{
			Api: root.SupportedServerRegistryVer, RealmID: root.RealmID,
		})
		if err != nil {
			return err
		}
		s.SendSysMessage(fmt.Sprintf("Layering overview: %d configured non-instance maps.", len(configuration.Maps)))
		layerCores := make(map[string]*pbServ.GetLayerStatsResponse_Layer)
		for _, configuredMap := range configuration.Maps {
			stats, statsErr := s.serversRegistryClient.GetLayerStats(ctx, &pbServ.GetLayerStatsRequest{
				Api: root.SupportedServerRegistryVer, RealmID: root.RealmID, MapID: configuredMap.MapID,
			})
			if statsErr != nil {
				return statsErr
			}
			marker := ""
			if configuredMap.MapID == s.character.Map {
				marker = fmt.Sprintf(" (current map, layer %d)", s.currentLayerID)
			}
			s.SendSysMessage(fmt.Sprintf("Map %d: %d configured layers%s", configuredMap.MapID, configuredMap.LayerCount, marker))
			layersByID := make(map[uint32]*pbServ.GetLayerStatsResponse_Layer, len(stats.Layers))
			for _, layer := range stats.Layers {
				layersByID[layer.LayerID] = layer
				layerCores[layer.GameServerID] = layer
			}
			for layerID := uint32(1); layerID <= configuredMap.LayerCount; layerID++ {
				layerMarker := ""
				if configuredMap.MapID == s.character.Map && layerID == s.currentLayerID {
					layerMarker = " (you)"
				}
				layer := layersByID[layerID]
				if layer == nil {
					s.SendSysMessage(fmt.Sprintf("  Layer %d: unavailable%s", layerID, layerMarker))
					continue
				}
				message := fmt.Sprintf("  Layer %d: available%s", layerID, layerMarker)
				if s.showGameserverConnChangeToClient {
					message += fmt.Sprintf("; gameserver %s (%s)", layer.GameServerID, layer.Address)
				}
				s.SendSysMessage(message)
			}
		}
		cores := make([]*pbServ.GetLayerStatsResponse_Layer, 0, len(layerCores))
		for _, core := range layerCores {
			cores = append(cores, core)
		}
		sort.Slice(cores, func(i, j int) bool { return cores[i].LayerID < cores[j].LayerID })
		s.SendSysMessage(fmt.Sprintf("Layer populations: %d layers.", len(cores)))
		for _, core := range cores {
			message := fmt.Sprintf("  Layer %d: approximately %d connected players", core.LayerID, core.Players)
			if s.showGameserverConnChangeToClient {
				message += fmt.Sprintf("; gameserver %s (%s)", core.GameServerID, core.Address)
			}
			s.SendSysMessage(message)
		}

		instanceStats, err := s.serversRegistryClient.GetInstancePoolStats(ctx, &pbServ.GetInstancePoolStatsRequest{
			Api: root.SupportedServerRegistryVer, RealmID: root.RealmID,
		})
		if err != nil {
			return err
		}
		s.SendSysMessage(fmt.Sprintf("Instance pool: %d configured cores.", len(instanceStats.Cores)))
		for index, core := range instanceStats.Cores {
			marker := ""
			if core.GameServerID == s.currentGameServerID && containsLayerMapID(core.MapIDs, s.character.Map) {
				marker = " (you)"
			}
			message := fmt.Sprintf("  Core %d: %d group/raid instance placements, %d supported instance maps%s", index+1, core.GroupPlacements, len(core.MapIDs), marker)
			if s.showGameserverConnChangeToClient {
				message += fmt.Sprintf("; gameserver %s (%s)", core.GameServerID, core.Address)
			}
			s.SendSysMessage(message)
		}
		return nil
	}
	if len(args) != 3 || strings.ToLower(args[1]) != "switch" {
		s.sendLayerHelp()
		return nil
	}
	layerID, err := strconv.ParseUint(args[2], 10, 32)
	if err != nil || layerID == 0 {
		s.SendSysMessage("Layer number must be a positive integer.")
		return nil
	}
	selection, err := s.selectLayerGameServer(ctx, 0, uint32(layerID))
	if err != nil {
		return err
	}
	if selection == nil || selection.Status == pbServ.SelectGameServerForPlayerResponse_LAYER_NOT_FOUND {
		s.SendSysMessage("That layer does not exist.")
		return nil
	}
	if selection.Status != pbServ.SelectGameServerForPlayerResponse_OK || selection.GameServer == nil {
		s.SendSysMessage("That layer has no available gameserver for this map.")
		return nil
	}
	if selection.GameServer.ID == s.currentGameServerID {
		s.SendSysMessage(fmt.Sprintf("You are already on layer %d.", layerID))
		return nil
	}
	s.SendSysMessage(fmt.Sprintf("Switching to layer %d.", layerID))
	if err := s.redirectToSelectedLayer(ctx, selection.GameServer); err != nil {
		return err
	}
	return nil
}

func containsLayerMapID(mapIDs []uint32, wanted uint32) bool {
	index := sort.Search(len(mapIDs), func(i int) bool { return mapIDs[i] >= wanted })
	return index < len(mapIDs) && mapIDs[index] == wanted
}

func currentInstanceCoreNumber(cores []*pbServ.GetInstancePoolStatsResponse_Core, gameServerID string, mapID uint32) int {
	for index, core := range cores {
		if core.GameServerID == gameServerID && containsLayerMapID(core.MapIDs, mapID) {
			return index + 1
		}
	}
	return 0
}

func (s *GameSession) handleCommandMsgListGameServers(ctx context.Context) error {
	resp, err := s.serversRegistryClient.ListAllGameServers(ctx, &pbServ.ListAllGameServersRequest{
		Api: root.SupportedServerRegistryVer,
	})
	if err != nil {
		return err
	}

	printServer := func(server *pbServ.GameServerDetailed) {
		mapsAvailable := "all"
		if len(server.AvailableMaps) > 0 {
			mapsAvailable = ""
			for _, availableMap := range server.AvailableMaps {
				mapsAvailable += fmt.Sprintf("%d ", availableMap)
			}
		}

		const maxMapsToShow = 8
		assignedMaps := ""
		if len(server.AssignedMaps) > maxMapsToShow {
			for i := 0; i < maxMapsToShow; i++ {
				assignedMaps += fmt.Sprintf("%d ", server.AssignedMaps[i])
			}
			assignedMaps += fmt.Sprintf("and %d more", len(server.AssignedMaps)-maxMapsToShow)
		} else {
			for i := 0; i < len(server.AssignedMaps); i++ {
				assignedMaps += fmt.Sprintf("%d ", server.AssignedMaps[i])
			}
		}

		isCurrentlyUsing := false
		if s.worldSocket != nil && s.worldSocket.Address() == server.Address {
			isCurrentlyUsing = true
		}

		s.SendSysMessage(fmt.Sprintf("> Node address: %s.", server.Address))
		s.SendSysMessage(fmt.Sprintf("  Available maps: %s.", mapsAvailable))
		s.SendSysMessage(fmt.Sprintf("  Assigned maps: %s.", assignedMaps))
		s.SendSysMessage(fmt.Sprintf("  Active connections: %d.", server.ActiveConnections))
		s.SendSysMessage(
			fmt.Sprintf(
				"  Diff (mean, median, 95, 99, max): %dms, %dms, %dms, %dms, %dms.",
				server.Diff.Mean, server.Diff.Median, server.Diff.Percentile95,
				server.Diff.Percentile99, server.Diff.Max,
			),
		)

		if isCurrentlyUsing {
			s.SendSysMessage("  You are |cff4CFF00connected |rto this one.")
		}

		s.SendSysMessage(" ")
	}

	var crossrealms []*pbServ.GameServerDetailed
	perRealm := make(map[uint32][]*pbServ.GameServerDetailed)
	for _, server := range resp.GameServers {
		if server.IsCrossRealm {
			crossrealms = append(crossrealms, server)
			continue
		}

		perRealm[server.RealmID] = append(perRealm[server.RealmID], server)
	}

	if len(crossrealms) > 0 {
		s.SendSysMessage(fmt.Sprintf("List of available |cff4f90ffcrossrealm|r worldservers:"))
		for _, server := range crossrealms {
			printServer(server)
		}
	}

	for realm, servers := range perRealm {
		s.SendSysMessage(fmt.Sprintf("List of available worldservers for |cff4f90ffrealm %d|r:", realm))
		for _, server := range servers {
			printServer(server)
		}
	}

	return nil
}

func (s *GameSession) handleCommandMsgListGateways(ctx context.Context) error {
	resp, err := s.serversRegistryClient.ListGatewaysForRealm(ctx, &pbServ.ListGatewaysForRealmRequest{
		Api:     root.SupportedServerRegistryVer,
		RealmID: root.RealmID,
	})
	if err != nil {
		return err
	}

	s.SendSysMessage("List of available |cffF84519gateways|r:")

	for _, server := range resp.Gateways {
		isCurrentlyUsing := root.RetrievedGatewayID == server.Id

		s.SendSysMessage(fmt.Sprintf("> Node healthCheckAddress: %s.", server.HealthAddress))
		s.SendSysMessage(fmt.Sprintf("  Active connections: %d.", server.ActiveConnections))
		if isCurrentlyUsing {
			s.SendSysMessage("  You are |cff4CFF00connected |rto this one.")
		}

		s.SendSysMessage(" ")
	}

	return nil
}
