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

package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ShouheiNishi/openapi5g/models"
	"github.com/ShouheiNishi/openapi5g/utils/error/middleware"
)

//go:generate go run github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen --config=config.yaml middleware_test.yaml

type testStrictServerInterfaceErrorMiddleware struct {
	result TestResponseObject
	err    error
}

func (s *testStrictServerInterfaceErrorMiddleware) Test(ctx context.Context, request TestRequestObject) (TestResponseObject, error) {
	return s.result, s.err
}

func TestErrorMiddleware(t *testing.T) {
	var ssi testStrictServerInterfaceErrorMiddleware
	si := NewStrictHandler(&ssi, []StrictMiddlewareFunc{middleware.GinStrictServerMiddleware})
	gin := gin.New()
	gin.Use(middleware.GinMiddleWare)
	gin.NoRoute(middleware.GinNotFoundHandler)
	RegisterHandlersWithOptions(gin, si, GinServerOptions{
		ErrorHandler: middleware.GinServerErrorHandler,
	})

	testOne := func(method, target, body string, result TestResponseObject, handlerErr error) (*http.Response, string, error) {
		ssi = testStrictServerInterfaceErrorMiddleware{
			result: result,
			err:    handlerErr,
		}
		req := httptest.NewRequest(method, target, bytes.NewReader([]byte(body)))
		rec := httptest.NewRecorder()
		gin.ServeHTTP(rec, req)
		res := rec.Result()
		resBody, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, "", err
		}
		err = res.Body.Close()
		if err != nil {
			return nil, "", err
		}
		return res, string(resBody), nil
	}

	goodMethod := "POST"
	goodUri := "/test/123"
	goodBody := ""
	if tmp, err := json.Marshal(Request{
		Field1: "123",
		Field2: "ABC",
	}); err != nil {
		require.NoError(t, err)
	} else {
		goodBody = string(tmp)
	}
	goodResponse := Test201JSONResponse(Response{
		FieldA: "123",
		FieldB: "ABC",
	})
	t.Run("Good case", func(t *testing.T) {
		res, resBody, err := testOne(goodMethod, goodUri, goodBody, goodResponse, nil)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusCreated, res.StatusCode)
		assert.Equal(t, "application/json", res.Header.Get("Content-Type"))
		assert.JSONEq(t, `{"fieldA": "123", "fieldB": "ABC"}`, resBody)
	})

	t.Run("Explicit return problem detail", func(t *testing.T) {
		res, resBody, err := testOne(goodMethod, goodUri, goodBody,
			TestdefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusInternalServerError,
				Body: models.ProblemDetails{
					Status: http.StatusInternalServerError,
					Cause:  lo.ToPtr("TEST"),
				},
			}, nil)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
		assert.Equal(t, "application/problem+json", res.Header.Get("Content-Type"))
		assert.JSONEq(t, `{"status": 500, "cause": "TEST"}`, resBody)
	})

	t.Run("No result from handler", func(t *testing.T) {
		res, resBody, err := testOne(goodMethod, goodUri, goodBody, nil, nil)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
		assert.Equal(t, "application/problem+json", res.Header.Get("Content-Type"))
		assert.JSONEq(t, `{"status": 500, "cause": "SYSTEM_FAILURE", "title": "System failure", "detail": "no responses are returned"}`, resBody)
	})

	t.Run("Result handler error", func(t *testing.T) {
		res, resBody, err := testOne(goodMethod, goodUri, goodBody, Test201JSONResponse(Response{
			FieldA: "123",
			FieldB: "ABC",
			FieldF: lo.ToPtr(float32(math.Inf(+1))),
		}), nil)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
		assert.Equal(t, "application/problem+json", res.Header.Get("Content-Type"))
		assert.JSONEq(t, `{"status": 500,
			"cause": "UNSPECIFIED_NF_FAILURE",
			"title": "Unspecified NF failure",
			"detail": "json: unsupported value: +Inf"
		}`, resBody)
	})

	t.Run("Error from handler", func(t *testing.T) {
		res, resBody, err := testOne(goodMethod, goodUri, goodBody, nil, errors.New("Test error"))
		assert.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
		assert.Equal(t, "application/problem+json", res.Header.Get("Content-Type"))
		assert.JSONEq(t, `{"status": 500, "cause": "SYSTEM_FAILURE", "title": "System failure", "detail": "Test error"}`, resBody)
	})

	t.Run("Bad method", func(t *testing.T) {
		res, resBody, err := testOne("PUT", goodUri, goodBody, goodResponse, nil)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, res.StatusCode)
		assert.Equal(t, "application/problem+json", res.Header.Get("Content-Type"))
		assert.JSONEq(t, `{"status": 404, "cause": "RESOURCE_URI_STRUCTURE_NOT_FOUND", "title": "Resource URI structure not found"}`, resBody)
	})

	t.Run("Bad path parameter", func(t *testing.T) {
		res, resBody, err := testOne(goodMethod, "/test/abc", goodBody, goodResponse, nil)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Equal(t, "application/problem+json", res.Header.Get("Content-Type"))
		assert.JSONEq(t, `{"status": 400,
			"cause": "UNSPECIFIED_MSG_FAILURE",
			"title": "Unspecified msg failure",
			"detail": "Invalid format for parameter path-param: error binding string parameter: strconv.ParseInt: parsing \"abc\": invalid syntax"
		}`, resBody)
	})

	t.Run("Bad request body", func(t *testing.T) {
		res, resBody, err := testOne(goodMethod, goodUri, "", goodResponse, nil)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Equal(t, "application/problem+json", res.Header.Get("Content-Type"))
		assert.JSONEq(t, `{"status": 400, "cause": "UNSPECIFIED_MSG_FAILURE", "title": "Unspecified msg failure", "detail": "EOF"}`, resBody)
	})
}
