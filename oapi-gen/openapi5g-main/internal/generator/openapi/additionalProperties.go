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
	"errors"

	"gopkg.in/yaml.v3"
)

type AdditionalProperties struct {
	Bool      *bool
	SchemaRef *Ref[Schema]
}

var _ yaml.IsZeroer = AdditionalProperties{}
var _ yaml.Marshaler = AdditionalProperties{}
var _ yaml.Unmarshaler = &AdditionalProperties{}

func (a AdditionalProperties) IsZero() bool {
	return a.Bool == nil && a.SchemaRef == nil
}

func (a AdditionalProperties) MarshalYAML() (interface{}, error) {
	if a.Bool != nil {
		return *a.Bool, nil
	}
	if a.SchemaRef != nil {
		return *a.SchemaRef, nil
	}
	return nil, errors.New("try to marshal empty AdditionalProperties")
}

func (a *AdditionalProperties) UnmarshalYAML(value *yaml.Node) error {
	var boolNew bool
	if err := value.Decode(&boolNew); err == nil {
		a.Bool = &boolNew
		return nil
	}
	return value.Decode(&a.SchemaRef)
}

func (a *AdditionalProperties) GetFromJsonPointerSub(p jsonPointer) (any, error) {
	if a.SchemaRef == nil {
		return nil, errors.New("cannot traverse non schema AdditionalProperties")
	}
	return a.SchemaRef.GetFromJsonPointerSub(p)
}
