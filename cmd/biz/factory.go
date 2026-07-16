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

	goredis "github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/operator/nssAAF/internal/amf"
	"github.com/operator/nssAAF/internal/api/aiw"
	"github.com/operator/nssAAF/internal/api/common"
	"github.com/operator/nssAAF/internal/api/nssaa"
	"github.com/operator/nssAAF/internal/ausf"
	"github.com/operator/nssAAF/internal/biz"
	"github.com/operator/nssAAF/internal/cache/redis"
	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/crypto"
	"github.com/operator/nssAAF/internal/debug"
	"github.com/operator/nssAAF/internal/discovery"
	"github.com/operator/nssAAF/internal/metrics"
	"github.com/operator/nssAAF/internal/nfclient"
	"github.com/operator/nssAAF/internal/nrf"
	"github.com/operator/nssAAF/internal/resilience"
	"github.com/operator/nssAAF/internal/storage"
	"github.com/operator/nssAAF/internal/storage/postgres"
	"github.com/operator/nssAAF/internal/tracing"
	"github.com/operator/nssAAF/internal/udm"
)

// BizPod holds all dependencies for the Biz Pod.
type BizPod struct {
	Server          *http.Server
	NRFClient       *nrf.Client
	NssaaStore      storage.NssaaStore
	AiwStore        storage.AiwStore
	Pool            *postgres.Pool
	RedisPool       *redis.Pool
	DLQ             *redis.DLQ
	AAAClient       *httpAAAClient
	Logger          *slog.Logger
	Debug           *debug.Debug
	HeartbeatCancel func() // cancels the podHeartbeat goroutine on shutdown
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

// newNFRegistry creates the circuit breaker registry from the canonical config path.
// Exposed for unit testing; callers should prefer Build() in production.
func (f *bizPodFactory) newNFRegistry() *resilience.Registry {
	cfg := f.cfg.InternalComm.Native.CB
	if cfg.FailureThreshold == 0 {
		cfg.FailureThreshold = 3
	}
	if cfg.RecoveryTimeout == 0 {
		cfg.RecoveryTimeout = 10 * time.Second
	}
	if cfg.SuccessThreshold == 0 {
		cfg.SuccessThreshold = 2
	}
	return resilience.NewRegistry(cfg.FailureThreshold, cfg.RecoveryTimeout, cfg.SuccessThreshold)
}

// rateLimiterSet holds the explicit per-scope limiter wiring.
type rateLimiterSet struct {
	amfRateLimiter  *redis.RateLimiter
	gpsiRateLimiter *redis.RateLimiter
}

func (f *bizPodFactory) newRateLimiterSet(client goredis.Cmdable, dbg *debug.Debug) rateLimiterSet {
	return rateLimiterSet{
		amfRateLimiter: redis.NewRateLimiter(
			client,
			1*time.Second,
			f.cfg.RateLimit.PerAmfPerSec,
			dbg,
		),
		gpsiRateLimiter: redis.NewRateLimiter(
			client,
			1*time.Minute,
			f.cfg.RateLimit.PerGpsiPerMin,
			dbg,
		),
	}
}

// Build creates a fully initialized BizPod with all dependencies wired.
// The caller is responsible for closing resources via Close().
func (f *bizPodFactory) Build(ctx context.Context) (*BizPod, func(), error) {
	cleanup := func() {}

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

	// ─── Per-UE debug subsystem (optional; off by default) ─────────────
	// Spec: docs/superpowers/specs/2026-07-12-nssAAF-per-ue-debug-tracing-design.md §6
	// Initialized before session stores so they can receive *debug.Debug.
	var dbg *debug.Debug
	if f.cfg.Debug.Enabled {
		dbg, err = debug.New(ctx, debug.Config{
			Enabled:   f.cfg.Debug.Enabled,
			RedisAddr: f.cfg.Debug.RedisAddr,
			Service:   "biz",
			PodID:     f.podID,
			TTL:       f.cfg.Debug.TTL,
			MaxLen:    f.cfg.Debug.MaxLen,
		})
		if err != nil {
			f.logger.Warn("debug subsystem init failed; continuing without debug", "error", err)
			dbg = nil
		}
	}
	dbgCleanup := func() {}
	if dbg != nil {
		dbgCleanup = func() { _ = dbg.Close() }
	}

	// ─── Session stores ──────────────────────────────────────────────────
	nssaaStore := postgres.NewNssaaRepository(pgPool, encryptor, dbg)
	aiwStore := postgres.NewAiwRepository(pgPool, encryptor, dbg)

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

	// ─── DLQ with dedicated HTTP client for retry delivery (DLQ-G1) ────
	dlq := redis.NewDLQ(redisPool)
	dlqHTTPClient := &http.Client{Timeout: 10 * time.Second}
	go dlq.Process(ctx, dlqHTTPClient)

	// Wire debug cleanup (dbg is initialized before session stores so they
	// can receive *debug.Debug; the actual close happens here to keep
	// cleanup ordering intact).
	if dbg != nil {
		prevCleanup := cleanup
		cleanup = func() {
			dbgCleanup()
			prevCleanup()
		}
	}

	// ─── Three isolated CB registries for blast-radius isolation ───────
	// Internal NF registry (NRF, UDM, AUSF) — wired from canonical config path (CB-G1)
	cbCfg := f.cfg.InternalComm.Native.CB
	if cbCfg.FailureThreshold == 0 {
		cbCfg.FailureThreshold = 3
	}
	if cbCfg.RecoveryTimeout == 0 {
		cbCfg.RecoveryTimeout = 10 * time.Second
	}
	if cbCfg.SuccessThreshold == 0 {
		cbCfg.SuccessThreshold = 2
	}
	internalNFRegistry := resilience.NewRegistry(cbCfg.FailureThreshold, cbCfg.RecoveryTimeout, cbCfg.SuccessThreshold)

	// AMF registry (for AMF notification delivery)
	amfCfg := config.CircuitBreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  15 * time.Second,
		SuccessThreshold: 2,
	}
	amfRegistry := resilience.NewRegistry(amfCfg.FailureThreshold, amfCfg.RecoveryTimeout, amfCfg.SuccessThreshold)

	// ─── NF clients with circuit breakers (CB-G1) ─────────────────────
	nrfFactory := nfclient.NewFactory(internalNFRegistry)
	nrfClient := nrf.NewClient(f.cfg.NRF, nrfFactory)

	// Load the NF profile YAML (if configured) before starting the heartbeat
	// loop. SetProfilePath initializes the HeartbeatManager which StartHeartbeat
	// depends on; without it StartHeartbeat returns "not initialized".
	if f.cfg.NRF.ProfilePath != "" {
		if err := nrfClient.SetProfilePath(f.cfg.NRF.ProfilePath, f.cfg.NRF.Heartbeat); err != nil {
			f.logger.Warn("failed to load NFProfile; continuing without profile-based registration",
				"path", f.cfg.NRF.ProfilePath,
				"error", err,
			)
		}
	}

	// StartHeartbeat performs the initial PUT registration synchronously and
	// then runs the PATCH heartbeat loop, so a separate RegisterAsync would
	// issue a duplicate PUT. Uses Background context because the heartbeat
	// manager manages its own cancellation via stopCh and deregisters on shutdown.
	// Failures are non-fatal: StartHeartbeat returns nil when the manager handles
	// background retries on its own.
	if err := nrfClient.StartHeartbeat(context.Background()); err != nil {
		f.logger.Warn("nrf heartbeat start failed; NRF registration will retry in background",
			"error", err,
		)
	}

	// Phase 3: Biz Pod discovers UDM via HTTP Gateway's internal discovery API
	// instead of direct NRF calls.
	// Spec: docs/superpowers/plans/2026-07-17-nssAAF-nrf-migration-spec.md §Phase 3
	httpGatewayDiscoveryURL := f.cfg.HTTPgw.DiscoveryURL
	if httpGatewayDiscoveryURL == "" {
		httpGatewayDiscoveryURL = "http://172.0.3.14:8443" // Default HTTP Gateway URL
	}
	discClient := discovery.NewClient(httpGatewayDiscoveryURL)

	udmClient := udm.NewClient(f.cfg.UDM, nrfFactory, discClient)
	ausfClient := ausf.NewClient(f.cfg.AUSF, nrfFactory)

	// ─── AMF notifier with circuit breaker (CB-G3) ────────────────────
	amfFactory := nfclient.NewFactory(amfRegistry)
	amfNotifier := amf.NewClient(
		amfFactory,
		amfRegistry,
		dlq,
		amfCfg,
		resilience.RetryConfig{
			MaxAttempts: 3,
			BaseDelay:   500 * time.Millisecond,
			MaxDelay:    2 * time.Second,
		},
	)

	// ─── Rate limiters (RL-G1) ───────────────────────────────────────────
	rateLimiters := f.newRateLimiterSet(redisPool.Client(), dbg)

	// ─── HTTP AAA client ────────────────────────────────────────────────
	if f.cfg.Biz.UseMTLS {
		f.logger.Info("mTLS configured for AAA Gateway",
			"ca", f.cfg.Biz.TLSCA,
			"cert", f.cfg.Biz.TLSCert,
			"sni", "aaa-gateway",
		)
	}
	// Populate TLS config from BizConfig for NativeCommConfig
	commCfg := f.cfg.InternalComm
	if commCfg.Native.TLS == nil {
		commCfg.Native.TLS = &config.TLSClientConfig{}
	}
	if f.cfg.Biz.UseMTLS {
		commCfg.Native.TLS = &config.TLSClientConfig{
			CACert:     f.cfg.Biz.TLSCA,
			ClientCert: f.cfg.Biz.TLSCert,
			ClientKey:  f.cfg.Biz.TLSKey,
			ServerName: "aaa-gateway",
		}
	}
	aaaClient := newHTTPAAAClient(
		f.cfg.Biz.AAAGatewayURL,
		f.podID,
		f.cfg.Version,
		commCfg,
		f.logger,
		dbg,
	)

	// Start VIP health check goroutine after pod initialization.
	// This resets the circuit breaker when the AAA Gateway VIP fails over.
	go aaaClient.StartVIPHealthCheck(ctx)

	// ─── Reverse-path coordinator for AAA server-initiated flows ─────────
	resolver := biz.NewCorrelationResolver(redisPool.Client(), biz.NewNssaaSessionResolver(nssaaStore))
	stateWriter := biz.NewReverseFlowStateWriter(nssaaStore)
	aiwLinker := biz.NewAIWCompletionLinker(aiwStore)
	coordinator := biz.NewServerInitiatedCoordinator(resolver, stateWriter, amfNotifier, aiwLinker)
	serverInitiatedHandler = NewServerInitiatedHandler(coordinator).HandleReAuth

	// ─── N58: Nnssaaf_NSSAA ─────────────────────────────────────────────
	nssaaHandler := nssaa.NewHandler(nssaaStore,
		nssaa.WithAPIRoot(apiRoot),
		nssaa.WithAAA(aaaClient),
		nssaa.WithNRFClient(nrfClient),
		nssaa.WithUDMClient(udmClient),
		nssaa.WithAMFRateLimiter(rateLimiters.amfRateLimiter), // PerAmfPerSec, 1-second window (RL-POLICY-AMF)
		nssaa.WithRateLimiter(rateLimiters.gpsiRateLimiter),   // PerGpsiPerMin, 1-minute window (RL-POLICY-AUTHCTX)
	)
	nssaaRouter := nssaa.NewRouter(nssaaHandler, apiRoot)

	// ─── N60: Nnssaaf_AIW ───────────────────────────────────────────────
	aiwHandler := aiw.NewHandler(aiwStore,
		aiw.WithAPIRoot(apiRoot),
		aiw.WithAUSFClient(ausfClient),
		aiw.WithRateLimiter(rateLimiters.gpsiRateLimiter), // PerGpsiPerMin, 1-minute window (RL-POLICY-GPSI)
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

	// DebugMiddleware must be INSIDE otelhttp.NewHandler so it can access
	// the span created by otelhttp for emitting http.request events.
	// This matches the http-gw pattern where DebugMiddleware wraps the mux
	// before otelhttp.NewHandler is applied.
	inner := common.DebugMiddleware(dbg)(handler)
	handler = otelhttp.NewHandler(inner, "biz")

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
		Server:     srv,
		NRFClient:  nrfClient,
		NssaaStore: nssaaStore,
		AiwStore:   aiwStore,
		Pool:       pgPool,
		RedisPool:  redisPool,
		DLQ:        dlq,
		AAAClient:  aaaClient,
		Logger:     f.logger,
		Debug:      dbg,
	}, cleanup, nil
}

// Close releases all resources held by BizPod.
func (bp *BizPod) Close() {
	if bp.HeartbeatCancel != nil {
		bp.HeartbeatCancel()
	}
	if bp.Pool != nil {
		bp.Pool.Close()
	}
	if bp.NRFClient != nil {
		nrfCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = bp.NRFClient.Deregister(nrfCtx, bp.NRFClient.NFInstanceID())
	}
	if bp.RedisPool != nil {
		_ = bp.RedisPool.Close()
	}
	if bp.AAAClient != nil {
		_ = bp.AAAClient.Close()
	}
	if bp.Debug != nil {
		_ = bp.Debug.Close()
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
