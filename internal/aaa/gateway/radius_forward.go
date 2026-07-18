// Package gateway provides the AAA Gateway component for the NSSAAF 3-component architecture.
// It handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
// Spec: PHASE §2.3, §6.3; RFC 2865, RFC 3579, TS 29.561 Ch.16
package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/operator/nssAAF/internal/debug"
	"github.com/operator/nssAAF/internal/radius"
	"github.com/operator/nssAAF/internal/radiusfactory"
)

// RadiusForwarderConfig holds configuration for the RADIUS forwarder.
type RadiusForwarderConfig struct {
	ServerAddress  string
	ServerPort     int
	SharedSecret   string
	Timeout        time.Duration
	MaxRetries     int
	ResponseWindow time.Duration
}

// radiusForwarder manages a RADIUS client for the AAA Gateway.
// It handles EAP forwarding to AAA-S via RADIUS Access-Request/Accept/Reject/Challenge.
// Spec: RFC 2865, RFC 3579, TS 29.561 Ch.16
type radiusForwarder struct {
	client radius.ClientInterface
	config RadiusForwarderConfig
	logger *slog.Logger
	debug  *debug.Debug // optional; nil-safe — see internal/debug hooks
}

// Config returns the RADIUS forwarder configuration.
func (rf *radiusForwarder) Config() RadiusForwarderConfig {
	return rf.config
}

// newRadiusForwarder creates a RADIUS forwarder using the factory.
// The backend is selected by the RADIUS_BACKEND environment variable:
//   - RADIUS_BACKEND=layeh  → uses layeh.com/radius backend
//   - RADIUS_BACKEND=legacy → uses legacy custom codec (default)
// The *debug.Debug parameter is nil-safe: Emit/Wrap* short-circuit when nil or
// disabled (see internal/debug/hooks.go).
func newRadiusForwarder(cfg RadiusForwarderConfig, logger *slog.Logger, d *debug.Debug) *radiusForwarder {
	// Nil-safe logger — use default if none provided.
	if logger == nil {
		logger = slog.Default()
	}
	r := &radiusForwarder{
		config: cfg,
		logger: logger,
		debug:  d,
	}
	if cfg.ServerAddress == "" {
		return r
	}

	factoryCfg := factory.ClientConfig{
		ServerAddress: cfg.ServerAddress,
		ServerPort:    cfg.ServerPort,
		SharedSecret:  cfg.SharedSecret,
		Timeout:       cfg.Timeout,
		MaxRetries:    cfg.MaxRetries,
		Logger:        logger,
	}

	client, backend, err := factory.NewClient(factoryCfg)
	if err != nil {
		logger.Error("radius_forward: failed to create client",
			"error", err,
			"server", cfg.ServerAddress,
			"backend", backend,
		)
		return r
	}

	logger.Info("radius_forward: client initialized",
		"server", cfg.ServerAddress,
		"backend", backend,
	)

	r.client = client
	return r
}

// Forward sends a raw EAP payload to AAA-S via RADIUS Access-Request and returns the response.
// Spec: RFC 2865 §3, RFC 3579 §3.2 (EAP-Message + Message-Authenticator)
// The eapPayload is wrapped in EAP-Message attributes and sent as an Access-Request.
// User-Name is derived from the sessionID (format: "nssAAF;{nano};{authCtxID}").
func (rf *radiusForwarder) Forward(ctx context.Context, eapPayload []byte, sessionID string, sst uint8, sd string, gpsi string) ([]byte, error) {
	if rf.client == nil {
		return nil, fmt.Errorf("radius_forward: client not configured")
	}

	// Extract userName from sessionID.
	// sessionID format: "nssAAF;{unixnano};{authCtxID}"
	// Use authCtxID portion as User-Name.
	userName := sessionID
	if len(sessionID) > 0 {
		// Try to extract the last segment (authCtxID) after the last semicolon.
		if idx := -1; true {
			for i := len(sessionID) - 1; i >= 0; i-- {
				if sessionID[i] == ';' {
					idx = i
					break
				}
			}
			if idx >= 0 && idx < len(sessionID)-1 {
				userName = sessionID[idx+1:]
			}
		}
	}

	attrs := []radius.Attribute{
		radius.MakeStringAttribute(radius.AttrUserName, userName),
		radius.MakeStringAttribute(radius.AttrCallingStationID, userName),
		radius.MakeIntegerAttribute(radius.AttrServiceType, radius.ServiceTypeAuthenticateOnly),
		radius.MakeIntegerAttribute(radius.AttrNASPortType, radius.NASPortTypeVirtual),
		radius.Make3GPPSNSSAIAttribute(sst, sd),
	}

	// Fragment EAP payload into 253-byte chunks per RFC 3579.
	eapFrags := radius.FragmentEAPMessage(eapPayload, 253)
	for _, frag := range eapFrags {
		attrs = append(attrs, radius.MakeAttribute(radius.AttrEAPMessage, frag))
	}

	rf.logger.Info("radius_forward_request",
		"session_id", sessionID,
		"user_name", userName,
		"eap_len", len(eapPayload),
		"fragments", len(eapFrags),
		"gpsi", gpsi,
	)

	// Protocol-kind debug event: surfaces RADIUS Access-Request send + outcome.
	// Detail includes RADIUS code (1 = Access-Request per RFC 2865 §3.1) and
	// the resolved AAA-S peer address. The actual underlying send call is
	// wrapped with WrapProtocol in Task 14 — this Emit is the higher-level
	// "we are about to call RADIUS" signal with request metadata.
	rf.debug.Emit(ctx, debug.Event{
		Op:     "aaa.radius.forward",
		Kind:   debug.KindProtocol,
		AuthID: userName,
		GPSI:   gpsi,
		Detail: map[string]any{
			"code":       radius.CodeAccessRequest,
			"peer":       rf.config.ServerAddress,
			"eap_len":    len(eapPayload),
			"fragments":  len(eapFrags),
			"session_id": sessionID,
		},
		Status: "ok",
	})

	// WrapProtocol captures timing + outcome of the actual radius.Client.Send
	// call. Emit "radius.eap.forward" with duration_ms so an operator pulling
	// a single UE's stream sees the wire-level send vs. the higher-level
	// radius.eap.send. WrapProtocol is nil-safe (short-circuits when debug is
	// nil or disabled).
	var response []byte
	var wrapErr error
	rf.debug.WrapProtocol(ctx, "radius.eap.forward", func() error {
		var e error
		response, e = rf.client.SendAccessRequest(ctx, attrs)
		if e != nil {
			wrapErr = e
			return e
		}

		// Emit RADIUS response received event with response code.
		if len(response) > 0 {
			respCode := response[0]
			var respStatus string
			switch respCode {
			case radius.CodeAccessAccept:
				respStatus = "Access-Accept"
			case radius.CodeAccessReject:
				respStatus = "Access-Reject"
			case radius.CodeAccessChallenge:
				respStatus = "Access-Challenge"
			default:
				respStatus = fmt.Sprintf("code-%d", respCode)
			}

			rf.debug.Emit(ctx, debug.Event{
				Op:     "aaa.radius.response_received",
				Kind:   debug.KindProtocol,
				AuthID: userName,
				GPSI:   gpsi,
				Detail: map[string]any{
					"code":        respCode,
					"status":      respStatus,
					"peer":        rf.config.ServerAddress,
					"eap_len":     len(response),
					"session_id":  sessionID,
				},
				Status: respStatus,
			})
		}
		return nil
	})
	return response, wrapErr
}

// Close shuts down the RADIUS client.
func (rf *radiusForwarder) Close() error {
	if rf.client != nil {
		return rf.client.Close()
	}
	return nil
}
