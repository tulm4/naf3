// Copyright 2024 NSSAAF Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.
//
// This file is auto-generated from dict/nssaa_extension.xml.
// Do not edit manually - regenerate with: make generate-dict
//
// Spec: TS 29.561 Ch.17 (Diameter-based NSSAA), RFC 4072 (Diameter EAP), RFC 6733 (Diameter Base)

package generated

import (
    "bytes"
    "fmt"
    "sync"

    "github.com/fiorix/go-diameter/v4/diam/dict"
)

// nssaaDictionaryXML is the embedded NSSAAF dictionary extension.
const nssaaDictionaryXML = `
    <?xml version="1.0" encoding="UTF-8"?>
    <!--
        NSSAAF Diameter Extension Dictionary
    
        This dictionary extends the RFC 6733 base dictionary with 3GPP-specific
        AVPs and commands for Network Slice Specific Authentication and Authorization.
    
        Spec: TS 29.561 Ch.17 (Diameter-based NSSAA), TS 29.571 §5.4.4.60-61
        RFC: RFC 6733 (Diameter Base), RFC 4072 (Diameter EAP)
    -->
    <diameter>
    
        <!-- ============================================================ -->
        <!-- Diameter EAP Application (AppID=5) - TS 29.561 §17.2.1        -->
        <!-- ============================================================ -->
        <application id="5" type="auth" name="Diameter EAP">
            <!--
                DER/DEA Command - Code 268, Short "DE"
                Spec: RFC 4072, TS 29.561 §17.2.1
    
                The short "DE" allows go-diameter to derive:
                - Request: DER (Diameter-EAP-Request)
                - Answer:  DEA (Diameter-EAP-Answer)
            -->
            <command code="268" short="DE" name="Diameter-EAP">
                <request>
                    <!-- RFC 6733 Base AVPs -->
                    <rule avp="Session-Id"              required="true"  max="1"/>
                    <rule avp="Auth-Application-Id"    required="true"  max="1"/>
                    <rule avp="Auth-Request-Type"      required="true"  max="1"/>
                    <rule avp="Origin-Host"            required="true"  max="1"/>
                    <rule avp="Origin-Realm"           required="true"  max="1"/>
                    <rule avp="Origin-State-Id"        required="false" max="1"/>
                    <rule avp="Destination-Host"       required="false" max="1"/>
                    <rule avp="Destination-Realm"       required="true"  max="1"/>
                    <rule avp="User-Name"              required="false" max="1"/>
                    <rule avp="EAP-Payload"            required="false"/>
                    <rule avp="Multi-Round-Time-Out"   required="false" max="1"/>
                    <rule avp="Auth-Session-State"      required="true"  max="1"/>
                    <!-- TS 29.561 §17: GPSI in Calling-Station-Id or External-Identifier -->
                    <rule avp="Calling-Station-Id"      required="false" max="1"/>
                    <rule avp="External-Identifier"    required="false" max="1"/>
                    <!-- TS 29.571 §5.4.4.60: 3GPP-S-NSSAI for network slice info -->
                    <rule avp="3GPP-S-NSSAI"           required="false"/>
                    <rule avp="NSSAI-Configuration"    required="false" max="1"/>
                    <!-- 3GPP AAA Proxy support -->
                    <rule avp="AAA-Server-Name"        required="false" max="1"/>
                    <!-- RFC 6733 Proxy infrastructure -->
                    <rule avp="Proxy-Info"              required="false"/>
                    <rule avp="Route-Record"           required="false"/>
                </request>
                <answer>
                    <!-- RFC 6733 Base AVPs -->
                    <rule avp="Session-Id"              required="true"  max="1"/>
                    <rule avp="Auth-Application-Id"     required="true"  max="1"/>
                    <rule avp="Auth-Request-Type"       required="true"  max="1"/>
                    <rule avp="Result-Code"              required="true"  max="1"/>
                    <rule avp="Experimental-Result"      required="false" max="1"/>
                    <rule avp="Origin-Host"              required="true"  max="1"/>
                    <rule avp="Origin-Realm"             required="true"  max="1"/>
                    <rule avp="User-Name"               required="false" max="1"/>
                    <rule avp="EAP-Payload"             required="false"/>
                    <rule avp="Multi-Round-Time-Out"    required="false" max="1"/>
                    <rule avp="Auth-Session-State"      required="true"  max="1"/>
                    <rule avp="Origin-State-Id"          required="false" max="1"/>
                    <!-- Error handling -->
                    <rule avp="Error-Message"            required="false" max="1"/>
                    <rule avp="Error-Reporting-Host"    required="false" max="1"/>
                    <rule avp="Failed-AVP"              required="false" max="1"/>
                    <!-- RFC 6733 Redirect -->
                    <rule avp="Redirect-Host"            required="false"/>
                    <rule avp="Redirect-Host-Usage"      required="false" max="1"/>
                    <rule avp="Redirect-Max-Cache-Time"  required="false" max="1"/>
                    <!-- RFC 6733 Proxy infrastructure -->
                    <rule avp="Proxy-Info"               required="false"/>
                </answer>
            </command>
    
            <!-- ============================================================ -->
            <!-- NSSAAF-Specific AVPs (3GPP Vendor ID = 10415)                -->
            <!-- ============================================================ -->
    
            <!--
                3GPP-S-NSSAI AVP (Code 200)
                Encodes S-NSSAI (Single Network Slice Selection Assistance Information)
    
                Format: SST(1 octet) + SD(3 octets, optional)
                Spec: TS 29.571 §5.4.4.60
    
                SST values:
                  0: Standardized SST value (not standardized)
                  1: eMBB (Enhanced Mobile Broadband)
                  2: URLLC (Ultra-Reliable Low-Latency Communications)
                  3: mMTC (Massive Machine Type Communications)
                  4-255: Standardized SST values (defined in TS 23.003)
            -->
            <avp name="3GPP-S-NSSAI" code="200" must="M,V" may-encrypt="Y" vendor-id="10415">
                <data type="OctetString"/>
            </avp>
    
            <!--
                NSSAI-Configuration AVP (Code 3100)
                Contains complete NSSAI configuration for the UE
                Spec: TS 29.571 §5.4.4.61
            -->
            <avp name="NSSAI-Configuration" code="3100" must="M,V" may-encrypt="Y" vendor-id="10415">
                <data type="Grouped">
                    <rule avp="Configured-NSSAI"      required="false"/>
                    <rule avp="Requested-NSSAI"        required="false"/>
                    <rule avp="NSSAI-Configuration-Data" required="false"/>
                    <rule avp="AVP"                    required="false"/>
                </data>
            </avp>
    
            <!--
                Configured-NSSAI AVP (Code 3101)
                Contains the Configured NSSAI for the subscribed S-NSSAI(s)
            -->
            <avp name="Configured-NSSAI" code="3101" must="M,V" may-encrypt="Y" vendor-id="10415">
                <data type="Grouped">
                    <rule avp="3GPP-S-NSSAI"          required="true"/>
                    <rule avp="AVP"                   required="false"/>
                </data>
            </avp>
    
            <!--
                Requested-NSSAI AVP (Code 3102)
                Contains the Requested NSSAI from the UE
            -->
            <avp name="Requested-NSSAI" code="3102" must="M,V" may-encrypt="Y" vendor-id="10415">
                <data type="Grouped">
                    <rule avp="3GPP-S-NSSAI"          required="true"/>
                    <rule avp="AVP"                   required="false"/>
                </data>
            </avp>
    
            <!--
                NSSAI-Configuration-Data AVP (Code 3103)
                Contains per-PLMN NSSAI configuration
            -->
            <avp name="NSSAI-Configuration-Data" code="3103" must="M,V" may-encrypt="Y" vendor-id="10415">
                <data type="Grouped">
                    <rule avp="PLMN-Id"               required="true"  max="1"/>
                    <rule avp="Configured-NSSAI"      required="false"/>
                    <rule avp="AVP"                   required="false"/>
                </data>
            </avp>
    
            <!--
                PLMN-Id AVP (Code 1467)
                Contains the PLMN identifier (MCC + MNC)
                Spec: TS 29.571 §5.4.4.30
            -->
            <avp name="PLMN-Id" code="1467" must="M,V" may-encrypt="N" vendor-id="10415">
                <data type="OctetString"/>
            </avp>
    
            <!--
                AAA-Server-Name AVP (Code 260)
                Contains the AAA server's FQDN for NSSAA routing
                Spec: TS 29.561 §17.2.1
            -->
            <avp name="AAA-Server-Name" code="260" must="M,V" may-encrypt="N" vendor-id="10415">
                <data type="DiameterIdentity"/>
            </avp>
    
            <!--
                NSSAAuthorization-Information AVP (Code 3104)
                Contains the result of slice-specific authorization
            -->
            <avp name="NSSAAuthorization-Information" code="3104" must="M,V" may-encrypt="Y" vendor-id="10415">
                <data type="Grouped">
                    <rule avp="3GPP-S-NSSAI"           required="true"  max="1"/>
                    <rule avp="Authorization-Result"   required="true"  max="1"/>
                    <rule avp="Authorization-Grace-Period" required="false" max="1"/>
                    <rule avp="AVP"                    required="false"/>
                </data>
            </avp>
    
            <!--
                Authorization-Result AVP (Code 3105)
                Indicates the result of slice authorization
                0: SLICE_AUTHORIZED
                1: SLICE_NOT_AUTHORIZED
            -->
            <avp name="Authorization-Result" code="3105" must="M,V" may-encrypt="Y" vendor-id="10415">
                <data type="Enumerated">
                    <item code="0" name="SLICE_AUTHORIZED"/>
                    <item code="1" name="SLICE_NOT_AUTHORIZED"/>
                </data>
            </avp>
    
            <!--
                Authorization-Grace-Period AVP (Code 3106)
                Time in seconds until the authorization result expires
            -->
            <avp name="Authorization-Grace-Period" code="3106" must="M,V" may-encrypt="Y" vendor-id="10415">
                <data type="Unsigned32"/>
            </avp>
    
            <!--
                NSSAAF-Server-Name AVP (Code 3107)
                Contains the NSSAAF's FQDN for routing
            -->
            <avp name="NSSAAF-Server-Name" code="3107" must="M,V" may-encrypt="N" vendor-id="10415">
                <data type="DiameterIdentity"/>
            </avp>
    
            <!--
                Rejected-SNSSAI-List AVP (Code 3108)
                List of rejected S-NSSAIs with reasons
            -->
            <avp name="Rejected-SNSSAI-List" code="3108" must="M,V" may-encrypt="Y" vendor-id="10415">
                <data type="Grouped">
                    <rule avp="3GPP-S-NSSAI"           required="true"  max="1"/>
                    <rule avp="Rejected-SNSSAI-Cause"  required="true"  max="1"/>
                    <rule avp="AVP"                    required="false"/>
                </data>
            </avp>
    
            <!--
                Rejected-SNSSAI-Cause AVP (Code 3109)
                Indicates why an S-NSSAI was rejected
            -->
            <avp name="Rejected-SNSSAI-Cause" code="3109" must="M,V" may-encrypt="Y" vendor-id="10415">
                <data type="Enumerated">
                    <item code="0" name="S-NSSAI_NOT_AVAILABLE"/>
                    <item code="1" name="S-NSSAI_NOT_SUBSCRIBED"/>
                    <item code="2" name="S-NSSAI_CHANGED"/>
                    <item code="3" name="SLICE_AUTH_FAILED"/>
                </data>
            </avp>
    
        </application>
    
        <!-- ============================================================ -->
        <!-- Base Application (AppID=0) - RFC 6733                        -->
        <!-- ============================================================ -->
        <!-- Note: Commands RAR/RAA (258), ASR/ASA (274), STR/STA (275)   -->
        <!-- are already defined in the base dictionary loaded by default.  -->
        <!-- Only NSSAAF-specific AVPs for these commands are added here.  -->
        <!-- ============================================================ -->
    
        <!--
            Calling-Station-Id AVP (Code 31)
            Identifies the location of the calling party (GPSI)
            Spec: RFC 2865, TS 29.561 §17.2.1
        -->
        <avp name="Calling-Station-Id" code="31" must="M" may="P" must-not="V" may-encrypt="Y">
            <data type="UTF8String"/>
        </avp>
    
        <!--
            External-Identifier AVP (Code 606)
            Identifies the external identifier for the subscriber
            Spec: TS 29.571 §5.4.4.5
        -->
        <avp name="External-Identifier" code="606" must="M,V" may-encrypt="N" vendor-id="10415">
            <data type="UTF8String"/>
        </avp>
    
        <!--
            Supported-Features AVP (Code 628)
            Indicates features supported by the client
            Spec: TS 29.571 §5.4.4.57
        -->
        <avp name="Supported-Features" code="628" must="M,V" may-encrypt="N" vendor-id="10415">
            <data type="Grouped">
                <rule avp="Vendor-Id"               required="true"  max="1"/>
                <rule avp="Feature-List-ID"         required="true"  max="1"/>
                <rule avp="Feature-List"            required="true"  max="1"/>
                <rule avp="AVP"                    required="false"/>
            </data>
        </avp>
    
        <!--
            Vendor-Id AVP (Code 266)
            Identifies the vendor (3GPP = 10415)
        -->
        <avp name="Vendor-Id" code="266" must="M" may="P" must-not="V" may-encrypt="-">
            <data type="Unsigned32"/>
        </avp>
    
        <!--
            Feature-List-ID AVP (Code 629)
            Identifies the feature list
        -->
        <avp name="Feature-List-ID" code="629" must="M,V" may-encrypt="N" vendor-id="10415">
            <data type="Unsigned32"/>
        </avp>
    
        <!--
            Feature-List AVP (Code 630)
            Contains the list of supported features
        -->
        <avp name="Feature-List" code="630" must="M,V" may-encrypt="N" vendor-id="10415">
            <data type="Unsigned32"/>
        </avp>
    
    </diameter>
`

var (
    dictOnce sync.Once
    nssaaDict *dict.Parser
)

// Parser returns a dictionary parser with NSSAAF extensions loaded.
// This parser extends dict.Default with NSSAAF-specific applications and AVPs.
func Parser() *dict.Parser {
    dictOnce.Do(func() {
        nssaaDict = dict.Default
        err := nssaaDict.Load(bytes.NewReader([]byte(nssaaDictionaryXML)))
        if err != nil {
            panic(fmt.Sprintf("Cannot load NSSAAF dictionary: %s", err))
        }
    })
    return nssaaDict
}
