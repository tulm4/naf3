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
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"

	"github.com/ShouheiNishi/openapi5g/models"
	"github.com/ShouheiNishi/openapi5g/nrf/token"
	"github.com/ShouheiNishi/openapi5g/nssf/selection"
)

type testProblemDefaultResponseType struct{}

func (t *testProblemDefaultResponseType) AccessTokenRequest(ctx context.Context, request token.AccessTokenRequestRequestObject) (token.AccessTokenRequestResponseObject, error) {
	return token.AccessTokenRequestdefaultApplicationProblemPlusJSONResponse{
		StatusCode: 444,
		Body: models.ProblemDetails{
			Status: 444,
			Title:  lo.ToPtr("Test Problem"),
			Cause:  lo.ToPtr("TEST_PROBLEM"),
			Detail: lo.ToPtr("TEST"),
		}}, nil
}

func TestProblemDefaultResponse(t *testing.T) {
	router := gin.New()
	token.RegisterHandlers(router, token.NewStrictHandler(&testProblemDefaultResponseType{}, nil))

	req := httptest.NewRequest("POST", "/oauth2/token", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, 444, rec.Result().StatusCode)
	assert.Equal(t, "application/problem+json", rec.Result().Header.Get("Content-Type"))
	assert.JSONEq(t, `{
	"status": 444,
	"title":  "Test Problem",
	"cause":  "TEST_PROBLEM",
	"detail": "TEST"
}`, rec.Body.String())
}

type testProblemDefaultStatus struct{}

func (t *testProblemDefaultStatus) NSSelectionGet(ctx context.Context, request selection.NSSelectionGetRequestObject) (selection.NSSelectionGetResponseObject, error) {
	return selection.NSSelectionGetdefaultApplicationProblemPlusJSONResponse{
		StatusCode: 444,
		Body: models.ProblemDetails{
			Status: 444,
			Title:  lo.ToPtr("Test Problem"),
			Cause:  lo.ToPtr("TEST_PROBLEM"),
			Detail: lo.ToPtr("TEST"),
		}}, nil
}

func TestProblemDefaultStatus(t *testing.T) {
	router := gin.New()
	selection.RegisterHandlers(router, selection.NewStrictHandler(&testProblemDefaultStatus{}, nil))

	var nfIdDummy uuid.UUID
	req := httptest.NewRequest("GET", "/network-slice-information?nf-type=test&nf-id="+nfIdDummy.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, 444, rec.Result().StatusCode)
	assert.Equal(t, "application/problem+json", rec.Result().Header.Get("Content-Type"))
	assert.JSONEq(t, `{
	"status": 444,
	"title":  "Test Problem",
	"cause":  "TEST_PROBLEM",
	"detail": "TEST"
}`, rec.Body.String())
}
