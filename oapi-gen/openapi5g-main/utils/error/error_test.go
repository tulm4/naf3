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

package error_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"

	"github.com/ShouheiNishi/openapi5g/models"
	utils_error "github.com/ShouheiNishi/openapi5g/utils/error"
)

func TestWrappedOpenAPIError(t *testing.T) {
	errBase := errors.New("Test")

	var err error = &utils_error.WrappedOpenAPIError{
		Method:    "test.test",
		BaseError: errBase,
	}

	assert.Equal(t, "test.test: Test", err.Error())
	assert.ErrorIs(t, err, errBase)
}

func TestProblemDetailsError(t *testing.T) {
	var err error = &utils_error.ProblemDetailsError{
		Message: "test",
		ProblemDetails: models.ProblemDetails{
			Status: http.StatusInternalServerError,
			Cause:  lo.ToPtr("TEST_TEST"),
		},
	}

	assert.Equal(t, "test {Status:500 Cause:TEST_TEST}", err.Error())
}

func TestExtractAndWrapOpenAPIError(t *testing.T) {
	errBase := errors.New("Test")
	err := utils_error.ExtractAndWrapOpenAPIError("test", nil, errBase)
	assert.Equal(t, "test: Test", err.Error())
	assert.IsType(t, &utils_error.WrappedOpenAPIError{}, err)
	assert.Equal(t, "test", err.(*utils_error.WrappedOpenAPIError).Method)
	assert.Equal(t, errBase, errors.Unwrap(err))

	err = utils_error.ExtractAndWrapOpenAPIError("test", nil, nil)
	assert.ErrorContains(t, err, "problem.ExtractBodyAndHttpResponse:")

	var resDummy struct {
		Body         []byte
		HTTPResponse *http.Response
	}
	err = utils_error.ExtractAndWrapOpenAPIError("test", &resDummy, nil)
	assert.ErrorContains(t, err, "no HTTP response")

	resDummy.HTTPResponse = &http.Response{}
	resDummy.Body = make([]byte, 10)
	err = utils_error.ExtractAndWrapOpenAPIError("test", &resDummy, nil)
	assert.ErrorContains(t, err, "no problemDetails, status code = 0, content-type = ")

	resDummy.HTTPResponse.Header = make(http.Header)
	resDummy.HTTPResponse.Header.Set("Content-Type", "application/problem+json")
	resDummy.Body = make([]byte, 0)
	err = utils_error.ExtractAndWrapOpenAPIError("test", &resDummy, nil)
	assert.ErrorContains(t, err, "no problemDetails, status code = 0, content-type = application/problem+json")

	resDummy.Body = make([]byte, 10)
	err = utils_error.ExtractAndWrapOpenAPIError("test", &resDummy, nil)
	assert.ErrorContains(t, err, "json.Unmarshal:")

	resDummy.Body = []byte(`{"status": 500, "cause": "TEST"}`)
	err = utils_error.ExtractAndWrapOpenAPIError("test", &resDummy, nil)
	assert.ErrorContains(t, err, "problemDetails received ")
	assert.IsType(t, &utils_error.ProblemDetailsError{}, errors.Unwrap(err))
	assert.Equal(t, models.ProblemDetails{
		Status: 500,
		Cause:  lo.ToPtr("TEST"),
	}, errors.Unwrap(err).(*utils_error.ProblemDetailsError).ProblemDetails)
}

func TestErrorToProblemDetails(t *testing.T) {
	pd := utils_error.ErrorToProblemDetails(&utils_error.WrappedOpenAPIError{
		Method: "Test",
		BaseError: &utils_error.ProblemDetailsError{
			Message: "TEST",
			ProblemDetails: models.ProblemDetails{
				Status: 500,
				Cause:  lo.ToPtr("TEST"),
			},
		},
	})
	assert.Equal(t, models.ProblemDetails{
		Status: 500,
		Cause:  lo.ToPtr("TEST"),
	}, pd)

	pd = utils_error.ErrorToProblemDetails(errors.New("TEST"))
	assert.Equal(t, models.ProblemDetails{
		Status: 500,
		Title:  lo.ToPtr("System failure"),
		Cause:  lo.ToPtr("SYSTEM_FAILURE"),
		Detail: lo.ToPtr("TEST"),
	}, pd)
}
