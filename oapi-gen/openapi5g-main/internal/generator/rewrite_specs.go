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

package generator

import (
	"fmt"
	"maps"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strings"

	"github.com/mohae/deepcopy"
	"github.com/samber/lo"
	"gopkg.in/yaml.v3"

	"github.com/ShouheiNishi/openapi5g/internal/generator/openapi"
)

const nameForModels = "f5gcModels"
const importForModels = "github.com/free5gc/openapi/models"
const commonDataSpec = "TS29571_CommonData.yaml"
const schemasPrefix = "/components/schemas"

func (s *GeneratorState) RewriteSpecs() error {
	if err := s.MoveSchemas(); err != nil {
		return fmt.Errorf("MoveSchemas: %w", err)
	}

	for spec := range pkgList {
		doc := s.Specs[spec]

		refs := map[string]struct{}{}
		if err := walkRewriteSpecs(doc, refs); err != nil {
			return err
		}

		if spec == commonDataSpec {
			doc.Components.Responses["default"].Value.Content = map[string]*openapi.MediaType{
				"application/problem+json": {
					Schema: openapi.Ref[openapi.Schema]{
						Ref: openapi.Reference{
							Path:    commonDataSpec,
							Pointer: schemasPrefix + "/ProblemDetails",
						},
					},
				},
			}
		} else {
			for _, pathItem := range doc.Paths {
				if pathItem.HasRef() {
					continue
				}
				for _, op := range pathItem.Value.Operations() {
					if op.Responses == nil {
						op.Responses = make(map[string]*openapi.Ref[openapi.Response])
					}
					if op.Responses["default"] == nil {
						op.Responses["default"] = &openapi.Ref[openapi.Response]{Value: &openapi.Response{}}
					}
					if op.Responses["default"].Ref.String() != commonDataSpec+"#/components/responses/default" &&
						op.Responses["default"].Value.Content["application/problem+json"] == nil {
						resNew := &openapi.Ref[openapi.Response]{
							Value: lo.ToPtr(*op.Responses["default"].Value),
						}
						if resNew.Value.Content == nil {
							resNew.Value.Content = make(map[string]*openapi.MediaType)
						} else {
							resNew.Value.Content = maps.Clone(resNew.Value.Content)
						}
						resNew.Value.Content["application/problem+json"] = &openapi.MediaType{
							Schema: openapi.Ref[openapi.Schema]{
								Ref: openapi.Reference{
									Path:    commonDataSpec,
									Pointer: schemasPrefix + "/ProblemDetails",
								},
							},
						}
						op.Responses["default"] = resNew
					}
				}
			}
		}

		deps := make([]string, 0, len(refs))
		for r := range refs {
			if r == spec {
				continue
			}
			if _, exist := pkgList[r]; !exist {
				panic(fmt.Sprintf("%s is not defined.", r))
			}
			deps = append(deps, r)
		}
		sort.Strings(deps)

		s.DepsForImport[spec] = deps
	}

	for _, parameterRef := range s.Specs["TS29503_Nudm_SDM.yaml"].Paths["/shared-data"].Value.Get.Parameters {
		parameter := parameterRef.Value
		if parameter.Name == "supportedFeatures" {
			parameter.GoName = "SupportedFeaturesShouldNotBeUsed"
		}
	}

	docCommonData := s.Specs[commonDataSpec]
	schema := docCommonData.Components.Schemas["ProblemDetails"].Value.Properties["status"].Value
	schema.GoTypeSkipOptionalPointer = true

	schema = docCommonData.Components.Schemas["ExtSnssai"].Value
	for i := range schema.AllOf {
		if strings.HasSuffix(schema.AllOf[i].Ref.Pointer, "/Snssai") {
			newSchema := *schema.AllOf[i].Value
			schema.AllOf[i] = openapi.Ref[openapi.Schema]{
				Value: &newSchema,
			}
		}
	}

	schema = docCommonData.Components.Schemas["Snssai"].Value
	schema.GoType = nameForModels + ".Snssai"
	schema.GoTypeImport = &openapi.GoTypeImport{
		Name: nameForModels,
		Path: importForModels,
	}

	schema = docCommonData.Components.Schemas["PlmnId"].Value
	schema.GoType = nameForModels + ".PlmnId"
	schema.GoTypeImport = &openapi.GoTypeImport{
		Name: nameForModels,
		Path: importForModels,
	}

	schema = docCommonData.Components.Schemas["Guami"].Value
	schema.GoType = nameForModels + ".Guami"
	schema.GoTypeImport = &openapi.GoTypeImport{
		Name: nameForModels,
		Path: importForModels,
	}

	for _, t := range []string{"Int32", "Int64", "Uint16", "Uint32", "Uint64"} {
		if schemaRef := docCommonData.Components.Schemas[t]; schemaRef != nil {
			schemaRef.Value.Format = strings.ToLower(t)
			schemaRef.Value.Minimum = nil
			schemaRef.Value.ExclusiveMinimum = false
			schemaRef.Value.Maximum = nil
			schemaRef.Value.ExclusiveMaximum = false
		}
		if schemaRef := docCommonData.Components.Schemas[t+"Rm"]; schemaRef != nil {
			schemaRef.Value.Format = strings.ToLower(t)
			schemaRef.Value.Minimum = nil
			schemaRef.Value.ExclusiveMinimum = false
			schemaRef.Value.Maximum = nil
			schemaRef.Value.ExclusiveMaximum = false
		}
	}
	if schemaRef := docCommonData.Components.Schemas["Uinteger"]; schemaRef != nil {
		schemaRef.Value.Format = "uint"
		schemaRef.Value.Minimum = nil
		schemaRef.Value.ExclusiveMinimum = false
		schemaRef.Value.Maximum = nil
		schemaRef.Value.ExclusiveMaximum = false
	}
	if schemaRef := docCommonData.Components.Schemas["UintegerRm"]; schemaRef != nil {
		schemaRef.Value.Format = "uint"
		schemaRef.Value.Minimum = nil
		schemaRef.Value.ExclusiveMinimum = false
		schemaRef.Value.Maximum = nil
		schemaRef.Value.ExclusiveMaximum = false
	}

	for spec := range pkgList {
		doc := s.Specs[spec]
		for path, pathRef := range doc.Paths {
			if !pathRef.HasRef() {
				for opName, op := range pathRef.Value.Operations() {
					for status, responseRef := range op.Responses {
						s.moveResponseSchema(doc, responseRef, spec, "/paths/"+path+"/"+strings.ToLower(opName)+"/responses/"+status)
					}
				}
			}
		}
		if doc.Components != nil {
			for name, responseRef := range doc.Components.Responses {
				s.moveResponseSchema(doc, responseRef, spec, "/components/responses/"+name)
			}
		}
	}

	return nil
}

func (s *GeneratorState) moveResponseSchema(doc *openapi.Document, responseRef *openapi.Ref[openapi.Response], spec string, pathBase string) {
	if !responseRef.HasRef() {
		for media, mediaType := range responseRef.Value.Content {
			schemaRef := &mediaType.Schema
			if !(schemaRef.HasRef() ||
				(schemaRef.Value.Type != nil && *schemaRef.Value.Type == openapi.SchemaTypeArray && schemaRef.Value.Items.HasRef()) ||
				((schemaRef.Value.Type == nil || *schemaRef.Value.Type == openapi.SchemaTypeObject) &&
					len(schemaRef.Value.Properties) == 0 &&
					schemaRef.Value.AdditionalProperties.SchemaRef != nil && schemaRef.Value.AdditionalProperties.SchemaRef.HasRef())) {
				path := pathBase + "/" + media
				newName := "response-for" + strings.ReplaceAll(path, "/", "-")
				if doc.Components == nil {
					doc.Components = &openapi.Components{}
				}
				if doc.Components.Schemas == nil {
					doc.Components.Schemas = make(map[string]*openapi.Ref[openapi.Schema])
				}
				doc.Components.Schemas[newName] = &openapi.Ref[openapi.Schema]{
					Value: mediaType.Schema.Value,
				}
				schemaRef.Ref = openapi.Reference{
					Path:    spec,
					Pointer: "/components/schemas/" + newName,
				}
			}
		}
	}
}

type schemaInfo struct {
	spec    string
	oldName string
	schema  *openapi.Schema
	refs    map[string]*refInfo
}

func (s *schemaInfo) newName() string {
	return s.refs[s.oldName].newName
}

type refInfo struct {
	isAlias bool
	oldName string
	newName string
	schema  *schemaInfo
	refs    map[openapi.Reference]struct{}
}

func (s *GeneratorState) MoveSchemas() error {
	usedSchemaRefs := make(map[openapi.Reference]struct{})
	for spec := range pkgList {
		if err := walkSchemaRefEnumeration(s.Specs[spec], usedSchemaRefs); err != nil {
			return err
		}
	}

	schemas := make(map[*openapi.Schema]*schemaInfo)
	for spec := range s.Specs {
		if components := s.Specs[spec].Components; components != nil {
			for name, ref := range components.Schemas {
				if !ref.HasRef() {
					info := &schemaInfo{
						spec:    spec,
						oldName: name,
						schema:  ref.Value,
					}
					schemas[ref.Value] = info
					info.refs = map[string]*refInfo{
						name: {
							isAlias: false,
							oldName: name,
							schema:  info,
							refs:    make(map[openapi.Reference]struct{}),
						},
					}
				}
			}
		}
	}

	for ref := range usedSchemaRefs {
		if !strings.HasPrefix(ref.Pointer, schemasPrefix+"/") {
			return fmt.Errorf("invalid ref %s", ref)
		}
		name := strings.TrimPrefix(ref.Pointer, schemasPrefix+"/")
		spec := ref.Path
		doc := s.Specs[spec]
		if doc == nil {
			return fmt.Errorf("spec %s is not exist", spec)
		}
		if doc.Components == nil {
			return fmt.Errorf("no components in spec %s", spec)
		}
		schemaRef := doc.Components.Schemas[name]
		if schemaRef == nil {
			return fmt.Errorf("%s is not exist", ref)
		}
		info := schemas[schemaRef.Value]
		if info == nil {
			return fmt.Errorf("%s has no info", ref)
		}
		if info.refs[name] == nil {
			info.refs[name] = &refInfo{
				isAlias: true,
				oldName: name,
				schema:  info,
				refs:    make(map[openapi.Reference]struct{}),
			}
		}
		info.refs[name].refs[ref] = struct{}{}
	}

	newNames := make(map[string]*refInfo)
	for _, schemaInfo := range schemas {
		if len(schemaInfo.refs) == 1 && len(schemaInfo.refs[schemaInfo.oldName].refs) == 0 {
			continue
		}
		for _, refInfo := range schemaInfo.refs {
			newName := refInfo.oldName
			switch newName {
			// TS 29.122
			case "DateTime",
				"DurationSec",
				"DurationSecRm",
				"ExternalGroupId",
				"InvalidParam",
				"Ipv4Addr",
				"Ipv6Addr",
				"Link",
				"ProblemDetails",
				"Uri":
				if refInfo.schema.spec == "TS29122_CommonData.yaml" {
					newName = "TS29122-" + newName
				}
			case "TrafficDescriptor":
				if refInfo.schema.spec == "TS29122_ResourceManagementOfBdt.yaml" {
					newName = "TS29122-" + newName
				}

			// traffic influence
			case "EventNotification", "TrafficInfluSub":
				if refInfo.schema.spec == "TS29522_TrafficInfluence.yaml" {
					newName = "TS29522-" + newName
				}

			// AUSF
			case "AuthType", "RgAuthCtx":
				if refInfo.schema.spec == "TS29509_Nausf_UEAuthentication.yaml" {
					newName = "ausf-" + newName
				}
			case "SecuredPacket", "SorInfo", "SteeringContainer":
				if refInfo.schema.spec == "TS29509_Nausf_SoRProtection.yaml" {
					newName = "ausf-" + newName
				}
			case "UpuData", "UpuInfo":
				if refInfo.schema.spec == "TS29509_Nausf_UPUProtection.yaml" {
					newName = "ausf-" + newName
				}

			// BSF
			case "ExtProblemDetails":
				if refInfo.schema.spec == "TS29521_Nbsf_Management.yaml" {
					newName = "bsf-" + newName
				}

			// GMLC
			case "CodeWord":
				if refInfo.schema.spec == "TS29515_Ngmlc_Location.yaml" {
					newName = "gmlc-" + newName
				}

			// LMF
			case "TerminationCause":
				if refInfo.schema.spec == "TS29572_Nlmf_Location.yaml" {
					newName = "lmf-" + newName
				}

			// NRF
			case "NFProfile":
				if refInfo.schema.spec == "TS29510_Nnrf_NFDiscovery.yaml" {
					newName = "NFDiscovery-" + newName
				}
				if refInfo.schema.spec == "TS29510_Nnrf_NFManagement.yaml" {
					newName = "NFManagement-" + newName
				}
			case "NFService", "PfdData", "SubscriptionData", "TransportProtocol":
				if refInfo.schema.spec == "TS29510_Nnrf_NFManagement.yaml" {
					newName = "nrf-" + newName
				}

			// PCF
			case "AtsssCapability", "FailureCode", "MulticastAccessControl":
				if refInfo.schema.spec == "TS29512_Npcf_SMPolicyControl.yaml" {
					newName = "pcf-" + newName
				}
			case "AfEvent":
				if refInfo.schema.spec == "TS29514_Npcf_PolicyAuthorization.yaml" {
					newName = "pcf-" + newName
				}
			case "BdtPolicyData", "BdtPolicyDataPatch", "NetworkAreaInfo":
				if refInfo.schema.spec == "TS29554_Npcf_BDTPolicyControl.yaml" {
					newName = "pcf-" + newName
				}
			case "PolicyAssociation",
				"PolicyAssociationReleaseCause",
				"PolicyAssociationRequest",
				"PolicyAssociationUpdateRequest",
				"PolicyUpdate",
				"RequestTrigger",
				"TerminationNotification":
				if refInfo.schema.spec == "TS29507_Npcf_AMPolicyControl.yaml" {
					newName = "AMPolicy-" + newName
				}
				if refInfo.schema.spec == "TS29525_Npcf_UEPolicyControl.yaml" {
					newName = "UEPolicy-" + newName
				}
			case "FlowDescription", "QosMonitoringReport", "QosNotificationControlInfo":
				if refInfo.schema.spec == "TS29512_Npcf_SMPolicyControl.yaml" {
					newName = "SMPolicy-" + newName
				}
				if refInfo.schema.spec == "TS29514_Npcf_PolicyAuthorization.yaml" {
					newName = "PolicyAuthorization-" + newName
				}
			case "AspId":
				if refInfo.schema.spec == "TS29514_Npcf_PolicyAuthorization.yaml" {
					newName = "PolicyAuthorization-" + newName
				}
				if refInfo.schema.spec == "TS29554_Npcf_BDTPolicyControl.yaml" {
					newName = "BDTPolicyControl-" + newName
				}

			// SMF
			case "EpsBearerId", "IpAddress":
				if refInfo.schema.spec == "TS29502_Nsmf_PDUSession.yaml" {
					newName = "smf-" + newName
				}

			// UDM
			case "DataSetName", "DatasetNames", "EcRestrictionDataWb":
				if refInfo.schema.spec == "TS29503_Nudm_SDM.yaml" {
					newName = "udm-" + newName
				}
			case "LocationArea":
				if refInfo.schema.spec == "TS29503_Nudm_PP.yaml" {
					newName = "udm-" + newName
				}
			case "ReferenceId":
				if refInfo.schema.spec == "TS29503_Nudm_EE.yaml" {
					newName = "udm-EE-" + newName
				}
				if refInfo.schema.spec == "TS29503_Nudm_PP.yaml" {
					newName = "udm-PP-" + newName
				}
			}
			if dupInfo := newNames[newName]; dupInfo != nil {
				return fmt.Errorf("%s is duplicate %s->%s:%s, %s->%s:%s", newName,
					dupInfo.oldName, dupInfo.schema.spec, dupInfo.schema.oldName,
					refInfo.oldName, refInfo.schema.spec, refInfo.schema.oldName)
			} else {
				refInfo.newName = newName
				newNames[newName] = refInfo
			}
		}
	}

	refMap := make(map[openapi.Reference]openapi.Reference)
	for newName, refInfo := range newNames {
		for ref := range refInfo.refs {
			refMap[ref] = openapi.Reference{
				Path:    commonDataSpec,
				Pointer: schemasPrefix + "/" + newName,
			}
		}
	}
	for spec := range pkgList {
		if err := walkSchemaRefRemap(s.Specs[spec], refMap); err != nil {
			return err
		}
	}

	newSchemas := make(openapi.ComponentsSchemas)
	for newName, refInfo := range newNames {
		newRef := &openapi.Ref[openapi.Schema]{
			Value: refInfo.schema.schema,
		}
		if refInfo.isAlias {
			newRef.Ref = openapi.Reference{
				Path:    commonDataSpec,
				Pointer: schemasPrefix + "/" + refInfo.schema.newName(),
			}
		} else if refInfo.schema.spec != commonDataSpec {
			origRefStr := "Original definition in " + refInfo.schema.spec + "#" + schemasPrefix + "/" + refInfo.schema.oldName
			if newRef.Value.Description == "" {
				newRef.Value.Description = origRefStr
			} else {
				newRef.Value.Description += " (" + origRefStr + ")"
			}
		}
		newSchemas[newName] = newRef
	}

	for spec := range pkgList {
		doc := s.Specs[spec]
		if doc.Components != nil {
			doc.Components.Schemas = nil
		}
	}
	s.Specs[commonDataSpec].Components.Schemas = newSchemas

	return nil
}

func getSchemaType(schema *openapi.Schema) openapi.SchemaType {
	if schema.Type != nil {
		return *schema.Type
	}
	return ""
}

func fixAnyOfEnum(v *openapi.Schema) error {
	if len(v.AnyOf) == 2 && !v.AnyOf[0].HasRef() && !v.AnyOf[1].HasRef() {
		v0 := v.AnyOf[0].Value
		v1 := v.AnyOf[1].Value
		if getSchemaType(v0) == getSchemaType(v1) && v0.Format == v1.Format &&
			(getSchemaType(v0) == openapi.SchemaTypeInteger || getSchemaType(v1) == openapi.SchemaTypeString) {
			newDescription := v.Description
			if newDescription == "" {
				newDescription = v0.Description
			}
			if newDescription == "" {
				newDescription = v1.Description
			}
			merged := false
			if v0.Enum == nil && len(v1.Enum) > 0 {
				*v = *v1
				v.Description = newDescription
				merged = true
			} else if v1.Enum == nil && len(v0.Enum) > 0 {
				*v = *v0
				v.Description = newDescription
				merged = true
			}
			if merged {
				v.GoTypeSkipOptionalPointer = v0.GoTypeSkipOptionalPointer && v1.GoTypeSkipOptionalPointer
			}
		}
	}
	return nil
}

func fixAnyOfString(v *openapi.Schema) error {
	if len(v.AnyOf) > 0 {
		for _, vRef := range v.AnyOf {
			if vRef.Value.Type == nil || *vRef.Value.Type != openapi.SchemaTypeString {
				return nil
			}
		}
		newDescription := []string{"Merged type of"}
		newSkipOptionalPointer := true
		for _, vRef := range v.AnyOf {
			if !vRef.HasRef() {
				if vRef.Value.Description == "" {
					newDescription = append(newDescription, "  Anonymous string")
				} else {
					newDescription = append(newDescription, "  "+vRef.Value.Description)
				}
			} else {
				if vRef.Value.Description == "" {
					newDescription = append(newDescription, "  string in "+vRef.Ref.String())
				} else {
					newDescription = append(newDescription, "  "+vRef.Value.Description+" in "+vRef.Ref.String())
				}
			}
			if !vRef.Value.GoTypeSkipOptionalPointer {
				if err := fixSkipOptionalPointer(vRef.Value); err != nil {
					return err
				}
				if !vRef.Value.GoTypeSkipOptionalPointer {
					newSkipOptionalPointer = false
				}
			}
		}
		*v = openapi.Schema{
			Type:                      lo.ToPtr(openapi.SchemaTypeString),
			Description:               strings.Join(newDescription, "\n"),
			GoTypeSkipOptionalPointer: newSkipOptionalPointer,
		}
	}
	return nil
}

func fixImplicitArray(v *openapi.Schema) error {
	if getSchemaType(v) == "" && !v.Items.IsZero() {
		v.Type = lo.ToPtr(openapi.SchemaTypeArray)
	}
	return nil
}

func fixEliminateCheckerUnion(v *openapi.Schema) error {
	var newOneOf []openapi.Ref[openapi.Schema]
	for _, ref := range v.OneOf {
		if !(!ref.HasRef() &&
			getSchemaType(ref.Value) == "" &&
			ref.Value.Description == "" &&
			len(ref.Value.Properties) == 0 &&
			ref.Value.OneOf == nil &&
			ref.Value.AnyOf == nil &&
			ref.Value.AllOf == nil) {
			newOneOf = append(newOneOf, ref)
		}
	}
	v.OneOf = newOneOf

	var newAnyOf []openapi.Ref[openapi.Schema]
	for _, ref := range v.AnyOf {
		if !(!ref.HasRef() &&
			getSchemaType(ref.Value) == "" &&
			ref.Value.Description == "" &&
			len(ref.Value.Properties) == 0 &&
			ref.Value.OneOf == nil &&
			ref.Value.AnyOf == nil &&
			ref.Value.AllOf == nil) {
			newAnyOf = append(newAnyOf, ref)
		}
	}
	v.AnyOf = newAnyOf

	var newAllOf []openapi.Ref[openapi.Schema]
	for _, ref := range v.AllOf {
		if !(!ref.HasRef() &&
			getSchemaType(ref.Value) == "" &&
			ref.Value.Description == "" &&
			len(ref.Value.Properties) == 0 &&
			ref.Value.OneOf == nil &&
			ref.Value.AnyOf == nil &&
			ref.Value.AllOf == nil) {
			newAllOf = append(newAllOf, ref)
		}
	}
	v.AllOf = newAllOf

	return nil
}

func fixAdditionalProperties(v *openapi.Schema) error {
	if (getSchemaType(v) == openapi.SchemaTypeObject || getSchemaType(v) == "") && len(v.Properties) > 0 &&
		v.AdditionalProperties.Bool == nil && v.AdditionalProperties.SchemaRef == nil {
		v.AdditionalProperties.Bool = lo.ToPtr(true)
	}
	return nil
}

func maySkipOptionalPointerByMin[T int | int64 | uint64 | float64](v T, exclusive bool) bool {
	if exclusive {
		if v >= T(0) {
			return true
		}
	} else {
		if v > T(0) {
			return true
		}
	}
	return false
}

func maySkipOptionalPointerByMax[T int | int64 | uint64 | float64](v T, exclusive bool) bool {
	if exclusive {
		if v <= T(0) {
			return true
		}
	} else {
		if v < T(0) {
			return true
		}
	}
	return false
}

func fixSkipOptionalPointer(v *openapi.Schema) error {
	skipOptionalPointer := false

	if v.Nullable != nil && *v.Nullable {
		return nil
	}

	switch getSchemaType(v) {
	case openapi.SchemaTypeString:
		// TODO format check
		// Check whether allow empty string
		if v.MinLength > 0 {
			skipOptionalPointer = true
			break
		}
		if r, err := regexp.Compile(v.Pattern); r != nil && err == nil {
			if !r.MatchString("") {
				skipOptionalPointer = true
				break
			}
		}
		if len(v.Enum) != 0 {
			existEmptyMember := false
			for _, m := range v.Enum {
				if m.Kind == yaml.ScalarNode && m.Value == "" {
					existEmptyMember = true
					break
				}
			}
			if !existEmptyMember {
				skipOptionalPointer = true
				break
			}
		}

	case openapi.SchemaTypeArray:
		if v.MinItems != nil && *v.MinItems > 0 {
			skipOptionalPointer = true
			break
		}

	case openapi.SchemaTypeInteger, openapi.SchemaTypeNumber:
		switch m := v.Minimum.(type) {
		case nil:
		case int:
			if maySkipOptionalPointerByMin(m, v.ExclusiveMinimum) {
				skipOptionalPointer = true
			}
		case int64:
			if maySkipOptionalPointerByMin(m, v.ExclusiveMinimum) {
				skipOptionalPointer = true
			}
		case uint64:
			if maySkipOptionalPointerByMin(m, v.ExclusiveMinimum) {
				skipOptionalPointer = true
			}
		case float64:
			if maySkipOptionalPointerByMin(m, v.ExclusiveMinimum) {
				skipOptionalPointer = true
			}
		default:
			return fmt.Errorf("unknown minimum type %T", m)
		}
		switch m := v.Maximum.(type) {
		case nil:
		case int:
			if maySkipOptionalPointerByMax(m, v.ExclusiveMaximum) {
				skipOptionalPointer = true
			}
		case int64:
			if maySkipOptionalPointerByMax(m, v.ExclusiveMaximum) {
				skipOptionalPointer = true
			}
		case uint64:
			if maySkipOptionalPointerByMax(m, v.ExclusiveMaximum) {
				skipOptionalPointer = true
			}
		case float64:
			if maySkipOptionalPointerByMax(m, v.ExclusiveMaximum) {
				skipOptionalPointer = true
			}
		default:
			return fmt.Errorf("unknown maximum type %T", m)
		}
	}

	if skipOptionalPointer {
		v.GoTypeSkipOptionalPointer = true
	}

	return nil
}

func fixNullable(v *openapi.Schema) error {
	nullValueRef := "TS29571_CommonData.yaml#/components/schemas/NullValue"

	if len(v.AnyOf) == 2 {
		if v.AnyOf[0].Ref.String() == nullValueRef {
			*v = *(deepcopy.Copy(v.AnyOf[1].Value).(*openapi.Schema))
			// kin-openapi don`t allow this
			// v.Nullable = true
			v.Nullable = nil
		} else if v.AnyOf[1].Ref.String() == nullValueRef {
			*v = *(deepcopy.Copy(v.AnyOf[0].Value).(*openapi.Schema))
			// kin-openapi don`t allow this
			// v.Nullable = true
			v.Nullable = nil
		}
	}
	return nil
}

func getRangeForGeneratedType(v *openapi.Schema) (minValue *big.Int, maxValue *big.Int) {
	switch v.Format {
	case "int64":
		return big.NewInt(math.MinInt64), big.NewInt(math.MaxInt64)
	case "int32":
		return big.NewInt(math.MinInt32), big.NewInt(math.MaxInt32)
	case "int16":
		return big.NewInt(math.MinInt16), big.NewInt(math.MaxInt16)
	case "int8":
		return big.NewInt(math.MinInt8), big.NewInt(math.MaxInt8)
	case "int":
		// support for 32bit arch
		return big.NewInt(math.MinInt32), big.NewInt(math.MaxInt32)
	case "uint64":
		return big.NewInt(0), new(big.Int).SetUint64(math.MaxUint64)
	case "uint32":
		return big.NewInt(0), big.NewInt(math.MaxUint32)
	case "uint16":
		return big.NewInt(0), big.NewInt(math.MaxUint16)
	case "uint8":
		return big.NewInt(0), big.NewInt(math.MaxUint8)
	case "uint":
		// support for 32bit arch
		return big.NewInt(0), big.NewInt(math.MaxUint32)
	default:
		// use int type
		// support for 32bit arch
		return big.NewInt(math.MinInt32), big.NewInt(math.MaxInt32)
	}
}

func isFitRange(v *openapi.Schema, min *big.Int, max *big.Int) bool {
	minValue, maxValue := getRangeForGeneratedType(v)
	if min != nil {
		if min.Cmp(minValue) < 0 || min.Cmp(maxValue) > 0 {
			return false
		}
	}
	if max != nil {
		if max.Cmp(minValue) < 0 || max.Cmp(maxValue) > 0 {
			return false
		}
	}
	return true
}

func dumpAndPanicSchema(v *openapi.Schema) {
	panic(fmt.Sprintf("%+v %+v %+v", *v, v.Minimum, v.Maximum))
}

var bigOne = big.NewInt(1)

func setupMinMax(in any, exclusive bool, max bool) (r *big.Int, err error) {
	var minMax string
	if max {
		minMax = "max"
	} else {
		minMax = "min"
	}
	switch in := in.(type) {
	case nil:
		return nil, nil
	case int:
		r = big.NewInt(int64(in))
	case int64:
		r = big.NewInt(in)
	case uint64:
		r = new(big.Int).SetUint64(in)
	case float64:
		if i, a := new(big.Float).SetFloat64(in).Int(nil); a != big.Exact {
			return nil, fmt.Errorf("%s value is not integer", minMax)
		} else {
			r = i
		}
	default:
		return nil, fmt.Errorf("unknown %s type %T", minMax, in)
	}

	if exclusive {
		if max {
			r = r.Sub(r, bigOne)
		} else {
			r = r.Add(r, bigOne)
		}
	}

	return r, nil
}

func fixIntegerFormat(v *openapi.Schema) error {
	if v.Type == nil || *v.Type != openapi.SchemaTypeInteger {
		return nil
	}

	var min, max *big.Int
	if m, err := setupMinMax(v.Minimum, v.ExclusiveMinimum, false); err != nil {
		return err
	} else {
		min = m
	}
	if m, err := setupMinMax(v.Maximum, v.ExclusiveMaximum, true); err != nil {
		return err
	} else {
		max = m
	}

	// Check whether min/max value fit to generated type
	if isFitRange(v, min, max) {
		return nil
	}

	v.Format = "" // assume int type
	if isFitRange(v, min, max) {
		return nil
	}
	v.Format = "int64"
	if isFitRange(v, min, max) {
		return nil
	}
	v.Format = "uint64"

	// Check whether min/max value fit to generated type
	if !isFitRange(v, min, max) {
		dumpAndPanicSchema(v)
	}

	return nil
}
