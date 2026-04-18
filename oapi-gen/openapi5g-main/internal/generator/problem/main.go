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

package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"text/template"

	"github.com/ShouheiNishi/openapi5g/internal/generator/writer"
)

type errorInfo struct {
	GolangName       string
	Cause            string
	Title            string
	StatusCode       string
	HasInvalidParams bool
}

type errorEntry struct {
	Cause            string
	StatusCode       int
	HasInvalidParams bool
}

var errorList []errorEntry = []errorEntry{
	{Cause: "INVALID_API", StatusCode: http.StatusBadRequest, HasInvalidParams: false},
	{Cause: "INVALID_MSG_FORMAT", StatusCode: http.StatusBadRequest, HasInvalidParams: false},
	{Cause: "INVALID_QUERY_PARAM", StatusCode: http.StatusBadRequest, HasInvalidParams: true},
	{Cause: "MANDATORY_QUERY_PARAM_INCORRECT", StatusCode: http.StatusBadRequest, HasInvalidParams: true},
	{Cause: "OPTIONAL_QUERY_PARAM_INCORRECT", StatusCode: http.StatusBadRequest, HasInvalidParams: true},
	{Cause: "MANDATORY_QUERY_PARAM_MISSING", StatusCode: http.StatusBadRequest, HasInvalidParams: true},
	{Cause: "MANDATORY_IE_INCORRECT", StatusCode: http.StatusBadRequest, HasInvalidParams: true},
	{Cause: "OPTIONAL_IE_INCORRECT", StatusCode: http.StatusBadRequest, HasInvalidParams: true},
	{Cause: "MANDATORY_IE_MISSING", StatusCode: http.StatusBadRequest, HasInvalidParams: true},
	{Cause: "UNSPECIFIED_MSG_FAILURE", StatusCode: http.StatusBadRequest, HasInvalidParams: false},
	{Cause: "NF_DISCOVERY_FAILURE", StatusCode: http.StatusBadRequest, HasInvalidParams: false},
	{Cause: "INVALID_DISCOVERY_PARAM", StatusCode: http.StatusBadRequest, HasInvalidParams: false},
	{Cause: "RESOURCE_CONTEXT_NOT_FOUND", StatusCode: http.StatusBadRequest, HasInvalidParams: false},
	{Cause: "MISSING_ACCESS_TOKEN_INFO", StatusCode: http.StatusBadRequest, HasInvalidParams: false},
	{Cause: "CCA_VERIFICATION_FAILURE", StatusCode: http.StatusForbidden, HasInvalidParams: false},
	{Cause: "TOKEN_CCA_MISMATCH", StatusCode: http.StatusForbidden, HasInvalidParams: false},
	{Cause: "MODIFICATION_NOT_ALLOWED", StatusCode: http.StatusForbidden, HasInvalidParams: false},
	{Cause: "ACCESS_TOKEN_DENIED", StatusCode: http.StatusForbidden, HasInvalidParams: false},
	{Cause: "SUBSCRIPTION_NOT_FOUND", StatusCode: http.StatusNotFound, HasInvalidParams: false},
	{Cause: "RESOURCE_URI_STRUCTURE_NOT_FOUND", StatusCode: http.StatusNotFound, HasInvalidParams: false},
	{Cause: "INCORRECT_LENGTH", StatusCode: http.StatusLengthRequired, HasInvalidParams: false},
	{Cause: "NF_CONGESTION_RISK", StatusCode: http.StatusTooManyRequests, HasInvalidParams: false},
	{Cause: "INSUFFICIENT_RESOURCES", StatusCode: http.StatusInternalServerError, HasInvalidParams: false},
	{Cause: "UNSPECIFIED_NF_FAILURE", StatusCode: http.StatusInternalServerError, HasInvalidParams: false},
	{Cause: "SYSTEM_FAILURE", StatusCode: http.StatusInternalServerError, HasInvalidParams: false},
	{Cause: "NF_FAILOVER", StatusCode: http.StatusInternalServerError, HasInvalidParams: false},
	{Cause: "NF_SERVICE_FAILOVER", StatusCode: http.StatusInternalServerError, HasInvalidParams: false},
	{Cause: "NF_CONGESTION", StatusCode: http.StatusServiceUnavailable, HasInvalidParams: false},
	{Cause: "TARGET_NF_NOT_REACHABLE", StatusCode: http.StatusGatewayTimeout, HasInvalidParams: false},
	{Cause: "TIMED_OUT_REQUEST", StatusCode: http.StatusGatewayTimeout, HasInvalidParams: false},
}

const tmpl = `

const (
{{range .}}
    Cause{{.GolangName}} = "{{.Cause}}"{{end}}
)

{{range .}}
func {{.GolangName}}({{if .HasInvalidParams}}invalidParams []models.InvalidParam,{{end}} detail string) models.ProblemDetails {
	pd := models.ProblemDetails{
		Status: {{.StatusCode}},
		Cause:  lo.ToPtr(Cause{{.GolangName}}),
		Title:  lo.ToPtr("{{.Title}}"),{{if .HasInvalidParams}}
		InvalidParams: invalidParams,{{end}}
	}
	if detail != "" {
		pd.Detail = &detail
	}
	return pd
}

{{end}}
`

func main() {
	out := writer.NewOutputFile(
		"messages.gen.go",
		"problem",
		"github.com/ShouheiNishi/openapi5g/internal/generator/problem",
		writer.ImportSpecs{
			{ImportPath: "net/http"},
			{},
			{ImportPath: "github.com/ShouheiNishi/openapi5g/models"},
			{ImportPath: "github.com/samber/lo"},
		},
	)
	defer func() {
		if err := out.Close(); err != nil {
			panic(err)
		}
	}()

	var errorInfos []errorInfo

	for _, entry := range errorList {
		i := errorInfo{
			Cause:            entry.Cause,
			HasInvalidParams: entry.HasInvalidParams,
		}

		switch entry.StatusCode {
		case http.StatusBadRequest:
			i.StatusCode = "http.StatusBadRequest"
		case http.StatusForbidden:
			i.StatusCode = "http.StatusForbidden"
		case http.StatusNotFound:
			i.StatusCode = "http.StatusNotFound"
		case http.StatusLengthRequired:
			i.StatusCode = "http.StatusLengthRequired"
		case http.StatusTooManyRequests:
			i.StatusCode = "http.StatusTooManyRequests"
		case http.StatusInternalServerError:
			i.StatusCode = "http.StatusInternalServerError"
		case http.StatusServiceUnavailable:
			i.StatusCode = "http.StatusServiceUnavailable"
		case http.StatusGatewayTimeout:
			i.StatusCode = "http.StatusGatewayTimeout"
		default:
			panic(fmt.Sprintf("Unknown status value %d", entry.StatusCode))
		}

		var titleWords, nameWords []string
		for n, w := range strings.Split(i.Cause, "_") {
			switch w {
			case "IE", "API", "URI", "CCA", "NF":
				titleWords = append(titleWords, w)
				nameWords = append(nameWords, w)
			default:
				wl := strings.ToLower(w)
				wt := strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
				if n == 0 {
					titleWords = append(titleWords, wt)
				} else {
					titleWords = append(titleWords, wl)
				}
				nameWords = append(nameWords, wt)
			}
		}
		i.Title = strings.Join(titleWords, " ")
		i.GolangName = strings.Join(nameWords, "")

		errorInfos = append(errorInfos, i)
	}

	sort.Slice(errorInfos, func(i, j int) bool {
		return errorInfos[i].GolangName < errorInfos[j].GolangName
	})

	if err := template.Must(template.New("error").Parse(tmpl)).Execute(out, errorInfos); err != nil {
		panic(err)
	}
}
