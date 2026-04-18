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
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ShouheiNishi/openapi5g/models"
	"github.com/ShouheiNishi/openapi5g/nrf/management"
)

type testAdditionalPropertiesInResponseType struct{ t *testing.T }

// Retrieves a collection of NF Instances
// (GET /nf-instances)
func (t *testAdditionalPropertiesInResponseType) GetNFInstances(ctx context.Context, request management.GetNFInstancesRequestObject) (management.GetNFInstancesResponseObject, error) {
	var l models.LinksValueSchema
	err := l.FromLink(models.Link{
		Href: lo.ToPtr("baz"),
	})
	require.NoError(t.t, err)
	res := management.ResponseForPathsNfInstancesGetResponses200Application3gppHalJson{
		Links: &map[string]models.LinksValueSchema{
			"foo": l,
		},
	}
	res.Set("bar", "test")
	return management.GetNFInstances200Application3gppHalPlusJSONResponse(res), nil
}

// Discover communication options supported by NRF for NF Instances
// (OPTIONS /nf-instances)
func (t *testAdditionalPropertiesInResponseType) OptionsNFInstances(ctx context.Context, request management.OptionsNFInstancesRequestObject) (management.OptionsNFInstancesResponseObject, error) {
	panic("not implemented") // not used
}

// Deregisters a given NF Instance
// (DELETE /nf-instances/{nfInstanceID})
func (t *testAdditionalPropertiesInResponseType) DeregisterNFInstance(ctx context.Context, request management.DeregisterNFInstanceRequestObject) (management.DeregisterNFInstanceResponseObject, error) {
	panic("not implemented") // not used
}

// Read the profile of a given NF Instance
// (GET /nf-instances/{nfInstanceID})
func (t *testAdditionalPropertiesInResponseType) GetNFInstance(ctx context.Context, request management.GetNFInstanceRequestObject) (management.GetNFInstanceResponseObject, error) {
	panic("not implemented") // not used
}

// Update NF Instance profile
// (PATCH /nf-instances/{nfInstanceID})
func (t *testAdditionalPropertiesInResponseType) UpdateNFInstance(ctx context.Context, request management.UpdateNFInstanceRequestObject) (management.UpdateNFInstanceResponseObject, error) {
	panic("not implemented") // not used
}

// Register a new NF Instance
// (PUT /nf-instances/{nfInstanceID})
func (t *testAdditionalPropertiesInResponseType) RegisterNFInstance(ctx context.Context, request management.RegisterNFInstanceRequestObject) (management.RegisterNFInstanceResponseObject, error) {
	panic("not implemented") // not used
}

// Create a new subscription
// (POST /subscriptions)
func (t *testAdditionalPropertiesInResponseType) CreateSubscription(ctx context.Context, request management.CreateSubscriptionRequestObject) (management.CreateSubscriptionResponseObject, error) {
	panic("not implemented") // not used
}

// Deletes a subscription
// (DELETE /subscriptions/{subscriptionID})
func (t *testAdditionalPropertiesInResponseType) RemoveSubscription(ctx context.Context, request management.RemoveSubscriptionRequestObject) (management.RemoveSubscriptionResponseObject, error) {
	panic("not implemented") // not used
}

// Updates a subscription
// (PATCH /subscriptions/{subscriptionID})
func (t *testAdditionalPropertiesInResponseType) UpdateSubscription(ctx context.Context, request management.UpdateSubscriptionRequestObject) (management.UpdateSubscriptionResponseObject, error) {
	panic("not implemented") // not used
}

func TestAdditionalPropertiesInResponse(t *testing.T) {
	router := gin.New()
	management.RegisterHandlers(router, management.NewStrictHandler(&testAdditionalPropertiesInResponseType{t: t}, nil))

	server := httptest.NewServer(router.Handler())
	defer server.Close()

	client, err := management.NewClientWithResponses(server.URL)
	require.NoError(t, err)
	res, err := client.GetNFInstancesWithResponse(context.TODO(), nil)
	require.NoError(t, err)
	require.Equal(t, 200, res.StatusCode())
	require.NotNil(t, res.Application3gppHalJSON200)
	bar, exist := res.Application3gppHalJSON200.Get("bar")
	assert.True(t, exist)
	assert.Equal(t, "test", bar)
	assert.JSONEq(t, `{
	"_links": {
		"foo": {
			"href": "baz"
		}
	},
	"bar": "test"
}`, string(res.Body))
}
