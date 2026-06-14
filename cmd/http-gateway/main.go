// Package main is the entry point for the NSSAAF HTTP Gateway.
// Spec: TS 29.526 v18.7.0
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/operator/nssAAF/internal/auth"
	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/httpclient"
)

var configPath = flag.String("config", "configs/http-gateway.yaml", "path to YAML configuration file")

func main() {
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if cfg.Component != config.ComponentHTTPGateway {
		slog.Error("config.component must be 'http-gateway'", "got", cfg.Component)
		os.Exit(1)
	}

	slog.Info("starting NSSAAF HTTP Gateway",
		"version", cfg.Version,
		"tls_enabled", cfg.HTTPgw.TLS != nil && cfg.HTTPgw.TLS.Cert != "",
		"tls_version", "1.3",
		"istio_mtls", os.Getenv("ISTIO_MTLS") == "1",
	)

	// REQ-22: Initialize JWT validator with NRF JWKS URL.
	// Falls back to default if nrf: section absent from http-gateway.yaml.
	nrfBaseURL := cfg.NRF.BaseURL
	if nrfBaseURL == "" {
		nrfBaseURL = "https://nrf.operator.com"
	}
	jwksURL := nrfBaseURL + "/.well-known/jwks.json"
	if err := auth.Init(auth.TokenValidatorConfig{
		JWKSURL:        jwksURL,
		Issuer:         nrfBaseURL,
		Audiences:      []string{"nnssaaf-nssaa", "nnssaaf-aiw"},
		AllowedNfTypes: []string{"AMF", "AUSF"},
		AllowedScopes:  []string{"nnssaaf-nssaa", "nnssaaf-aiw"},
	}); err != nil {
		// Use a local logger so the error is logged regardless of slog.SetDefault
		// ordering — avoids silent failure if SetDefault is moved in a future refactor.
		tmpLog := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		tmpLog.Error("auth.Init failed", "error", err)
		os.Exit(1)
	}
	slog.Info("auth initialized", "jwks_url", jwksURL)

	bizClient := httpclient.NewFactory(cfg.InternalComm).NewBizServiceClient(cfg.HTTPgw.BizServiceURL)

	// Use a mux for path-based auth scoping.
	mux := http.NewServeMux()

	var authCfg auth.Config
	if cfg.HTTPgw.Auth != nil {
		authCfg.Disabled = cfg.HTTPgw.Auth.Disabled
	}

	// N58: Nnssaaf_NSSAA — requires nnssaaf-nssaa scope
	mux.Handle("/nnssaaf-nssaa/", auth.NewAuthMiddleware(authCfg)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body []byte
			if r.Body != nil {
				body, _ = io.ReadAll(r.Body)
			}

			requestID := r.Header.Get("X-Request-ID")
			respBody, status, err := bizClient.ForwardRequest(r.Context(), r.URL.Path, r.Method, body, requestID)
			if err != nil {
				slog.Error("forward to biz failed", "error", err, "path", r.URL.Path)
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
				return
			}

			w.WriteHeader(status)
			if len(respBody) > 0 {
				_, _ = w.Write(respBody)
			}
		}),
	))

	// N60: Nnssaaf_AIW — requires nnssaaf-aiw scope
	mux.Handle("/nnssaaf-aiw/", auth.NewAuthMiddleware(authCfg)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body []byte
			if r.Body != nil {
				body, _ = io.ReadAll(r.Body)
			}

			requestID := r.Header.Get("X-Request-ID")
			respBody, status, err := bizClient.ForwardRequest(r.Context(), r.URL.Path, r.Method, body, requestID)
			if err != nil {
				slog.Error("forward to biz failed", "error", err, "path", r.URL.Path)
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
				return
			}

			w.WriteHeader(status)
			if len(respBody) > 0 {
				_, _ = w.Write(respBody)
			}
		}),
	))

	// Health endpoints — no auth required
	mux.HandleFunc("/healthz/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	handler := mux

	// TODO(phase-6): Log TLS cipher suite on each connection for audit.
	// AuditEntry.TLSCipher field per docs/design/15_sbi_security.md §8.
	// This requires a TLS getter hook (tls.Config.GetConfigForConnection) or
	// connection-level logging via http.ConnState.

	// Build TLS 1.3 config for HTTP Gateway server.
	// REQ-20: TLS 1.3 mandatory per RFC 8446 / TS 29.500 §5.
	// Cipher suites per docs/design/15_sbi_security.md §2.1.
	var tlsConfig *tls.Config
	if os.Getenv("ISTIO_MTLS") == "1" {
		slog.Info("istio mTLS mode enabled — skipping explicit TLS config")
		tlsConfig = nil // Istio sidecar handles TLS termination
	} else if cfg.HTTPgw.TLS != nil && cfg.HTTPgw.TLS.Cert != "" && cfg.HTTPgw.TLS.Key != "" {
		tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS13,
			MaxVersion: tls.VersionTLS13, // Enforce ceiling — prevents TLS 1.2 fallback
			CurvePreferences: []tls.CurveID{
				tls.X25519,
				tls.CurveP384,
				tls.CurveP256,
			},
			// Cipher suites per docs/design/15_sbi_security.md §2.1 (TLS 1.3 suites only)
			CipherSuites: []uint16{
				tls.TLS_AES_256_GCM_SHA384,
				tls.TLS_AES_128_GCM_SHA256,
				tls.TLS_CHACHA20_POLY1305_SHA256,
			},
			PreferServerCipherSuites: true,
		}
	} else {
		slog.Warn("no TLS configured for HTTP Gateway — running in HTTP mode")
		tlsConfig = nil
	}

	var srv *http.Server
	if tlsConfig != nil {
		srv = &http.Server{
			Addr:      cfg.Server.Addr,
			Handler:   handler,
			TLSConfig: tlsConfig,
		}
		go func() {
			slog.Info("http-gateway HTTPS listening (TLS 1.3)", "addr", srv.Addr)
			if err := srv.ListenAndServeTLS(cfg.HTTPgw.TLS.Cert, cfg.HTTPgw.TLS.Key); err != nil && err != http.ErrServerClosed {
				slog.Error("https server error", "error", err)
				os.Exit(1)
			}
		}()
	} else {
		srv = &http.Server{
			Addr:    cfg.Server.Addr,
			Handler: handler,
		}
		go func() {
			slog.Info("http-gateway HTTP listening (no TLS)", "addr", srv.Addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("http server error", "error", err)
				os.Exit(1)
			}
		}()
	}

	<-signalReceived()
	slog.Info("shutting down HTTP Gateway")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

func signalReceived() <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		close(ch)
	}()
	return ch
}
