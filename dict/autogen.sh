#!/bin/bash
#
# NSSAAF Dictionary Code Generator
#
# This script generates Go code from the XML dictionary extension files
# using the same approach as go-diameter's autogen.sh.
#
# Generated files:
#   - nssaa_codes.go : AVP codes, command codes, and application IDs
#   - nssaa_dict.go  : Embedded dictionary XML and parser setup
#
# Usage:
#   ./autogen.sh [output_dir]
#
# The output_dir defaults to ../internal/diameter/generated relative to
# the dict directory.
#
# Spec: TS 29.561 Ch.17, RFC 6733, RFC 4072

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DICT_FILE="${SCRIPT_DIR}/nssaa_extension.xml"
OUTPUT_DIR="${1:-$(cd "${SCRIPT_DIR}/.." && pwd)/internal/diameter/generated}"

# Verify dictionary file exists
if [[ ! -f "${DICT_FILE}" ]]; then
    echo "ERROR: Dictionary file not found: ${DICT_FILE}" >&2
    exit 1
fi

echo "=== NSSAAF Dictionary Code Generator ==="
echo "Dictionary: ${DICT_FILE}"
echo "Output:     ${OUTPUT_DIR}"

# Create output directory if needed
mkdir -p "${OUTPUT_DIR}"

# Determine sed command (gsed on macOS)
os="$(uname -s)"
if [[ "$os" = "Darwin" ]]; then
    SED="gsed"
    command -v gsed >/dev/null 2>&1 || {
        echo "ERROR: gsed required on macOS. Install with: brew install gnu-sed" >&2
        exit 1
    }
else
    SED="sed"
fi

#
# Generate nssaa_codes.go
#
CODECS_FILE="${OUTPUT_DIR}/nssaa_codes.go"

echo "Generating ${CODECS_FILE}..."

# Write header
cat > "${CODECS_FILE}" << 'GOEOF'
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
GOEOF

# Helper function to convert name to Go identifier
# Replaces dashes and spaces with underscores
name_to_go_id() {
    echo "$1" | "${SED}" -e 's/-/_/g' -e 's/ /_/g'
}

# Generate application IDs from the dictionary
grep -E '<application id=' "${DICT_FILE}" | while IFS= read -r line; do
    id=$(echo "${line}" | grep -oP 'id="\K[0-9]+')
    name=$(echo "${line}" | grep -oP 'name="\K[^"]*')
    go_name=$(name_to_go_id "${name}")
    echo "    AppID${go_name} uint32 = ${id}" >> "${CODECS_FILE}"
done

# Write command codes section
cat >> "${CODECS_FILE}" << 'GOEOF'
)

// Diameter command codes.
const (
GOEOF

# Generate command codes
grep -E '<command code=' "${DICT_FILE}" | while IFS= read -r line; do
    code=$(echo "${line}" | grep -oP 'code="\K[0-9]+')
    name=$(echo "${line}" | grep -oP 'name="\K[^"]*')
    go_name=$(name_to_go_id "${name}")
    echo "    Cmd${go_name} uint32 = ${code}" >> "${CODECS_FILE}"
done

# Write short command names section
cat >> "${CODECS_FILE}" << 'GOEOF'
)

// Short Command Names (go-diameter convention: append R/A for Request/Answer)
const (
GOEOF

# Generate short command names
grep -E '<command code=' "${DICT_FILE}" | while IFS= read -r line; do
    short=$(echo "${line}" | grep -oP 'short="\K[^"]*')
    echo "    Short${short}R = \"${short}R\"" >> "${CODECS_FILE}"
    echo "    Short${short}A = \"${short}A\"" >> "${CODECS_FILE}"
done

# Write AVP codes section
cat >> "${CODECS_FILE}" << 'GOEOF'
)

// Diameter AVP codes (non-vendor-specific AVPs).
const (
GOEOF

# Generate AVP codes - only non-vendor-specific ones
grep -E '<avp name=' "${DICT_FILE}" | grep -v 'vendor-id=' | while IFS= read -r line; do
    code=$(echo "${line}" | grep -oP 'code="\K[0-9]+')
    name=$(echo "${line}" | grep -oP 'name="\K[^"]*')
    go_name=$(name_to_go_id "${name}")
    echo "    AVP${go_name} uint32 = ${code}" >> "${CODECS_FILE}"
done

# Write vendor IDs section
cat >> "${CODECS_FILE}" << 'GOEOF'
)

// Vendor IDs.
const (
    VendorID3GPP uint32 = 10415
)
GOEOF

#
# Generate nssaa_dict.go
#
DICTGO_FILE="${OUTPUT_DIR}/nssaa_dict.go"

echo "Generating ${DICTGO_FILE}..."

# Write header
cat > "${DICTGO_FILE}" << 'GOEOF'
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
GOEOF

# Append the dictionary XML content with proper indentation
cat "${DICT_FILE}" | "${SED}" 's/^/    /' >> "${DICTGO_FILE}"

# Write the rest of the file
cat >> "${DICTGO_FILE}" << 'GOEOF'
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
GOEOF

echo ""
echo "=== Generated Files ==="
echo "  - ${CODECS_FILE}"
echo "  - ${DICTGO_FILE}"
echo ""
echo "Done!"
