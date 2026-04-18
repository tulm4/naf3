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

package problem_test

import (
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"

	"github.com/ShouheiNishi/openapi5g/models"
	"github.com/ShouheiNishi/openapi5g/utils/problem"
)

func TestMessages(t *testing.T) {
	assert.Equal(t, models.ProblemDetails{
		Status: http.StatusInternalServerError,
		Title:  lo.ToPtr("Unspecified NF failure"),
		Cause:  lo.ToPtr("UNSPECIFIED_NF_FAILURE"),
		Detail: lo.ToPtr("Not implemented"),
	}, problem.NotImplemented())

	assert.Equal(t, models.ProblemDetails{
		Status: http.StatusInternalServerError,
		Title:  lo.ToPtr("System failure"),
		Cause:  lo.ToPtr("SYSTEM_FAILURE"),
	}, problem.SystemFailure(""))

	assert.Equal(t, models.ProblemDetails{
		Status: http.StatusInternalServerError,
		Title:  lo.ToPtr("System failure"),
		Cause:  lo.ToPtr("SYSTEM_FAILURE"),
		Detail: lo.ToPtr("TEST"),
	}, problem.SystemFailure("TEST"))

	assert.Equal(t, models.ProblemDetails{
		Status: http.StatusBadRequest,
		Title:  lo.ToPtr("Mandatory IE missing"),
		Cause:  lo.ToPtr("MANDATORY_IE_MISSING"),
		InvalidParams: []models.InvalidParam{
			{
				Param:  "/foo",
				Reason: lo.ToPtr("Test"),
			},
		},
	}, problem.MandatoryIEMissing([]models.InvalidParam{
		{
			Param:  "/foo",
			Reason: lo.ToPtr("Test"),
		},
	}, ""))
}
