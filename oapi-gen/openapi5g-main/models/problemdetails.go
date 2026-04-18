// Copyright 2023-2024 APRESIA Systems LTD.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package models

import (
	"fmt"
	"io"
	"strings"
)

func (p ProblemDetails) String() string {
	var b strings.Builder
	p.formatPrint(&b, "%v")
	return b.String()
}

func (p ProblemDetails) formatPrint(w io.Writer, fmtV string) {
	fmt.Fprintf(w, "{Status:%d", p.Status)

	if p.Title != nil {
		fmt.Fprintf(w, " Title:%s", *p.Title)
	}

	if p.Cause != nil {
		fmt.Fprintf(w, " Cause:%s", *p.Cause)
	}

	if p.Detail != nil {
		fmt.Fprintf(w, " Detail:%s", *p.Detail)
	}

	if p.AccessTokenError != nil {
		fmt.Fprintf(w, " AccessTokenError:"+fmtV, p.AccessTokenError)
	}

	if p.AccessTokenRequest != nil {
		fmt.Fprintf(w, " AccessTokenRequest:"+fmtV, p.AccessTokenRequest)
	}

	if p.Instance != nil {
		fmt.Fprintf(w, " Instance:%s", *p.Instance)
	}

	if len(p.InvalidParams) != 0 {
		fmt.Fprintf(w, " InvalidParams:"+fmtV, p.InvalidParams)
	}

	if p.NrfId != nil {
		fmt.Fprintf(w, " NrfId:%s", *p.NrfId)
	}

	if p.SupportedFeatures != nil {
		fmt.Fprintf(w, " SupportedFeatures:%s", *p.SupportedFeatures)
	}

	if p.Type != nil {
		fmt.Fprintf(w, " Type:%s", *p.Type)
	}

	if len(p.AdditionalProperties) != 0 {
		fmt.Fprintf(w, " AdditionalProperties:"+fmtV, p.AdditionalProperties)
	}

	fmt.Fprint(w, "}")
}

func (p ProblemDetails) GoString() string {
	type tmpType ProblemDetails
	return strings.Replace(fmt.Sprintf("%#v", tmpType(p)), "models.tmpType", "models.ProblemDetails", 1)
}

func (p ProblemDetails) Format(state fmt.State, verb rune) {
	switch verb {
	case 'v':
		if state.Flag('#') {
			fmt.Fprint(state, p.GoString())
		} else if state.Flag('+') {
			p.formatPrint(state, "%+v")
		} else {
			p.formatPrint(state, "%v")
		}
	case 's':
		fmt.Fprint(state, p.String())
	default:
		fmt.Fprintf(state, "%%!%c(%T=%v)", verb, p, p)
	}
}
