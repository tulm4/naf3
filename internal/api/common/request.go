package common

import (
	"encoding/json"
	"io"
	"net/http"
)

// ReadRequestBody reads and decodes a JSON request body.
//
// It performs two passes over the bytes:
//  1. Unmarshal into map[string]any to build a presence map (required vs absent)
//  2. Unmarshal into the typed struct T
//
// If decoding fails, a ValidationProblem is written to w and nil/nil/nil is returned.
// The presence map allows callers to distinguish "field absent" from "field is zero value".
//
// Usage:
//
//	body, present, err := ReadRequestBody[w](w, r)
//	if err != nil { return }
//	if !present["snssai"] { ... handle absent snssai ... }
func ReadRequestBody[T any](w http.ResponseWriter, r *http.Request) (*T, map[string]bool, error) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		WriteProblem(w, ValidationProblem("body", err.Error()))
		return nil, nil, err
	}

	var raw map[string]any
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		WriteProblem(w, ValidationProblem("body", err.Error()))
		return nil, nil, err
	}

	present := make(map[string]bool, len(raw))
	for k := range raw {
		present[k] = true
	}

	var body T
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		WriteProblem(w, ValidationProblem("body", err.Error()))
		return nil, nil, err
	}

	return &body, present, nil
}
