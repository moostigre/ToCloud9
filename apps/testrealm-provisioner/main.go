package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Config struct {
	ListenAddress         string
	StateFile             string
	GitHubOwner           string
	GitHubRepo            string
	GitHubToken           string
	ImageCatalogFile      string
	AllowedImagePrefix    string
	AllowedImporterPrefix string
	CatalogueSecret       string
	ActivitySecret        string
	MaxRunning            int
	IdleAfter             time.Duration
	PurgeAfter            time.Duration
	OperatorURL           string
	OperatorSecret        string
	BuildOwner            string
	BuildRepo             string
	BuildWorkflow         string
	BuildRef              string
	MaxBuilds             int
	MaintenanceMessage    string
}

func loadConfig() (Config, error) {
	c := Config{ListenAddress: env("LISTEN_ADDRESS", ":8080"), StateFile: env("STATE_FILE", "/data/testrealm/state.json"), GitHubOwner: env("GITHUB_OWNER", "azerothcore"), GitHubRepo: env("GITHUB_REPO", "azerothcore-wotlk"), GitHubToken: os.Getenv("GITHUB_TOKEN"), ImageCatalogFile: env("IMAGE_CATALOG_FILE", "/data/catalog/images.json"), AllowedImagePrefix: env("ALLOWED_IMAGE_PREFIX", "ghcr.io/moostigre/tc9-ac-pr"), AllowedImporterPrefix: env("ALLOWED_IMPORTER_PREFIX", "ghcr.io/moostigre/tc9-ac-pr-dbimport"), CatalogueSecret: os.Getenv("CATALOGUE_HMAC_SECRET"), ActivitySecret: os.Getenv("ACTIVITY_HMAC_SECRET"), MaxRunning: envInt("MAX_RUNNING_REALMS", 5), IdleAfter: envDuration("IDLE_AFTER", "30m"), PurgeAfter: envDuration("PURGE_AFTER", "48h"), OperatorURL: os.Getenv("REALM_OPERATOR_URL"), OperatorSecret: os.Getenv("REALM_OPERATOR_SECRET"), BuildOwner: env("BUILD_GITHUB_OWNER", "moostigre"), BuildRepo: env("BUILD_GITHUB_REPO", "ToCloud9"), BuildWorkflow: env("BUILD_GITHUB_WORKFLOW", "build-ac-pr-testrealm.yml"), BuildRef: env("BUILD_GITHUB_REF", "master"), MaxBuilds: envInt("MAX_CONCURRENT_BUILDS", 25), MaintenanceMessage: strings.TrimSpace(os.Getenv("MAINTENANCE_MESSAGE"))}
	if c.MaxRunning < 1 || c.MaxRunning > 5 {
		return c, errors.New("MAX_RUNNING_REALMS must be between 1 and 5")
	}
	if c.MaxBuilds < 1 || c.MaxBuilds > 25 {
		return c, errors.New("MAX_CONCURRENT_BUILDS must be between 1 and 25")
	}
	if c.IdleAfter < 5*time.Minute || c.PurgeAfter < time.Hour {
		return c, errors.New("lifecycle durations are below safe minimums")
	}
	if len(c.ActivitySecret) < 32 {
		return c, errors.New("ACTIVITY_HMAC_SECRET must contain at least 32 characters")
	}
	if len(c.CatalogueSecret) < 32 {
		return c, errors.New("CATALOGUE_HMAC_SECRET must contain at least 32 characters")
	}
	return c, nil
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	store, err := newStore(cfg.StateFile)
	if err != nil {
		slog.Error("state initialization failed", "error", err)
		os.Exit(1)
	}
	var executor RealmExecutor = UnavailableExecutor{}
	provisioningAvailable := false
	if cfg.OperatorURL != "" {
		op, err := newOperatorExecutor(cfg.OperatorURL, cfg.OperatorSecret)
		if err != nil {
			slog.Error("operator initialization failed", "error", err)
			os.Exit(1)
		}
		executor = op
		provisioningAvailable = true
	} else {
		slog.Warn("realm provisioning disabled; set REALM_OPERATOR_URL to enable the trusted operator")
		if err := store.markUnbackedRealmsSimulated(); err != nil {
			slog.Error("could not repair simulated realm state", "error", err)
			os.Exit(1)
		}
	}
	client := &http.Client{Timeout: 15 * time.Second}
	server := &Server{store: store, github: &GitHubClient{http: client, owner: cfg.GitHubOwner, repo: cfg.GitHubRepo, token: cfg.GitHubToken}, builds: &BuildClient{http: client, owner: cfg.BuildOwner, repo: cfg.BuildRepo, workflow: cfg.BuildWorkflow, ref: cfg.BuildRef, token: cfg.GitHubToken, maxActive: cfg.MaxBuilds}, images: ImageCatalog{Path: cfg.ImageCatalogFile, AllowedPrefix: cfg.AllowedImagePrefix, AllowedImporterPrefix: cfg.AllowedImporterPrefix}, executor: executor, provisioningAvailable: provisioningAvailable, maintenanceMessage: cfg.MaintenanceMessage, activitySecret: []byte(cfg.ActivitySecret), catalogueSecret: []byte(cfg.CatalogueSecret), maxRunning: cfg.MaxRunning, idleAfter: cfg.IdleAfter, purgeAfter: cfg.PurgeAfter, rates: map[string]*rateBucket{}}
	httpServer := &http.Server{Addr: cfg.ListenAddress, Handler: server.routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go server.lifecycle(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	slog.Info("test realm provisioner listening", "address", cfg.ListenAddress, "max_running", cfg.MaxRunning)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
func envInt(name string, fallback int) int {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return -1
	}
	return n
}
func envDuration(name string, fallback string) time.Duration {
	v := env(name, fallback)
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0
	}
	return d
}
