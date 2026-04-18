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

	"gopkg.in/yaml.v3"
)

type Reference struct {
	Path    string
	Pointer string
}

var _ fmt.Stringer = Reference{}
var _ yaml.IsZeroer = Reference{}
var _ yaml.Marshaler = Reference{}
var _ yaml.Unmarshaler = &Reference{}

func (r Reference) String() string {
	if r.Pointer == "" {
		return ""
	}
	return r.Path + "#" + r.Pointer
}

func (r Reference) IsZero() bool {
	return r.Path == "" && r.Pointer == ""
}

func (r Reference) MarshalYAML() (any, error) {
	return r.String(), nil
}

func (r *Reference) UnmarshalYAML(n *yaml.Node) error {
	var s string
	if err := n.Decode(&s); err != nil {
		return err
	}
	return r.Parse(s)
}

func (r *Reference) Parse(s string) error {
	split := strings.Split(s, "#")
	if len(split) != 2 || split[1] == "" {
		return fmt.Errorf("invalid reference %s", s)
	}
	r.Path = split[0]
	r.Pointer = split[1]
	return nil
}

func ParseReference(s string) (*Reference, error) {
	r := &Reference{}
	if err := r.Parse(s); err != nil {
		return nil, err
	}
	return r, nil
}
