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
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/fiorix/go-diameter/v4/diam/sm"
)

// AppIDAAP is the Diameter EAP Application ID (RFC 4072).
const AppIDAAP = 5

// Auth-Application-Id for NASREQ.
const authAppNASREQ = 256

// Vendor IDs (3GPP).
const vendor3GPP = 10415

// Diameter result codes.
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
	}

	machine := sm.New(settings)

	machine.HandleFunc("DER", s.handleDER)
	// DWR is handled internally by sm.New (watchdogOK wrapper ensures peer passed CER/CEA).
	// Do NOT register a handler here — would override sm's internal DWR handler.

	errc := make(chan error, 1)
	go func() {
		errc <- diam.ListenAndServeNetwork(s.network, s.addr, machine, dict.Default)
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
	a := m.Answer(diam.Success)
	a.Header.HopByHopID = m.Header.HopByHopID
	a.Header.EndToEndID = m.Header.EndToEndID

	// Result-Code.
	a.NewAVP(avp.ResultCode, avp.Mbit, 0, datatype.Unsigned32(resultCode))

	// Auth-Application-Id.
	a.NewAVP(avp.AuthApplicationID, avp.Mbit, 0, datatype.Unsigned32(authAppNASREQ))

	// Session-ID — extract from DER or generate.
	sessionID := s.extractSessionID(m)
	a.NewAVP(avp.SessionID, avp.Mbit, 0, datatype.UTF8String(sessionID))

	// Auth-Request-Type (1 = Authorize_Auth).
	a.NewAVP(avp.AuthRequestType, avp.Mbit, 0, datatype.Unsigned32(1))

	// EAP-Payload as Vendor-Specific AVP (3GPP).
	if eapPayload != nil {
		eapGroup := &diam.GroupedAVP{
			AVP: []*diam.AVP{
				diam.NewAVP(avp.VendorID, avp.Mbit, 0, datatype.Unsigned32(vendor3GPP)),
				diam.NewAVP(1265, avp.Mbit, 0, datatype.OctetString(eapPayload)),
			},
		}
		a.NewAVP(avp.VendorSpecificApplicationID, avp.Mbit, 0, eapGroup)
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
