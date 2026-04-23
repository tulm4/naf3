// Package gateway provides the AAA Gateway component for the NSSAAF 3-component architecture.
// It handles both client-initiated (Biz Pod → AAA-S) and server-initiated (AAA-S → Biz Pod) flows.
// Spec: PHASE §2.3, §6.3; RFC 2865, RFC 3579, TS 29.561 Ch.16
package gateway

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/operator/nssAAF/internal/radius"
)

// radiusForwarder manages a RADIUS client for the AAA Gateway.
// It handles EAP forwarding to AAA-S via RADIUS Access-Request/Accept/Reject/Challen

// Spec: RFC 2865, RFC 3579, TS 29.561 Ch.16
type radiusForwarder struct {
	client *radius.Client
	logger *slog.Logger
}

// newRadiusForwarder creates a RADIUS forwarder using the existing radius.Client.
func newRadiusForwarder(serverAddr string, serverPort int, sharedSecret string, logger *slog.Logger) *radiusForwarder {
	r := &radiusForwarder{
		logger: logger,
	}
	if serverAddr == "" {
		// No RADIUS config — forwarder disabled
		return r
	}

	cfg := radius.Config{
		ServerAddress:  serverAddr,
		ServerPort:    serverPort,
		SharedSecret:   sharedSecret,
		Timeout:       10,
		MaxRetries:    3,
		ResponseWindow: 10,
		Transport:     "UDP",
	}

	client, err := radius.NewRadiusClient(cfg, logger)
	if err != nil {
		logger.Error("radius_forward: failed to create client", "error", err, "server", serverAddr)
		return r
	}
	r.client = client
	return r
}

// Forward sends a raw EAP payload to AAA-S via RADIUS Access-Request and returns the response.
// Spec: RFC 2865 §3, RFC 3579 §3.2 (EAP-Message + Message-Authenticator)
// The eapPayload is wrapped in EAP-Message attributes and sent as an Access-Request.
// User-Name is derived from the sessionID (format: "nssAAF;{nano};{authCtxID}").
func (rf *radiusForwarder) Forward(ctx context.Context, eapPayload []byte, sessionID string, sst uint8, sd string) ([]byte, error) {
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

	rf.logger.Debug("radius_forward_request",
		"session_id", sessionID,
		"user_name", userName,
		"eap_len", len(eapPayload),
		"fragments", len(eapFrags),
	)

	return rf.client.SendAccessRequest(ctx, attrs)
}

// Close shuts down the RADIUS client.
func (rf *radiusForwarder) Close() error {
	if rf.client != nil {
		return rf.client.Close()
	}
	return nil
}
