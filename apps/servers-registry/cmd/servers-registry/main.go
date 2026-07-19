package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	nats "github.com/nats-io/nats.go"
	redis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"

	"github.com/walkline/ToCloud9/apps/servers-registry/config"
	"github.com/walkline/ToCloud9/apps/servers-registry/mapbalancing/binpack"
	"github.com/walkline/ToCloud9/apps/servers-registry/repo"
	"github.com/walkline/ToCloud9/apps/servers-registry/server"
	"github.com/walkline/ToCloud9/apps/servers-registry/service"
	"github.com/walkline/ToCloud9/gen/servers-registry/pb"
	"github.com/walkline/ToCloud9/shared/events"
	"github.com/walkline/ToCloud9/shared/healthandmetrics"
)

func main() {
	mainContext, cancel := context.WithCancel(context.Background())
	defer cancel()

	conf, err := config.LoadConfig()
	if err != nil {
		panic(err)
	}

	log.Logger = conf.Logger()

	lis, err := net.Listen("tcp4", ":"+conf.Port)
	if err != nil {
		log.Fatal().Err(err).Msg("can't listen for incoming connections")
	}

	nc, err := nats.Connect(
		conf.NatsURL,
		nats.PingInterval(20*time.Second),
		nats.MaxPingsOutstanding(5),
		nats.Timeout(10*time.Second),
		nats.Name("servers-registry"),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("can't connect to nats")
	}
	defer nc.Close()

	rdb, err := newRedisClient(conf)
	if err != nil {
		log.Fatal().Err(err).Msg("can't configure redis")
	}
	pingRes := rdb.Ping(context.Background())
	if pingRes.Err() != nil {
		log.Fatal().Err(pingRes.Err()).Msg("can't connect to redis")
	}
	defer rdb.Close()

	healthChecker := healthandmetrics.NewHealthChecker(time.Second*4, 4, healthandmetrics.NewHttpHealthCheckProcessor(time.Second*15))
	go healthChecker.Start()

	metricsConsumer := healthandmetrics.NewMetricsConsumer(time.Second*5, 3, healthandmetrics.NewHttpPrometheusMetricsReader(time.Second))
	go metricsConsumer.Start()

	supportedRealms := conf.RealmsID
	layerStore := repo.NewLayerRedisStore(rdb)
	if conf.AreaTriggerCatalogVersion == "" || strings.ContainsAny(conf.AreaTriggerCatalogVersion, "{}") {
		log.Fatal().Str("catalogVersion", conf.AreaTriggerCatalogVersion).Msg("invalid area-trigger catalog version")
	}
	portalStore := repo.NewPortalRedisStore(rdb, conf.AreaTriggerCatalogVersion)
	var instanceMaps []uint32
	if conf.AreaTriggerCatalogImportEnabled {
		worldDB, dbErr := sql.Open("mysql", conf.WorldDBConnection)
		if dbErr != nil {
			log.Fatal().Err(dbErr).Msg("can't open world database for area-trigger import")
		}
		if dbErr = worldDB.PingContext(mainContext); dbErr != nil {
			_ = worldDB.Close()
			log.Fatal().Err(dbErr).Msg("can't connect to world database for area-trigger import")
		}
		destinations, importErr := repo.LoadAreaTriggerTeleportDestinations(mainContext, worldDB)
		if importErr == nil {
			instanceMaps, importErr = repo.LoadInstanceMaps(mainContext, worldDB)
		}
		_ = worldDB.Close()
		if importErr != nil {
			log.Fatal().Err(importErr).Msg("can't import area-trigger teleport destinations")
		}
		if importErr = portalStore.ReplaceDestinations(mainContext, destinations); importErr != nil {
			log.Fatal().Err(importErr).Msg("can't publish area-trigger teleport destinations to redis")
		}
		if importErr = portalStore.ReplaceInstanceMaps(mainContext, instanceMaps); importErr != nil {
			log.Fatal().Err(importErr).Msg("can't publish instance map catalog to redis")
		}
		log.Info().Int("destinations", len(destinations)).Int("instanceMaps", len(instanceMaps)).Str("catalogVersion", conf.AreaTriggerCatalogVersion).Msg("Imported world routing catalog")
	}
	instanceMaps, err = portalStore.InstanceMaps(mainContext)
	if err != nil {
		log.Fatal().Err(err).Msg("can't load instance map catalog")
	}
	gameServersService, err := service.NewGameServer(
		mainContext,
		repo.NewGameServerRedisRepo(rdb),
		healthChecker,
		metricsConsumer,
		binpack.NewBinPackBalancer(binpack.DefaultMapsWeight), // TODO: implement providing custom maps weight list.
		events.NewServerRegistryProducerNatsJSON(nc, "0.0.1"),
		layerStore,
		supportedRealms,
	)
	if err != nil {
		log.Fatal().Err(err).Msg("can't create game server service")
	}

	gatewayService, err := service.NewGateway(
		mainContext,
		repo.NewGatewayRedisRepo(rdb),
		healthChecker,
		metricsConsumer,
		events.NewServerRegistryProducerNatsJSON(nc, "0.0.1"),
		[]uint32{1},
	)
	if err != nil {
		log.Fatal().Err(err).Msg("can't create gateway service")
	}

	layerService := service.NewLayer(gameServersService, layerStore)
	instancePoolService := service.NewInstancePool(gameServersService, portalStore, instanceMaps)
	portalService := service.NewPortal(gameServersService, layerService, instancePoolService, portalStore)
	startupLayers := make(map[uint32]uint32, len(conf.Layering.Maps)+len(conf.Layering.MapSpecs))
	for _, item := range conf.Layering.Maps {
		startupLayers[item.MapID] = item.Layers
	}
	for _, spec := range conf.Layering.MapSpecs {
		parts := strings.SplitN(spec, ":", 2)
		if len(parts) != 2 {
			log.Fatal().Str("mapLayer", spec).Msg("invalid LAYER_MAPS entry; expected mapID:layers")
		}
		mapID, mapErr := strconv.ParseUint(parts[0], 10, 32)
		layers, layerErr := strconv.ParseUint(parts[1], 10, 32)
		if mapErr != nil || layerErr != nil || layers == 0 {
			log.Fatal().Str("mapLayer", spec).Msg("invalid LAYER_MAPS entry")
		}
		startupLayers[uint32(mapID)] = uint32(layers)
	}
	if len(startupLayers) > 0 {
		for _, realmID := range supportedRealms {
			if err := layerService.UpdateConfiguration(mainContext, realmID, startupLayers); err != nil {
				log.Fatal().Err(err).Uint32("realmID", realmID).Msg("can't apply layer configuration")
			}
		}
	}
	for _, realmID := range supportedRealms {
		if err := gameServersService.ConfigureInstancePool(mainContext, realmID, instanceMaps, conf.InstancePoolReplicas); err != nil {
			log.Fatal().Err(err).Uint32("realmID", realmID).Msg("can't configure instance server pool")
		}
	}

	registryService := server.NewServersRegistry(gameServersService, gatewayService, layerService, instancePoolService, portalService)
	if conf.LogLevel == zerolog.DebugLevel {
		registryService = server.NewServersRegistryDebugLoggerMiddleware(registryService, log.Logger)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterServersRegistryServiceServer(
		grpcServer,
		registryService,
	)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		sig := <-sigCh
		fmt.Println("")
		log.Info().Msgf("🧨 Got signal %v, attempting graceful shutdown...", sig)
		grpcServer.GracefulStop()
		wg.Done()
	}()

	log.Info().Str("address", lis.Addr().String()).Msg("🚀 Servers Registry started!")

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatal().Err(err).Msg("couldn't serve")
	}

	wg.Wait()

	log.Info().Msg("👍 Server successfully stopped.")
}

func newRedisClient(conf *config.Config) (redis.UniversalClient, error) {
	if len(conf.RedisAddresses) > 0 {
		return redis.NewUniversalClient(&redis.UniversalOptions{
			Addrs:      conf.RedisAddresses,
			MasterName: conf.RedisMasterName,
			Username:   conf.RedisUsername,
			Password:   conf.RedisPassword,
			DB:         conf.RedisDB,
		}), nil
	}
	opt, err := redis.ParseURL(conf.RedisConnection)
	if err != nil {
		return nil, err
	}
	return redis.NewClient(opt), nil
}
