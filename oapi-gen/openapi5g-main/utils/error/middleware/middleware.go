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

package middleware

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	strictgin "github.com/oapi-codegen/runtime/strictmiddleware/gin"
	"github.com/samber/lo"

	"github.com/ShouheiNishi/openapi5g/models"
	utils_error "github.com/ShouheiNishi/openapi5g/utils/error"
	"github.com/ShouheiNishi/openapi5g/utils/problem"
)

func writeProblemDetails(ctx *gin.Context, pd models.ProblemDetails, removeHeader bool) error {
	if buf, err := json.Marshal(pd); err != nil {
		return err
	} else {
		if removeHeader {
			for key := range ctx.Writer.Header() {
				ctx.Writer.Header().Del(key)
			}
		}
		ctx.Header("Content-Type", "application/problem+json")
		ctx.Header("Content-Length", strconv.Itoa(len(buf)))
		ctx.Status(pd.Status)
		_, err := ctx.Writer.Write(buf)
		return err
	}
}

func GinServerErrorHandler(ctx *gin.Context, errIn error, statusCode int) {
	var pd models.ProblemDetails
	switch statusCode {
	case http.StatusBadRequest:
		pd = problem.UnspecifiedMsgFailure(errIn.Error())
	case http.StatusInternalServerError:
		pd = problem.UnspecifiedNFFailure(errIn.Error())
	default:
		pd = models.ProblemDetails{
			Status: statusCode,
			Detail: lo.ToPtr(errIn.Error()),
		}
	}

	if err := writeProblemDetails(ctx, pd, false); err != nil {
		ctx.Error(errIn)
		ctx.Error(err)
	}
}

func GinMiddleWare(ctx *gin.Context) {
	ctx.Next()

	if ctx.Writer.Written() {
		// Do nothing
		return
	}

	if errFromHandler := ctx.Errors.Last(); errFromHandler != nil {
		var pd models.ProblemDetails
		switch ctx.Writer.Status() {
		case http.StatusBadRequest:
			pd = problem.UnspecifiedMsgFailure(errFromHandler.Error())
		case http.StatusInternalServerError:
			fallthrough
		default:
			pd = problem.UnspecifiedNFFailure(errFromHandler.Error())
		}
		if err := writeProblemDetails(ctx, pd, false); err != nil {
			ctx.Error(err)
		}
	}
}

func GinStrictServerMiddleware(f strictgin.StrictGinHandlerFunc, operationID string) strictgin.StrictGinHandlerFunc {
	return func(ctx *gin.Context, request any) (response any, err error) {
		response, err = f(ctx, request)

		if ctx.Writer.Written() {
			// Do nothing
			return
		}

		if response == nil && err == nil {
			err = errors.New("no responses are returned")
		}

		if err != nil {
			pd := utils_error.ErrorToProblemDetails(err)
			if errWrite := writeProblemDetails(ctx, pd, false); errWrite != nil {
				return nil, fmt.Errorf("%w (fail to send problemDetails: %w)", err, errWrite)
			}
			return nil, nil
		}

		return
	}
}

func GinNotFoundHandler(ctx *gin.Context) {
	pd := problem.ResourceURIStructureNotFound("")
	if err := writeProblemDetails(ctx, pd, false); err != nil {
		ctx.Error(err)
	}
}
