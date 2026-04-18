// Copyright 2024 APRESIA Systems LTD.
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

package openapi

import (
	"fmt"
	"strings"
)

type jsonPointer []string

type jsonPointerResolver interface {
	GetFromJsonPointerSub(jsonPointer) (any, error)
}

func (d *Document) GetFromJsonPointer(pointer string) (any, error) {
	tokens := strings.Split(pointer, "/")
	if len(tokens) < 2 || tokens[0] != "" {
		return nil, fmt.Errorf("invalid JSON pointer %s", pointer)
	}

	tokens = tokens[1:]
	for i := range tokens {
		if tokens[i] == "" {
			return nil, fmt.Errorf("invalid JSON pointer %s", pointer)
		}
		tokens[i] = strings.Replace(strings.Replace(strings.Replace(strings.Replace(tokens[i], "~1", "/", -1), "~0", "~", -1), "%7B", "{", -1), "%7D", "}", -1)
	}

	return d.GetFromJsonPointerSub(tokens)
}
