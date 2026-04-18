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
	"github.com/stretchr/testify/assert"

	"github.com/ShouheiNishi/openapi5g/nrf/discovery"
)

type testOapiCodegenPR1412Type struct{}

// Search a collection of NF Instances
// (GET /nf-instances)
func (t *testOapiCodegenPR1412Type) SearchNFInstances(ctx context.Context, request discovery.SearchNFInstancesRequestObject) (discovery.SearchNFInstancesResponseObject, error) {
	return discovery.SearchNFInstances200JSONResponse{}, nil
}

// (GET /searches/{searchId})
func (t *testOapiCodegenPR1412Type) RetrieveStoredSearch(ctx context.Context, request discovery.RetrieveStoredSearchRequestObject) (discovery.RetrieveStoredSearchResponseObject, error) {
	panic("not implemented") // not used
}

// (GET /searches/{searchId}/complete)
func (t *testOapiCodegenPR1412Type) RetrieveCompleteSearch(ctx context.Context, request discovery.RetrieveCompleteSearchRequestObject) (discovery.RetrieveCompleteSearchResponseObject, error) {
	panic("not implemented") // not used
}

// t *testOapiCodegenPR1412Type discovery.StrictServerInterface

func TestOapiCodegenPR1412(t *testing.T) {
	router := gin.New()
	discovery.RegisterHandlers(router, discovery.NewStrictHandler(&testOapiCodegenPR1412Type{}, nil))

	req := httptest.NewRequest("GET", "/nf-instances?target-nf-type=foo&requester-nf-type=bar", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Result().StatusCode)
	assert.Equal(t, "application/json", rec.Result().Header.Get("Content-Type"))
	assert.Nil(t, rec.Result().Header.Values("Cache-Control"))
}
