// Package diameter: dict.go provides the shared Diameter EAP-extended dictionary.
//
// go-diameter's default dict only knows apps 0 (Base) and 3 (Base Accounting).
// For Diameter EAP (AppID=5, RFC 4072) — which NSSAAF requires — callers must
// use Dict() instead of dict.Default everywhere they construct a sm.Client or
// sm.Settings. Using dict.Default causes CER with Auth-Application-Id=5 to fail
// with "Client attempts to advertise unsupported application".
//
// Dict() returns a *dict.Parser that is the default parser augmented with the
// full NSSAA dictionary extension (via generated.Parser()). The parser is loaded
// once and cached at package init.
package diameter

import (
	"github.com/fiorix/go-diameter/v4/diam/dict"
	"github.com/operator/nssAAF/internal/diameter/generated"
)

// Dict returns the go-diameter dictionary extended with the NSSAA extension.
// All NSSAAF code that constructs Diameter clients or state machines must use
// this — never dict.Default — because RFC 4072 (Diameter EAP) is not in the
// base dictionary.
//
// This function delegates to generated.Parser() which loads the full NSSAA
// dictionary including all 3GPP-specific AVPs (codes 200, 3100-3109).
func Dict() *dict.Parser {
	return generated.Parser()
}
