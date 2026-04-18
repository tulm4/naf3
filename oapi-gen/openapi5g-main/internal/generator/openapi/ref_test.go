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

package openapi_test

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"

	"github.com/ShouheiNishi/openapi5g/internal/generator/openapi"
)

type testTypeInner struct {
	Foo string `yaml:"foo,omitempty"`
	Bar int    `yaml:"bar,omitempty"`
}

type testTypeOuter struct {
	Test  openapi.Ref[testTypeInner] `yaml:"test,omitempty"`
	Dummy int                        `yaml:"dummy,omitempty"`
}

func TestRef(t *testing.T) {
	tests := []struct {
		name      string
		value     testTypeOuter
		yaml      string
		roundTrip bool
	}{
		{
			name: "skip",
			value: testTypeOuter{
				Dummy: 1,
			},
			yaml: `
dummy: 1
`,
			roundTrip: true,
		},
		{
			name: "Ref",
			value: testTypeOuter{
				Test: openapi.Ref[testTypeInner]{
					Ref: openapi.Reference{
						Path:    "test",
						Pointer: "/",
					},
				},
			},
			yaml: `
test:
  $ref: 'test#/'
`,
			roundTrip: true,
		},
		{
			name: "Value",
			value: testTypeOuter{
				Test: openapi.Ref[testTypeInner]{
					Value: &testTypeInner{
						Foo: "foo",
					},
				},
			},
			yaml: `
test:
  foo: 'foo'
`,
			roundTrip: true,
		},
		{
			name: "refValue",
			value: testTypeOuter{
				Test: openapi.Ref[testTypeInner]{
					Ref: openapi.Reference{
						Path:    "test",
						Pointer: "/",
					},
					Value: &testTypeInner{
						Foo: "foo",
					},
				},
			},
			yaml: `
test:
  $ref: 'test#/'
`,
			roundTrip: false,
		},
		{
			name: "refRemove",
			value: testTypeOuter{
				Test: openapi.Ref[testTypeInner]{
					Ref: openapi.Reference{
						Path:    "test",
						Pointer: "/",
					},
					Value: &testTypeInner{
						Foo: "foo",
					},
					CurFile: lo.ToPtr("test"),
				},
			},
			yaml: `
test:
  $ref: '#/'
`,
			roundTrip: false,
		},
		{
			name: "refNotRemove",
			value: testTypeOuter{
				Test: openapi.Ref[testTypeInner]{
					Ref: openapi.Reference{
						Path:    "test",
						Pointer: "/",
					},
					Value: &testTypeInner{
						Foo: "foo",
					},
					CurFile: lo.ToPtr("test2"),
				},
			},
			yaml: `
test:
  $ref: 'test#/'
`,
			roundTrip: false,
		},
		{
			name: "skip",
			value: testTypeOuter{
				Dummy: 1,
				Test: openapi.Ref[testTypeInner]{
					Value: &testTypeInner{},
				},
			},
			yaml: `
dummy: 1
test:
  {}
`,
			roundTrip: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			buf, err := yaml.Marshal(tt.value)
			assert.NoError(t, err)
			assert.YAMLEq(t, tt.yaml, string(buf))
			if tt.roundTrip {
				var v testTypeOuter
				err = yaml.Unmarshal(buf, &v)
				assert.NoError(t, err)
				assert.Equal(t, tt.value, v)
			}
		})
	}
}
