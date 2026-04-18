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
	"reflect"
	"testing"

	modelsFree5GC "github.com/free5gc/openapi/models"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ShouheiNishi/openapi5g/models"
)

func TestProblemDetails(t *testing.T) {
	assert.NotEqual(t, reflect.Pointer, reflect.TypeOf(models.ProblemDetails{}.Status).Kind())
}

func TestAliasTypes(t *testing.T) {
	buf, err := json.Marshal(models.ExtSnssai{
		Sst:        255,
		Sd:         "012345",
		SdRanges:   nil,
		WildcardSd: lo.ToPtr(models.ExtSnssaiWildcardSdTrue),
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"sst": 255,
	"sd": "012345",
	"wildcardSd": true
}`, string(buf))

	assert.IsType(t, modelsFree5GC.Snssai{}, models.Snssai{})
	assert.IsType(t, modelsFree5GC.PlmnId{}, models.PlmnId{})
	assert.IsType(t, modelsFree5GC.Guami{}, models.Guami{})
}

func TestIntegerTypes(t *testing.T) {
	// assert.IsType(t, int32(0), models.int32(0))
	// assert.IsType(t, int32(0), models.int32Rm(0))
	// assert.IsType(t, int64(0), models.int64(0))
	// assert.IsType(t, int64(0), models.int64Rm(0))
	assert.IsType(t, uint16(0), models.Uint16(0))
	// assert.IsType(t, uint16(0), models.Uint16Rm(0))
	assert.IsType(t, uint32(0), models.Uint32(0))
	assert.IsType(t, uint32(0), models.Uint32Rm(0))
	assert.IsType(t, uint64(0), models.Uint64(0))
	// assert.IsType(t, uint64(0), models.Uint64Rm(0))
	assert.IsType(t, uint(0), models.Uinteger(0))
	// assert.IsType(t, uint(0), models.UintegerRm(0))
}
