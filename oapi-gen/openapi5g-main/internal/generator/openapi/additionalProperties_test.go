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

type testAdditionalProperties struct {
	Test  openapi.AdditionalProperties `yaml:"test,omitempty"`
	Dummy int                          `yaml:"dummy,omitempty"`
}

func TestAdditionalProperties(t *testing.T) {
	tests := []struct {
		name      string
		value     testAdditionalProperties
		yaml      string
		roundTrip bool
	}{
		{
			name: "skip",
			value: testAdditionalProperties{
				Dummy: 1,
			},
			yaml: `
dummy: 1
`,
			roundTrip: true,
		},
		{
			name: "SchemaRef",
			value: testAdditionalProperties{
				Test: openapi.AdditionalProperties{
					SchemaRef: &openapi.Ref[openapi.Schema]{
						Ref: openapi.Reference{
							Path:    "test",
							Pointer: "/",
						},
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
			value: testAdditionalProperties{
				Test: openapi.AdditionalProperties{
					SchemaRef: &openapi.Ref[openapi.Schema]{
						Value: &openapi.Schema{
							Description: "foo",
						},
					},
				},
			},
			yaml: `
test:
  description: 'foo'
`,
			roundTrip: true,
		},
		{
			name: "true",
			value: testAdditionalProperties{
				Test: openapi.AdditionalProperties{
					Bool: lo.ToPtr(true),
				},
			},
			yaml: `
test: true
`,
			roundTrip: true,
		},
		{
			name: "false",
			value: testAdditionalProperties{
				Test: openapi.AdditionalProperties{
					Bool: lo.ToPtr(false),
				},
			},
			yaml: `
test: false
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
				var v testAdditionalProperties
				err = yaml.Unmarshal(buf, &v)
				assert.NoError(t, err)
				assert.Equal(t, tt.value, v)
			}
		})
	}
}
