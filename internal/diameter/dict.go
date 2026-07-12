// Package diameter: dict.go provides the shared Diameter EAP-extended dictionary.
//
// go-diameter's default dict only knows apps 0 (Base) and 3 (Base Accounting).
// For Diameter EAP (AppID=5, RFC 4072) — which NSSAAF requires — callers must
// use Dict() instead of dict.Default everywhere they construct a sm.Client or
// sm.Settings. Using dict.Default causes CER with Auth-Application-Id=5 to fail
// with "Client attempts to advertise unsupported application".
//
// Dict() returns a *dict.Parser that is the default parser augmented with the
// minimum EAP application and command codes. The parser is loaded once and
// cached at package init.
package diameter

import (
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// eapDictionaryXML is the minimum dictionary extension that registers the
// Diameter EAP application (AppID=5, RFC 4072) and its DER/DEA command codes.
//
// IMPORTANT: go-diameter's dict parser indexes commands by (appID, code) —
// declaring two <command code="268"> entries (one with short="DER" and one
// with short="DEA") makes Load() return "Command: ... cannot be added:
// index exists" and the application is rejected. The convention (used by
// the go-diameter default dictionary) is a single entry whose .Short is the
// common base, and go-diameter's mux dispatcher appends "R" / "A" to derive
// the lookup key (server.go: cmd = dcmd.Short + "R"|"A"). With short="DE"
// the request becomes "DER" and the answer becomes "DEA" — matching the
// HandleFunc("DER", ...) registrations across the codebase.
const eapDictionaryXML = `<?xml version="1.0" encoding="UTF-8"?>
<diameter>
	<application id="5" type="auth" name="Diameter EAP">
		<command code="268" short="DE" name="Diameter-EAP-Request">
			<request>
				<rule avp="Session-Id" required="false" max="1"/>
				<rule avp="Auth-Application-Id" required="true" max="1"/>
				<rule avp="Auth-Request-Type" required="true" max="1"/>
				<rule avp="Destination-Host" required="false" max="1"/>
				<rule avp="Destination-Realm" required="true" max="1"/>
				<rule avp="Origin-Host" required="true" max="1"/>
				<rule avp="Origin-Realm" required="true" max="1"/>
				<rule avp="User-Name" required="false" max="1"/>
				<rule avp="EAP-Payload" required="false"/>
				<rule avp="EAP-Master-Session-Key" required="false"/>
			</request>
			<answer>
				<rule avp="Result-Code" required="true" max="1"/>
				<rule avp="Session-Id" required="true" max="1"/>
				<rule avp="Auth-Application-Id" required="true" max="1"/>
				<rule avp="Auth-Request-Type" required="true" max="1"/>
				<rule avp="User-Name" required="false" max="1"/>
				<rule avp="EAP-Payload" required="false"/>
			</answer>
		</command>
	</application>
</diameter>
`

var (
	dictOnce sync.Once
	dictExt  *dict.Parser
)

// Dict returns the default go-diameter dictionary extended with the Diameter
// EAP application (AppID=5). All NSSAAF code that constructs Diameter clients
// or state machines must use this — never dict.Default — because RFC 4072
// (Diameter EAP) is not in the base dictionary.
func Dict() *dict.Parser {
	dictOnce.Do(func() {
		dictExt = dict.Default
		if err := dictExt.Load(strings.NewReader(eapDictionaryXML)); err != nil {
			// Loud failure: previously silent — masked every DER/DEA round-trip
			// in tests because FindCommand(5, 268) returned an error and the
			// dispatch fell through to an "ALL" catch-all (or none at all),
			// silently dropping DER messages on the server side.
			slog.New(slog.NewJSONHandler(os.Stderr, nil)).
				Error("diameter.Dict: failed to load EAP dictionary extension; EAP support is broken",
					"error", err)
			dictExt = dict.Default
		}
	})
	return dictExt
}
