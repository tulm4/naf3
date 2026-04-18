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

type pkgEntry struct {
	path string
}

var pkgList map[string]pkgEntry = map[string]pkgEntry{
	// Common Data Types
	"TS29571_CommonData.yaml": {"models"},

	// Other dependency
	"TS29122_CommonData.yaml":    {"northbound/commondata"},
	"TS29122_PfdManagement.yaml": {"pfd/management"},
	// "TS29122_ResourceManagementOfBdt.yaml":   {"northbound/bdt"},
	// "TS29122_CpProvisioning.yaml":            {"northbound/provisioning"},
	// "TS29515_Ngmlc_Location.yaml":            {"gmlc/location"},
	// "TS29522_IPTVConfiguration.yaml":         {"iptv"},
	// "TS29522_ServiceParameter.yaml":          {"service"},
	// "TS29517_Naf_EventExposure.yaml":         {"af/event"},
	// "TS29520_Nnwdaf_AnalyticsInfo.yaml":      {"nwdaf/analytics"},
	// "TS29520_Nnwdaf_EventsSubscription.yaml": {"nwdaf/event"},
	"TS29521_Nbsf_Management.yaml":  {"bsf/management"},
	"TS29522_TrafficInfluence.yaml": {"influence"},
	// "TS29544_Nspaf_SecuredPacket.yaml": {"spaf/secure"},
	"TS29551_Nnef_PFDmanagement.yaml": {"nef/management"},
	// "TS29572_Nlmf_Location.yaml":          {"lmf/location"},
	// "TS32291_Nchf_ConvergedCharging.yaml": {"chf/charging"},

	// free5GC modules
	"TS29518_Namf_Communication.yaml": {"amf/communication"},
	"TS29518_Namf_EventExposure.yaml": {"amf/event"},
	"TS29518_Namf_Location.yaml":      {"amf/location"},
	"TS29518_Namf_MT.yaml":            {"amf/mt"},

	"TS29509_Nausf_SoRProtection.yaml":    {"ausf/sor"},
	"TS29509_Nausf_UEAuthentication.yaml": {"ausf/authentication"},
	"TS29509_Nausf_UPUProtection.yaml":    {"ausf/upu"},

	"TS29510_Nnrf_AccessToken.yaml":   {"nrf/token"},
	"TS29510_Nnrf_Bootstrapping.yaml": {"nrf/bootstrapping"},
	"TS29510_Nnrf_NFDiscovery.yaml":   {"nrf/discovery"},
	"TS29510_Nnrf_NFManagement.yaml":  {"nrf/management"},

	"TS29531_Nnssf_NSSAIAvailability.yaml": {"nssf/availability"},
	"TS29531_Nnssf_NSSelection.yaml":       {"nssf/selection"},

	"TS29507_Npcf_AMPolicyControl.yaml":     {"pcf/AMpolicy"},
	"TS29512_Npcf_SMPolicyControl.yaml":     {"pcf/SMpolicy"},
	"TS29514_Npcf_PolicyAuthorization.yaml": {"pcf/authorization"},
	"TS29523_Npcf_EventExposure.yaml":       {"pcf/event"},
	"TS29525_Npcf_UEPolicyControl.yaml":     {"pcf/UEpolicy"},
	"TS29554_Npcf_BDTPolicyControl.yaml":    {"pcf/BDTpolicy"},

	"TS29502_Nsmf_PDUSession.yaml":    {"smf/session"},
	"TS29508_Nsmf_EventExposure.yaml": {"smf/event"},

	"TS29503_Nudm_EE.yaml":     {"udm/ee"},
	"TS29503_Nudm_MT.yaml":     {"udm/mt"},
	"TS29503_Nudm_NIDDAU.yaml": {"udm/niddau"},
	"TS29503_Nudm_PP.yaml":     {"udm/pp"},
	"TS29503_Nudm_SDM.yaml":    {"udm/sdm"},
	"TS29503_Nudm_UEAU.yaml":   {"udm/ueau"},
	"TS29503_Nudm_UECM.yaml":   {"udm/uecm"},

	"TS29504_Nudr_DR.yaml":           {"udr/dr"},
	"TS29504_Nudr_GroupIDmap.yaml":   {"udr/idmap"},
	"TS29505_Subscription_Data.yaml": {"udr/subscription"},
	"TS29519_Application_Data.yaml":  {"udr/application"},
	"TS29519_Exposure_Data.yaml":     {"udr/exposure"},
	"TS29519_Policy_Data.yaml":       {"udr/policy"},
}
