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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ShouheiNishi/openapi5g/amf/communication"
	"github.com/ShouheiNishi/openapi5g/models"
	"github.com/ShouheiNishi/openapi5g/utils/problem"
)

type resInterface interface {
	Dummy()
}

type resType struct {
	Body         []byte
	HTTPResponse *http.Response
}

func (r resType) Dummy() {}

func TestExtractBodyAndHttpResponse(t *testing.T) {
	res := struct {
		Body         []byte
		HTTPResponse *http.Response
	}{}
	body, httpResponse, err := problem.ExtractBodyAndHttpResponse(res)
	assert.NoError(t, err)
	assert.Nil(t, body)
	assert.Nil(t, httpResponse)

	_, _, err = problem.ExtractBodyAndHttpResponse(nil)
	assert.Error(t, err)
	assert.NotContains(t, "panic", err.Error())

	body, httpResponse, err = problem.ExtractBodyAndHttpResponse(&res)
	assert.NoError(t, err)
	assert.Nil(t, body)
	assert.Nil(t, httpResponse)

	body, httpResponse, err = problem.ExtractBodyAndHttpResponse(resInterface(resType(res)))
	assert.NoError(t, err)
	assert.Nil(t, body)
	assert.Nil(t, httpResponse)

	_, _, err = problem.ExtractBodyAndHttpResponse("string")
	assert.Error(t, err)
	assert.NotContains(t, "panic", err.Error())

	_, _, err = problem.ExtractBodyAndHttpResponse(struct {
		HTTPResponse *http.Response
	}{})
	assert.Error(t, err)
	assert.NotContains(t, "panic", err.Error())

	_, _, err = problem.ExtractBodyAndHttpResponse(struct {
		Body         int
		HTTPResponse *http.Response
	}{})
	assert.Error(t, err)
	assert.NotContains(t, "panic", err.Error())

	_, _, err = problem.ExtractBodyAndHttpResponse(struct {
		Body []byte
	}{})
	assert.Error(t, err)
	assert.NotContains(t, "panic", err.Error())

	_, _, err = problem.ExtractBodyAndHttpResponse(struct {
		Body         []byte
		HTTPResponse string
	}{})
	assert.Error(t, err)
	assert.NotContains(t, "panic", err.Error())

	body, httpResponse, err = problem.ExtractBodyAndHttpResponse(resType{
		Body: []byte("TEST"),
		HTTPResponse: &http.Response{
			StatusCode: 200,
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, []byte("TEST"), body)
	assert.Equal(t, 200, httpResponse.StatusCode)
}

func TestExtractStatusCodeAndProblemDetails(t *testing.T) {
	pdSend := problem.NotImplemented()
	buf, err := json.Marshal(pdSend)
	assert.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
		w.WriteHeader(pdSend.Status)
		_, err := w.Write(buf)
		assert.NoError(t, err)
	}))
	defer ts.Close()

	c, err := communication.NewClientWithResponses(ts.URL)
	assert.NoError(t, err)

	res, err := c.N1N2MessageSubscribeWithResponse(context.TODO(), "", models.UeN1N2InfoSubscriptionCreateData{})
	assert.NoError(t, err)

	statusCode, pdRecv, err := problem.ExtractStatusCodeAndProblemDetails(res)
	assert.NoError(t, err)
	assert.Equal(t, pdSend.Status, statusCode)
	assert.Equal(t, pdSend, *pdRecv)
}
