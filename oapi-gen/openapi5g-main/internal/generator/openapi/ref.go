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
	"fmt"

	"gopkg.in/yaml.v3"
)

type baseRef struct {
	Ref         Reference `yaml:"$ref"`
	Description string    `yaml:"description,omitempty"`
	MinItems    int       `yaml:"minItems,omitempty"`
}

type Ref[T any] struct {
	Ref         Reference
	Description string
	MinItems    int
	Value       *T
	CurFile     *string
}

var _ yaml.IsZeroer = Ref[int]{}
var _ yaml.Marshaler = Ref[int]{}
var _ yaml.Unmarshaler = &Ref[int]{}

func (r Ref[T]) IsZero() bool {
	return r.Ref.IsZero() && r.Value == nil
}

func (r Ref[T]) HasRef() bool {
	return !r.Ref.IsZero()
}

func (r Ref[T]) MarshalYAML() (interface{}, error) {
	if !r.HasRef() {
		return r.Value, nil
	}
	ref := r.Ref
	if r.CurFile != nil {
		if ref.Path == *r.CurFile {
			ref.Path = ""
		}
	}
	return baseRef{Ref: ref, Description: r.Description, MinItems: r.MinItems}, nil
}

func (r *Ref[T]) UnmarshalYAML(value *yaml.Node) error {
	var rNew baseRef
	if err := value.Decode(&rNew); err == nil && !rNew.Ref.IsZero() {
		r.Ref = rNew.Ref
		r.Description = rNew.Description
		r.MinItems = rNew.MinItems
		return nil
	}
	var vNew T
	if err := value.Decode(&vNew); err != nil {
		return err
	}
	r.Value = &vNew
	return nil
}

func (r *Ref[T]) GetFromJsonPointerSub(p jsonPointer) (any, error) {
	if !r.HasRef() {
		if r.Value == nil {
			return nil, errors.New("cannot traverse empty ref")
		} else {
			if v, _ := (any)(r.Value).(jsonPointerResolver); v != nil {
				return v.GetFromJsonPointerSub(p)
			} else {
				return nil, fmt.Errorf("cannot traverse type %T", r.Value)
			}
		}
	}

	switch p[0] {
	case "$ref":
		if len(p) == 1 {
			return r.Ref.String(), nil
		} else {
			return nil, errors.New("cannot traverse type string")
		}
	case "description":
		if len(p) == 1 {
			return r.Description, nil
		} else {
			return nil, errors.New("cannot traverse type string")
		}
	case "minItems":
		if len(p) == 1 {
			return r.MinItems, nil
		} else {
			return nil, errors.New("cannot traverse type int")
		}
	default:
		return nil, fmt.Errorf("unknown entry %s", p[0])
	}
}
