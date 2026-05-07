// Package main is the entry point for the NSSAAF Biz Pod.
// Spec: TS 29.526 v18.7.0
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/operator/nssAAF/internal/amf"
	"github.com/operator/nssAAF/internal/api/aiw"
	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/api/nssaa"
	"github.com/operator/nssAAF/internal/ausf"
	"github.com/operator/nssAAF/internal/cache/redis"
	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/crypto"
	"github.com/operator/nssAAF/internal/metrics"
	"github.com/operator/nssAAF/internal/nrf"
	"github.com/operator/nssAAF/internal/resilience"
	"github.com/operator/nssAAF/internal/storage/postgres"
	"github.com/operator/nssAAF/internal/tracing"
	"github.com/operator/nssAAF/internal/udm"
)

// BizPod holds all dependencies for the Biz Pod.
type BizPod struct {
	Server         *http.Server
	NRFClient      *nrf.Client
	SessionStore   *postgres.Store
	AIWSessionStore *postgres.AIWStore
	Pool           *postgres.Pool
	RedisPool      *redis.Pool
	DLQ            *redis.DLQ
	AAAClient      *httpAAAClient
	Logger         *slog.Logger
}

// BizPodOption configures a BizPod.
type BizPodOption func(*bizPodFactory)

// bizPodFactory creates BizPod instances with dependency injection.
type bizPodFactory struct {
	cfg    *config.Config
	logger *slog.Logger
	podID  string
}

// NewBizPodFactory creates a new factory.
func NewBizPodFactory(cfg *config.Config, opts ...BizPodOption) *bizPodFactory {
	f := &bizPodFactory{cfg: cfg, logger: slog.Default(), podID: "unknown"}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// WithLogger sets the logger on the factory.
func WithLogger(logger *slog.Logger) BizPodOption {
	return func(f *bizPodFactory) { f.logger = logger }
}

// WithPodID sets the pod ID for service discovery.
func WithPodID(podID string) BizPodOption {
	return func(f *bizPodFactory) { f.podID = podID }
}

// Build creates a fully initialized BizPod with all dependencies wired.
// The caller is responsible for closing resources via Close().
func (f *bizPodFactory) Build(ctx context.Context) (*BizPod, func(), error) {
	var cleanup func()

	// Initialize OpenTelemetry tracing
	tracingShutdown := tracing.Init("nssAAF-biz", f.cfg.Version, f.podID)
	cleanup = func() {
		tracingShutdown()
	}

	// Build API root URL
	apiRoot := f.cfg.Server.Addr
	if !hasScheme(apiRoot) {
		apiRoot = "http://" + apiRoot
	}

	// ─── PostgreSQL pool + session stores ────────────────────────────────
	pgPool, err := postgres.NewPool(ctx, postgres.Config{
		DSN: fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
			f.cfg.Database.User, f.cfg.Database.Password, f.cfg.Database.Host,
			f.cfg.Database.Port, f.cfg.Database.Name, f.cfg.Database.SSLMode),
		MaxConns:          int32(f.cfg.Database.MaxConns),
		MinConns:          int32(f.cfg.Database.MinConns),
		MaxConnLifetime:   f.cfg.Database.ConnMaxLifetime,
		MaxConnIdleTime:   10 * time.Minute,
		HealthCheckPeriod: 30 * time.Second,
	})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("postgres pool: %w", err)
	}
	prevCleanup := cleanup
	cleanup = func() {
		pgPool.Close()
		prevCleanup()
	}

	// ─── Run database migrations ─────────────────────────────────────────
	migrator := postgres.NewMigrator(pgPool)
	if err := migrator.Migrate(ctx); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("database migration: %w", err)
	}

	// ─── Crypto initialization ───────────────────────────────────────────
	var vaultCfg *crypto.VaultConfig
	if f.cfg.Crypto.VaultConfig != nil {
		vaultCfg = &crypto.VaultConfig{
			Address:    f.cfg.Crypto.VaultConfig.Address,
			KeyName:    f.cfg.Crypto.VaultConfig.KeyName,
			AuthMethod: f.cfg.Crypto.VaultConfig.AuthMethod,
			K8sRole:    f.cfg.Crypto.VaultConfig.K8sRole,
			Token:      f.cfg.Crypto.VaultConfig.Token,
			TokenFile:  f.cfg.Crypto.VaultConfig.TokenFile,
		}
	}
	if err := crypto.Init(&crypto.Config{
		KeyManager:     f.cfg.Crypto.KeyManager,
		MasterKeyHex:   f.cfg.Crypto.MasterKeyHex,
		KEKOverlapDays: f.cfg.Crypto.KEKOverlapDays,
		Vault:          vaultCfg,
		SoftHSM:        nil,
	}); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("crypto initialization: %w", err)
	}

	// Build encryptor using the initialized key manager
	encryptor, err := postgres.NewEncryptorFromKeyManager(crypto.KM())
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("session encryptor: %w", err)
	}

	// ─── Session stores ──────────────────────────────────────────────────
	nssaaStore := postgres.NewSessionStore(pgPool, encryptor)
	aiwStore := postgres.NewAIWSessionStore(pgPool, encryptor)

	// ─── Redis pool + DLQ ───────────────────────────────────────────────
	redisPool, err := redis.NewPool(ctx, redis.Config{
		Addrs:        []string{f.cfg.Redis.Addr},
		Password:     f.cfg.Redis.Password,
		DB:           f.cfg.Redis.DB,
		PoolSize:     f.cfg.Redis.PoolSize,
		MinIdleConns: 10,
		DialTimeout:  100 * time.Millisecond,
		ReadTimeout:  100 * time.Millisecond,
		WriteTimeout: 100 * time.Millisecond,
	})
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("redis pool: %w", err)
	}
	prevCleanup = cleanup
	cleanup = func() {
		_ = redisPool.Close()
		prevCleanup()
	}

	dlq := redis.NewDLQ(redisPool)
	go dlq.Process(ctx)

	// ─── Resilience ─────────────────────────────────────────────────────
	cbRegistry := resilience.NewRegistry(
		f.cfg.AAA.FailureThreshold,
		f.cfg.AAA.RecoveryTimeout,
		3*time.Second,
	)

	// ─── NRF client ─────────────────────────────────────────────────────
	nrfClient := nrf.NewClient(f.cfg.NRF)
	go nrfClient.RegisterAsync(ctx)
	go nrfClient.StartHeartbeat(ctx)

	// ─── UDM client ────────────────────────────────────────────────────
	udmClient := udm.NewClient(f.cfg.UDM, nrfClient)

	// ─── AUSF client ───────────────────────────────────────────────────
	ausfClient := ausf.NewClient(f.cfg.AUSF)

	// ─── AMF notifier ──────────────────────────────────────────────────
	_ = amf.NewClient(30*time.Second, cbRegistry, dlq)

	// ─── HTTP AAA client ────────────────────────────────────────────────
	tlsCfg := &tls.Config{}
	if f.cfg.Biz.UseMTLS {
		tlsCfg.RootCAs = mustLoadCertPool(f.cfg.Biz.TLSCA)
		tlsCfg.Certificates = []tls.Certificate{mustLoadCert(f.cfg.Biz.TLSCert, f.cfg.Biz.TLSKey)}
		tlsCfg.ServerName = "aaa-gateway"
		f.logger.Info("mTLS configured for AAA Gateway",
			"ca", f.cfg.Biz.TLSCA,
			"cert", f.cfg.Biz.TLSCert,
			"sni", "aaa-gateway",
		)
	}
	aaaClient := newHTTPAAAClient(
		f.cfg.Biz.AAAGatewayURL,
		f.cfg.Redis.Addr,
		f.podID,
		f.cfg.Version,
		&http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
			Timeout:   30 * time.Second,
		},
	)

	// ─── N58: Nnssaaf_NSSAA ─────────────────────────────────────────────
	nssaaHandler := nssaa.NewHandler(nssaaStore,
		nssaa.WithAPIRoot(apiRoot),
		nssaa.WithAAA(aaaClient),
		nssaa.WithNRFClient(nrfClient),
		nssaa.WithUDMClient(udmClient),
	)
	nssaaRouter := nssaa.NewRouter(nssaaHandler, apiRoot)

	// ─── N60: Nnssaaf_AIW ───────────────────────────────────────────────
	aiwHandler := aiw.NewHandler(aiwStore,
		aiw.WithAPIRoot(apiRoot),
		aiw.WithAUSFClient(ausfClient),
	)
	aiwRouter := aiw.NewRouter(aiwHandler, apiRoot)

	// ─── Compose router ─────────────────────────────────────────────────
	mux := http.NewServeMux()
	mux.HandleFunc("/aaa/forward", handleAaaForward)
	mux.HandleFunc("/aaa/server-initiated", handleServerInitiated)
	mux.Handle("/nnssaaf-nssaa/", nssaaRouter)
	mux.Handle("/nnssaaf-aiw/", aiwRouter)
	mux.HandleFunc("/healthz/live", handleLiveness)
	mux.HandleFunc("/healthz/ready", handleReadiness)
	mux.Handle("/metrics", metrics.Handler())

	// ─── Middleware stack ───────────────────────────────────────────────
	var handler http.Handler = mux
	handler = common.RecoveryMiddleware(handler)
	handler = common.RequestIDMiddleware(handler)
	handler = common.MetricsMiddleware(handler)
	handler = common.LoggingMiddleware(handler)
	handler = common.CORSMiddleware(handler)

	// ─── HTTP server ───────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         f.cfg.Server.Addr,
		Handler:      handler,
		ReadTimeout:  f.cfg.Server.ReadTimeout,
		WriteTimeout: f.cfg.Server.WriteTimeout,
		IdleTimeout:  f.cfg.Server.IdleTimeout,
	}

	f.logger.Info("BizPod built",
		"api_root", apiRoot,
		"server_addr", f.cfg.Server.Addr,
	)

	return &BizPod{
		Server:          srv,
		NRFClient:       nrfClient,
		SessionStore:    nssaaStore,
		AIWSessionStore: aiwStore,
		Pool:            pgPool,
		RedisPool:       redisPool,
		DLQ:             dlq,
		AAAClient:       aaaClient,
		Logger:          f.logger,
	}, cleanup, nil
}

// Close releases all resources held by BizPod.
func (bp *BizPod) Close() {
	if bp.Pool != nil {
		bp.Pool.Close()
	}
	if bp.NRFClient != nil {
		nrfCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = bp.NRFClient.Deregister(nrfCtx)
	}
	if bp.RedisPool != nil {
		_ = bp.RedisPool.Close()
	}
	if bp.AAAClient != nil {
		_ = bp.AAAClient.Close()
	}
	if bp.Server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = bp.Server.Shutdown(ctx)
	}
}

// hasScheme returns true if s already contains a URL scheme prefix.
func hasScheme(s string) bool {
	return len(s) >= 7 && (s[:7] == "http://" || s[:8] == "https://")
}

// mustLoadCertPool loads and parses a CA certificate file into an x509.CertPool.
func mustLoadCertPool(caPath string) *x509.CertPool {
	data, err := os.ReadFile(caPath)
	if err != nil {
		panic("failed to read TLS CA cert: " + err.Error())
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		panic("failed to parse TLS CA cert from: " + caPath)
	}
	return pool
}

// mustLoadCert loads a client certificate and key for mTLS.
func mustLoadCert(certPath, keyPath string) tls.Certificate {
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		panic("failed to load TLS cert/key pair: " + err.Error())
	}
	return cert
}
