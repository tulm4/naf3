// Copyright 2024 NSSAAF Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license.
//
// This file is auto-generated from dict/nssaa_extension.xml.
// Do not edit manually - regenerate with: make generate-dict
//
// Spec: TS 29.561 Ch.17 (Diameter-based NSSAA), RFC 4072 (Diameter EAP), RFC 6733 (Diameter Base)

package generated

// Diameter application IDs.
const (
    AppIDDiameter_EAP uint32 = 5
)

// Diameter command codes.
const (
    CmdDiameter_EAP uint32 = 268
)

// Short Command Names (go-diameter convention: append R/A for Request/Answer)
const (
    ShortDER = "DER"
    ShortDEA = "DEA"
)

// Diameter AVP codes (non-vendor-specific AVPs).
const (
    AVPCalling_Station_Id uint32 = 31
    AVPVendor_Id uint32 = 266
)

// Vendor IDs.
const (
    VendorID3GPP uint32 = 10415
)
