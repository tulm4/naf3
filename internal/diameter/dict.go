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
	"strings"
	"sync"

	"github.com/fiorix/go-diameter/v4/diam/dict"
)

// eapDictionaryXML is the minimum dictionary extension that registers the
// Diameter EAP application (AppID=5, RFC 4072) and its DER/DEA command codes.
const eapDictionaryXML = `<?xml version="1.0" encoding="UTF-8"?>
<diameter>
	<application id="5" type="auth" name="Diameter EAP">
		<command code="268" short="DER" name="Diameter-EAP-Request"/>
		<command code="268" short="DEA" name="Diameter-EAP-Answer"/>
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
			// On error, fall back to the default parser; sm.PrepareSupportedApps
			// will not include app 5, which means clients sending CER with
			// Auth-Application-Id=5 will be rejected. We log nothing here
			// because this package may be imported by tests that don't want
			// to set up a logger; the failure is loud enough at runtime.
			dictExt = dict.Default
		}
	})
	return dictExt
}
