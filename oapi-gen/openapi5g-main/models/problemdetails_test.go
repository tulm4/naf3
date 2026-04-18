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

package models_test

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"

	"github.com/ShouheiNishi/openapi5g/models"
	"github.com/ShouheiNishi/openapi5g/utils/problem"
)

func TestProblemDetails(t *testing.T) {
	pd := problem.NotImplemented()

	goodString := "{Status:500 Title:Unspecified NF failure Cause:UNSPECIFIED_NF_FAILURE Detail:Not implemented}"
	goodVplus := "{Status:500 Title:Unspecified NF failure Cause:UNSPECIFIED_NF_FAILURE Detail:Not implemented}"

	regNil := "\\(nil\\)"
	regPtr := "0x[0-9a-f]{1,16}"
	regNilString := "\\(\\*string\\)" + regNil
	regNonNilString := "\\(\\*string\\)\\(" + regPtr + "\\)"
	regGoString := regexp.MustCompile("models\\.ProblemDetails\\{AccessTokenError:\\(\\*models.AccessTokenErr\\)" + regNil + ", AccessTokenRequest:\\(\\*models.AccessTokenReq\\)" + regNil + ", " +
		"Cause:" + regNonNilString + ", Detail:" + regNonNilString + ", Instance:" + regNilString + ", InvalidParams:\\[\\]models\\.InvalidParam" + regNil + ", " +
		"NrfId:" + regNilString + ", Status:500, SupportedFeatures:" + regNilString + ", Title:" + regNonNilString + ", Type:" + regNilString + ", " +
		"AdditionalProperties:map\\[string\\]interface \\{\\}" + regNil + "\\}")

	assert.Equal(t, goodString, pd.String())
	assert.Regexp(t, regGoString, pd.GoString())
	assert.Equal(t, goodString, fmt.Sprint(pd))
	assert.Equal(t, goodString, fmt.Sprintf("%s", pd))
	assert.Equal(t, goodString, fmt.Sprintf("%v", pd))
	assert.Equal(t, goodVplus, fmt.Sprintf("%+v", pd))
	assert.Regexp(t, regGoString, fmt.Sprintf("%#v", pd))
	assert.Equal(t, "%!x(models.ProblemDetails="+goodString+")", fmt.Sprintf("%x", pd))

	pd = models.ProblemDetails{
		AccessTokenError: &models.AccessTokenErr{
			Error: models.InvalidRequest,
		},
		AccessTokenRequest: &models.AccessTokenReq{
			GrantType:    models.ClientCredentials,
			NfInstanceId: uuid.UUID{},
			Scope:        "TEST",
		},
		Cause:    lo.ToPtr("TEST_CAUSE"),
		Detail:   lo.ToPtr("test detail"),
		Instance: lo.ToPtr("http://localhost/instance"),
		InvalidParams: []models.InvalidParam{
			{
				Param:  "/foo",
				Reason: lo.ToPtr("Test"),
			},
		},
		NrfId:             lo.ToPtr("test nrfid"),
		Status:            http.StatusTeapot,
		SupportedFeatures: lo.ToPtr("TestSupportedFeatures"),
		Title:             lo.ToPtr("test title"),
		Type:              lo.ToPtr("http://localhost/type"),
		AdditionalProperties: map[string]interface{}{
			"test1": "test additional properties",
			"test2": map[string]interface{}{
				"sub1": "sub1",
				"sub2": "sub2",
			},
		},
	}

	goodString = "{Status:418 Title:test title Cause:TEST_CAUSE Detail:test detail AccessTokenError:&{invalid_request <nil> <nil> map[]} " +
		"AccessTokenRequest:&{client_credentials 00000000-0000-0000-0000-000000000000 <nil> <nil> <nil> [] [] [] TEST <nil> <nil> <nil> <nil> [] <nil> [] map[]} " +
		"Instance:http://localhost/instance InvalidParams:[{/foo " +
		fmt.Sprintf("%p", pd.InvalidParams[0].Reason) +
		" map[]}] NrfId:test nrfid SupportedFeatures:TestSupportedFeatures " +
		"Type:http://localhost/type AdditionalProperties:map[test1:test additional properties test2:map[sub1:sub1 sub2:sub2]]}"
	goodVplus = "{Status:418 Title:test title Cause:TEST_CAUSE Detail:test detail AccessTokenError:&{Error:invalid_request " +
		"ErrorDescription:<nil> ErrorUri:<nil> AdditionalProperties:map[]} AccessTokenRequest:&{GrantType:client_credentials " +
		"NfInstanceId:00000000-0000-0000-0000-000000000000 NfType:<nil> RequesterFqdn:<nil> RequesterPlmn:<nil> RequesterPlmnList:[] " +
		"RequesterSnpnList:[] RequesterSnssaiList:[] Scope:TEST TargetNfInstanceId:<nil> TargetNfServiceSetId:<nil> TargetNfSetId:<nil> " +
		"TargetNfType:<nil> TargetNsiList:[] TargetPlmn:<nil> TargetSnssaiList:[] AdditionalProperties:map[]} " +
		"Instance:http://localhost/instance InvalidParams:[{Param:/foo Reason:" +
		fmt.Sprintf("%p", pd.InvalidParams[0].Reason) +
		" AdditionalProperties:map[]}] NrfId:test nrfid " +
		"SupportedFeatures:TestSupportedFeatures Type:http://localhost/type " +
		"AdditionalProperties:map[test1:test additional properties test2:map[sub1:sub1 sub2:sub2]]}"

	type testType models.ProblemDetails
	goodGoString := "models.ProblemDetails" + strings.TrimPrefix(fmt.Sprintf("%#v", testType(pd)), "models_test.testType")

	assert.Equal(t, goodString, pd.String())
	assert.Equal(t, goodGoString, pd.GoString())
	assert.Equal(t, goodString, fmt.Sprint(pd))
	assert.Equal(t, goodString, fmt.Sprintf("%s", pd))
	assert.Equal(t, goodString, fmt.Sprintf("%v", pd))
	assert.Equal(t, goodVplus, fmt.Sprintf("%+v", pd))
	assert.Equal(t, goodGoString, fmt.Sprintf("%#v", pd))
	assert.Equal(t, "%!x(models.ProblemDetails="+goodString+")", fmt.Sprintf("%x", pd))
}
