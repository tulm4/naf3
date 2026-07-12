// Package aaa_sim provides a standalone AAA-S simulator for E2E testing.
package aaa_sim

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/fiorix/go-diameter/v4/diam"
	"github.com/fiorix/go-diameter/v4/diam/avp"
	"github.com/fiorix/go-diameter/v4/diam/datatype"
	"github.com/fiorix/go-diameter/v4/diam/sm"

	"github.com/operator/nssAAF/internal/diameter"
)

// AppIDAAP is the Diameter EAP Application ID (RFC 4072).
const AppIDAAP = 5

// Vendor IDs (3GPP).
const vendor3GPP = 10415

// Diameter result codes. Currently unused — handled inline in handleDER
// via diam.Success / diam.Answer(). Kept for future modes that need to
// override the result code (e.g. partial-auth challenges).
const (
	diameterSuccess      = 2001
	diameterAuthRejected = 4003
	diameterChallenge    = 4002
)

// DiameterServer handles Diameter EAP requests using go-diameter/v4/sm
// for RFC 6733-compliant CER/CEA handshake and DWR/DWA watchdog.
type DiameterServer struct {
	network string
	addr   string
	mode   Mode
	logger *slog.Logger
}

// NewDiameterServer creates a Diameter server.
func NewDiameterServer(network, addr string, mode Mode, logger *slog.Logger) *DiameterServer {
	return &DiameterServer{
		network: network,
		addr:   addr,
		mode:   mode,
		logger: logger,
	}
}

// Run starts the Diameter server. It uses go-diameter/v4/sm for CER/CEA
// handshake and DWR/DWA watchdog handling. DER/DEA EAP response logic
// stays in manual code within this package.
func (s *DiameterServer) Run(ctx context.Context) error {
	settings := &sm.Settings{
		OriginHost:  datatype.DiameterIdentity("aaa-sim"),
		OriginRealm: datatype.DiameterIdentity("test.local"),
		VendorID:    datatype.Unsigned32(vendor3GPP),
		ProductName: "AAA-Simulator",
		Dict:        diameter.Dict(),
	}

	machine := sm.New(settings)

	machine.HandleFunc("ALL", func(c diam.Conn, m *diam.Message) {
		s.logger.Warn("unhandled diameter message",
			"cmd_code", m.Header.CommandCode,
			"app_id", m.Header.ApplicationID,
			"is_request", m.Header.CommandFlags&diam.RequestFlag == diam.RequestFlag,
		)
	})
	// Register DER at the EAP app index (AppID=5, Code=268). With the EAP
	// dict entry short="DE" (see internal/diameter/dict.go), go-diameter's
	// ServeMux will derive cmd="DER" for requests and dispatch here.
	machine.HandleIdx(diam.CommandIndex{AppID: AppIDAAP, Code: 268, Request: true}, diam.HandlerFunc(s.handleDER))
	// DWR is handled internally by sm.New (watchdogOK wrapper ensures peer passed CER/CEA).
	// Do NOT register a handler here — would override sm's internal DWR handler.

	errc := make(chan error, 1)
	go func() {
		errc <- diam.ListenAndServeNetwork(s.network, s.addr, machine, diameter.Dict())
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errc:
		return err
	}
}

// handleDER handles Diameter-EAP-Request messages.
// It runs only after the peer has passed CER/CEA (enforced by sm's handshakeOK wrapper).
func (s *DiameterServer) handleDER(c diam.Conn, m *diam.Message) {
	var resultCode uint32
	var eapPayload []byte

	switch s.mode {
	case ModeEAP_TLS_SUCCESS:
		resultCode = diameterSuccess
		eapPayload = []byte{3, 0, 0, 4} // EAP Success
	case ModeEAP_TLS_FAILURE:
		resultCode = diameterSuccess
		eapPayload = []byte{4, 0, 0, 4} // EAP Failure
	case ModeEAP_TLS_CHALLENGE:
		resultCode = diameterChallenge
		eapPayload = []byte{1, 13, 0, 6, 0, 0, 0, 0} // EAP Request (TLS data)
	}

	// Build DEA response using go-diameter's message builder.
	// Note: m.Answer(diam.Success) already inserts a Result-Code AVP (2001).
	a := m.Answer(diam.Success)
	a.Header.HopByHopID = m.Header.HopByHopID
	a.Header.EndToEndID = m.Header.EndToEndID

	// Override Result-Code only when the mode requires a non-Success code
	// (e.g. EAP_TLS_CHALLENGE → 4002). Inserting a second Result-Code AVP
	// when the value already matches diam.Success is invalid per RFC 6733
	// and triggers the peer's parser to drop the message.
	if resultCode != diam.Success {
		a.NewAVP(avp.ResultCode, avp.Mbit, 0, datatype.Unsigned32(resultCode))
	}

	// Auth-Application-Id — must equal the client's AppID (Diameter EAP = 5)
	// so that go-diameter's client-side parser matches the response to its
	// pending request.
	a.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(AppIDAAP))

	// Session-ID — extract from DER or generate.
	sessionID := s.extractSessionID(m)
	a.NewAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String(sessionID))

	// Auth-Request-Type (1 = Authorize_Auth).
	a.NewAVP(avp.AuthRequestType, avp.Mbit, 0, datatype.Unsigned32(1))

	// EAP-Payload AVP (code 209, RFC 4072 §4.2) — same AVP code the
	// aaa-gateway DER builder uses (see internal/diameter.AVPCodeEAPPayload),
	// so the DEA round-trips through the same dictionary entry.
	if eapPayload != nil {
		a.NewAVP(209, avp.Mbit, 0, datatype.OctetString(eapPayload))
	}

	if _, err := a.WriteTo(c); err != nil {
		s.logger.Error("failed to write DEA", "error", err)
	}
}

// extractSessionID extracts Session-ID AVP from a DER message.
// Returns a generated ID if not found.
func (s *DiameterServer) extractSessionID(m *diam.Message) string {
	avp, err := m.FindAVP(avp.SessionID, 0)
	if err == nil && avp != nil {
		if sid, ok := avp.Data.(datatype.UTF8String); ok {
			return string(sid)
		}
	}
	return fmt.Sprintf("diameter-session-%d", time.Now().UnixNano())
}
