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

package test_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ShouheiNishi/openapi5g/models"
)

func TestOapiCodegenPR1303(t *testing.T) {
	var dec any
	buf, err := json.Marshal(models.TunnelInfo{AnType: ""})
	require.NoError(t, err)
	err = json.Unmarshal(buf, &dec)
	require.NoError(t, err)
	m, _ := dec.(map[string]any)
	require.NotNil(t, m)
	_, exist := m["anType"]
	assert.False(t, exist)

	buf, err = json.Marshal(models.ProblemDetails{InvalidParams: []models.InvalidParam{}})
	require.NoError(t, err)
	err = json.Unmarshal(buf, &dec)
	require.NoError(t, err)
	m, _ = dec.(map[string]any)
	require.NotNil(t, m)
	_, exist = m["invalidParams"]
	assert.False(t, exist)

	buf, err = json.Marshal(models.AmfEventMode{SampRatio: 0})
	require.NoError(t, err)
	err = json.Unmarshal(buf, &dec)
	require.NoError(t, err)
	m, _ = dec.(map[string]any)
	require.NotNil(t, m)
	_, exist = m["sampRatio"]
	assert.False(t, exist)
}
