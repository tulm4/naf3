// Package main is the entry point for the NSSAAF HTTP Gateway.
// Spec: TS 29.526 v18.7.0
package main

import (
	"bytes"
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

	"github.com/operator/nssAAF/internal/config"
	"github.com/operator/nssAAF/internal/proto"
)

var configPath = flag.String("config", "configs/http-gateway.yaml", "path to YAML configuration file")

// httpBizClient satisfies proto.BizServiceClient.
type httpBizClient struct {
	bizServiceURL string
	httpClient    *http.Client
	version       string
}

// ForwardRequest satisfies proto.BizServiceClient.
func (c *httpBizClient) ForwardRequest(ctx context.Context, path, method string, body []byte) ([]byte, int, error) {
	url := c.bizServiceURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(proto.HeaderName, c.version)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, 503, err
		}
		return nil, 502, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

var _ proto.BizServiceClient = (*httpBizClient)(nil)

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

	bizClient := &httpBizClient{
		bizServiceURL: cfg.HTTPgw.BizServiceURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		version: cfg.Version,
	}

	// Forward all requests to Biz Pods
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
		}

		respBody, status, err := bizClient.ForwardRequest(r.Context(), r.URL.Path, r.Method, body)
		if err != nil {
			slog.Error("forward to biz failed", "error", err, "path", r.URL.Path)
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(status)
		if len(respBody) > 0 {
			w.Write(respBody)
		}
	})

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
			CurvePreferences: []tls.CurveID{
				tls.X25519,
				tls.CurveP384,
				tls.CurveP256,
			},
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
	srv.Shutdown(ctx)
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
