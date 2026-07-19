package config

import (
	"github.com/walkline/ToCloud9/shared/config"
)

// Config is config of application
type Config struct {
	config.Logging `yaml:"logging"`

	// Port is port that would be used for grpc server
	Port string `yaml:"port" env:"PORT" env-default:"8999"`

	// RedisConnection is connection string for the redis connection
	RedisConnection string `yaml:"redisUrl" env:"REDIS_URL" env-default:"redis://:@redis:6379/0"`
	// RedisAddresses enables go-redis universal mode. One address selects a
	// standalone server, multiple addresses select Redis Cluster, and setting
	// RedisMasterName selects Sentinel failover mode.
	RedisAddresses  []string `yaml:"redisAddresses" env:"REDIS_ADDRESSES"`
	RedisMasterName string   `yaml:"redisMasterName" env:"REDIS_MASTER_NAME"`
	RedisUsername   string   `yaml:"redisUsername" env:"REDIS_USERNAME"`
	RedisPassword   string   `yaml:"redisPassword" env:"REDIS_PASSWORD"`
	RedisDB         int      `yaml:"redisDB" env:"REDIS_DB" env-default:"0"`

	// NatsURL is nats connection url
	NatsURL string `yaml:"natsUrl" env:"NATS_URL" env-default:"nats://nats:4222"`

	// RealmsIDs is id of realms that the system supports.
	RealmsID []uint32 `yaml:"realmsID" env:"REALMs_ID" env-default:"1"`

	// WorldDBConnection is used only to import immutable area-trigger teleport
	// destinations into Redis. Runtime placement reads Redis exclusively.
	WorldDBConnection               string `yaml:"worldDB" env:"WORLD_DB_CONNECTION" env-default:"trinity:trinity@tcp(127.0.0.1:3306)/acore_world"`
	AreaTriggerCatalogVersion       string `yaml:"areaTriggerCatalogVersion" env:"AREA_TRIGGER_CATALOG_VERSION" env-default:"default"`
	AreaTriggerCatalogImportEnabled bool   `yaml:"areaTriggerCatalogImportEnabled" env:"AREA_TRIGGER_CATALOG_IMPORT_ENABLED" env-default:"true"`
	InstancePoolReplicas            uint32 `yaml:"instancePoolReplicas" env:"INSTANCE_POOL_REPLICAS" env-default:"1"`

	Layering LayeringConfig `yaml:"layering"`
}

type LayeringConfig struct {
	Maps     []MapLayerConfig `yaml:"maps"`
	MapSpecs []string         `yaml:"-" env:"LAYER_MAPS"`
}

type MapLayerConfig struct {
	MapID  uint32 `yaml:"mapID"`
	Layers uint32 `yaml:"layers"`
}

// LoadConfig loads config from env variables
func LoadConfig() (*Config, error) {
	var c struct {
		Root Config `yaml:"servers-registry"`
	}

	err := config.LoadConfig(&c)
	if err != nil {
		return nil, err
	}

	return &c.Root, nil
}
