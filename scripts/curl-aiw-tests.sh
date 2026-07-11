#!/usr/bin/env bash
#
# scripts/curl-aiw-tests.sh
#
# AIW Conformance Test Suite — Shell Script Runner
#
# Tests the Nnssaaf_Aiw service-based interface using curl.
# Supports both auth-disabled and real OAuth2 modes.
#
# Usage:
#   ./curl-aiw-tests.sh                    # Run all tests
#   ./curl-aiw-tests.sh --auth-disabled   # Use auth-disabled mode
#   ./curl-aiw-tests.sh --auth-oauth2     # Use real OAuth2 (requires NRF mock)
#   ./curl-aiw-tests.sh --scenario create  # Run only create scenarios
#   ./curl-aiw-tests.sh --verbose          # Show curl output
#
# Environment Variables:
#   NAF3_AUTH_DISABLED    Set to 1 to disable auth (default: 1)
#   NAF3_HTTP_GATEWAY_URL HTTP Gateway URL (default: https://localhost:8443)
#   NAF3_NRF_MOCK_URL     NRF Mock URL (default: http://localhost:8082)
#   E2E_TLS_DIR           TLS certificate directory (default: /tmp/e2e-tls)
#   VERBOSE               Set to 1 for curl output
#
# Spec References:
#   TS 29.526 §7.3.2 — Nnssaaf_Aiw Create
#   TS 29.526 §7.3.3 — Nnssaaf_Aiw Query
#   TS 29.526 §7.3.4 — Nnssaaf_Aiw Confirm
#   TS 29.526 §7.3.5 — Nnssaaf_Aiw Delete
#   TS 29.571 — Common Data Types

set -euo pipefail

# ─── Configuration ────────────────────────────────────────────────────────────

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Defaults
AUTH_DISABLED="${NAF3_AUTH_DISABLED:-1}"
HTTP_GATEWAY_URL="${NAF3_HTTP_GATEWAY_URL:-https://localhost:8443}"
NRF_MOCK_URL="${NAF3_NRF_MOCK_URL:-http://localhost:8082}"
E2E_TLS_DIR="${E2E_TLS_DIR:-/tmp/e2e-tls}"
VERBOSE="${VERBOSE:-0}"
SCENARIO_FILTER="${SCENARIO_FILTER:-}"

# Auth token (set by get_auth_token)
AUTH_TOKEN=""

# Test results
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_SKIPPED=0

# ─── Helpers ──────────────────────────────────────────────────────────────────

usage() {
    cat <<EOF
AIW Conformance Test Suite

Usage: $0 [OPTIONS]

Options:
  --auth-disabled    Use auth-disabled mode (default)
  --auth-oauth2     Use real OAuth2 (requires NRF mock)
  --scenario NAME   Run only scenarios matching NAME (create, query, delete, error)
  --verbose         Show curl output
  --help            Show this help

Environment Variables:
  NAF3_AUTH_DISABLED    Set to 0 to enable auth
  NAF3_HTTP_GATEWAY_URL  HTTP Gateway URL
  NAF3_NRF_MOCK_URL     NRF Mock URL
  E2E_TLS_DIR           TLS certificate directory
EOF
}

log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_pass() {
    echo -e "${GREEN}[PASS]${NC} $*"
    ((TESTS_PASSED++))
}

log_fail() {
    echo -e "${RED}[FAIL]${NC} $*"
    ((TESTS_FAILED++))
}

log_skip() {
    echo -e "${YELLOW}[SKIP]${NC} $*"
    ((TESTS_SKIPPED++))
}

log_section() {
    echo ""
    echo -e "${CYAN}════════════════════════════════════════════════════════════════${NC}"
    echo -e "${CYAN}  $*${NC}"
    echo -e "${CYAN}════════════════════════════════════════════════════════════════${NC}"
}

# get_auth_token fetches an OAuth2 token from the NRF mock.
get_auth_token() {
    if [[ "$AUTH_DISABLED" == "1" ]]; then
        return 0
    fi

    log_info "Fetching OAuth2 token from NRF mock..."

    local token_response
    token_response=$(curl -sf -X POST \
        "${NRF_MOCK_URL}/oauth2/token" \
        -H "Content-Type: application/x-www-form-urlencoded" \
        -d "grant_type=client_credentials&scope=nnssaaf_aiw" \
        2>/dev/null) || {
        log_fail "Failed to get OAuth2 token"
        return 1
    }

    AUTH_TOKEN=$(echo "$token_response" | jq -r '.access_token // empty')
    if [[ -z "$AUTH_TOKEN" ]]; then
        log_fail "Invalid OAuth2 token response"
        return 1
    fi

    log_info "OAuth2 token obtained successfully"
}

# build_curl_args returns curl args with auth headers.
build_curl_args() {
    local -a args=(
        -sk
        -X "$1"
        "${HTTP_GATEWAY_URL}$2"
        -H "Content-Type: application/json"
        -H "Accept: application/json"
        -H "X-Request-ID: test-$(date +%s)-$$"
    )

    if [[ "$AUTH_DISABLED" != "1" ]] && [[ -n "$AUTH_TOKEN" ]]; then
        args+=(-H "Authorization: Bearer ${AUTH_TOKEN}")
    fi

    if [[ "$VERBOSE" == "1" ]]; then
        echo "${args[@]}"
    else
        # Silent but show errors
        echo "${args[@]}" 2>/dev/null
    fi
}

# run_test executes a single test scenario.
# Usage: run_test <id> <name> <expected_status> <method> <path> [body]
run_test() {
    local id="$1"
    local name="$2"
    local expected_status="$3"
    local method="$4"
    local path="$5"
    local body="${6:-}"

    # Apply scenario filter
    if [[ -n "$SCENARIO_FILTER" ]]; then
        local scenario_type="${id%%-*}"
        if [[ "$scenario_type" != "$SCENARIO_FILTER" ]]; then
            return 0
        fi
    fi

    log_info "Running: $id - $name"

    # Build curl command
    local -a curl_args=(
        -sk
        -w "\n%{http_code}"
    )

    case "$method" in
        GET|DELETE)
            curl_args+=(-X "$method" "${HTTP_GATEWAY_URL}${path}")
            ;;
        POST|PUT)
            curl_args+=(-X "$method" "${HTTP_GATEWAY_URL}${path}" -d "$body")
            ;;
    esac

    curl_args+=(
        -H "Content-Type: application/json"
        -H "Accept: application/json"
        -H "X-Request-ID: test-${id}-$$"
    )

    if [[ "$AUTH_DISABLED" != "1" ]] && [[ -n "$AUTH_TOKEN" ]]; then
        curl_args+=(-H "Authorization: Bearer ${AUTH_TOKEN}")
    fi

    # Execute curl
    local response
    local curl_exit_code=0

    if [[ "$VERBOSE" == "1" ]]; then
        echo "  curl ${curl_args[*]}" >&2
    fi

    response=$(curl "${curl_args[@]}" 2>/dev/null) || curl_exit_code=$?

    if [[ $curl_exit_code -ne 0 ]]; then
        log_fail "$id - $name (curl failed: exit $curl_exit_code)"
        return 1
    fi

    # Extract status code (last line)
    local status_code
    status_code=$(echo "$response" | tail -n1)
    local body=$(echo "$response" | sed '$d')

    # Check status code
    if [[ "$status_code" == "$expected_status" ]]; then
        log_pass "$id - $name (expected $expected_status, got $status_code)"
    else
        log_fail "$id - $name (expected $expected_status, got $status_code)"
        if [[ "$VERBOSE" == "1" ]] && [[ -n "$body" ]]; then
            echo "  Response: $body" >&2
        fi
    fi
}

# print_summary prints the test summary.
print_summary() {
    local total=$((TESTS_PASSED + TESTS_FAILED + TESTS_SKIPPED))
    echo ""
    echo "════════════════════════════════════════════════════════════════"
    echo "  TEST SUMMARY"
    echo "════════════════════════════════════════════════════════════════"
    echo "  Total:  $total"
    echo -e "  ${GREEN}Passed:${NC}  $TESTS_PASSED"
    echo -e "  ${RED}Failed:${NC}  $TESTS_FAILED"
    echo -e "  ${YELLOW}Skipped:${NC} $TESTS_SKIPPED"
    echo "════════════════════════════════════════════════════════════════"

    if [[ $TESTS_FAILED -gt 0 ]]; then
        exit 1
    fi
}

# ─── Parse Arguments ──────────────────────────────────────────────────────────

while [[ $# -gt 0 ]]; do
    case "$1" in
        --auth-disabled)
            AUTH_DISABLED=1
            shift
            ;;
        --auth-oauth2)
            AUTH_DISABLED=0
            shift
            ;;
        --scenario)
            SCENARIO_FILTER="$2"
            shift 2
            ;;
        --verbose)
            VERBOSE=1
            shift
            ;;
        --help|-h)
            usage
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            usage
            exit 1
            ;;
    esac
done

# ─── Pre-flight Checks ────────────────────────────────────────────────────────

log_info "AIW Conformance Test Suite"
log_info "HTTP Gateway: $HTTP_GATEWAY_URL"
log_info "Auth Mode: $([ "$AUTH_DISABLED" == "1" ] && echo "Disabled" || echo "OAuth2")"
log_info ""

# Check TLS certificates exist
if [[ ! -d "$E2E_TLS_DIR" ]]; then
    log_info "TLS directory not found at $E2E_TLS_DIR, generating..."
    mkdir -p "$E2E_TLS_DIR"
fi

if [[ ! -f "${E2E_TLS_DIR}/server.crt" ]]; then
    log_info "Generating self-signed TLS certificate..."
    openssl req -x509 -newkey rsa:4096 -nodes -keyout "${E2E_TLS_DIR}/server.key" \
        -out "${E2E_TLS_DIR}/server.crt" -days 365 \
        -subj "/CN=localhost/O=NSSAAF/C=US" 2>/dev/null || true
fi

# Get auth token if using OAuth2
if [[ "$AUTH_DISABLED" != "1" ]]; then
    get_auth_token || {
        log_fail "Cannot proceed without OAuth2 token"
        exit 1
    }
fi

# ─── Test Scenarios ───────────────────────────────────────────────────────────

# Positive Create Scenarios
run_create_positive() {
    log_section "1. CREATE — Positive Scenarios"

    # Create-01: Basic Create
    run_test "create-01" "Basic Create" "201" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "supi": "imsi-123456789012345",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012345",
                    "end": "imsi-123456789012345"
                }]
            }
        }
    }'

    # Create-02: With SupiRange
    run_test "create-02" "With SupiRange" "201" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678902",
        "supi": "imsi-123456789012346",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012000",
                    "end": "imsi-123456789012999"
                }]
            }
        }
    }'

    # Create-03: With ValidNotifUri
    run_test "create-03" "With ValidNotifUri" "201" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678903",
        "supi": "imsi-123456789012347",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012347",
                    "end": "imsi-123456789012347"
                }]
            },
            "validNotifUri": ["https://example.com/nssaa/callback"]
        }
    }'

    # Create-04: With Snssai
    run_test "create-04" "With Snssai" "201" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678904",
        "supi": "imsi-123456789012348",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012348",
                    "end": "imsi-123456789012348"
                }]
            },
            "nssai": {
                "nssai": [{
                    "sst": 1,
                    "sd": "000001"
                }]
            }
        }
    }'

    # Create-05: With ExemptionInd
    run_test "create-05" "With ExemptionInd" "201" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678905",
        "supi": "imsi-123456789012349",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012349",
                    "end": "imsi-123456789012349"
                }]
            },
            "exemptionInd": true
        }
    }'
}

# Negative Create Scenarios
run_create_negative() {
    log_section "2. CREATE — Negative Scenarios"

    # Create-10: Missing Required Gpsi
    run_test "create-10" "Missing Required Gpsi" "400" "POST" "/nnssaaf/v1/aiw" '{
        "supi": "imsi-123456789012345",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012345",
                    "end": "imsi-123456789012345"
                }]
            }
        }
    }'

    # Create-11: Invalid Gpsi Format
    run_test "create-11" "Invalid Gpsi Format" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "invalid-gpsi",
        "supi": "imsi-123456789012345",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012345",
                    "end": "imsi-123456789012345"
                }]
            }
        }
    }'

    # Create-12: Invalid Snssai
    run_test "create-12" "Invalid Snssai" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "supi": "imsi-123456789012345",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012345",
                    "end": "imsi-123456789012345"
                }]
            },
            "nssai": {
                "nssai": [{
                    "sst": 256,
                    "sd": "000001"
                }]
            }
        }
    }'

    # Create-13: Missing Supi
    run_test "create-13" "Missing Supi" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012345",
                    "end": "imsi-123456789012345"
                }]
            }
        }
    }'

    # Create-14: Invalid Supi Format
    run_test "create-14" "Invalid Supi Format" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "supi": "invalid-supi",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012345",
                    "end": "imsi-123456789012345"
                }]
            }
        }
    }'

    # Create-15: Missing NssaaInfo
    run_test "create-15" "Missing NssaaInfo" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "supi": "imsi-123456789012345"
    }'

    # Create-16: Invalid NssaaInfo
    run_test "create-16" "Invalid NssaaInfo" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "supi": "imsi-123456789012345",
        "nssaaInfo": {}
    }'

    # Create-17: Missing SupiRange
    run_test "create-17" "Missing SupiRange" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "supi": "imsi-123456789012345",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"]
        }
    }'

    # Create-18: Invalid SupiRange
    run_test "create-18" "Invalid SupiRange" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "supi": "imsi-123456789012345",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "",
                    "end": ""
                }]
            }
        }
    }'

    # Create-19: Invalid AuthSchemes
    run_test "create-19" "Invalid AuthSchemes" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "supi": "imsi-123456789012345",
        "nssaaInfo": {
            "authSchemes": ["INVALID_SCHEME"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012345",
                    "end": "imsi-123456789012345"
                }]
            }
        }
    }'

    # Create-20: Invalid NotifUri
    run_test "create-20" "Invalid NotifUri" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "supi": "imsi-123456789012345",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012345",
                    "end": "imsi-123456789012345"
                }]
            },
            "validNotifUri": ["not-a-valid-url"]
        }
    }'

    # Create-21: Invalid NotifMethod
    run_test "create-21" "Invalid NotifMethod" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "supi": "imsi-123456789012345",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012345",
                    "end": "imsi-123456789012345"
                }]
            }
        }
    }'

    # Create-22: Invalid ExemptionInd
    run_test "create-22" "Invalid ExemptionInd" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "supi": "imsi-123456789012345",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012345",
                    "end": "imsi-123456789012345"
                }]
            },
            "exemptionInd": "not-a-boolean"
        }
    }'

    # Create-23: Missing AuthSchemes
    run_test "create-23" "Missing AuthSchemes" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "supi": "imsi-123456789012345",
        "nssaaInfo": {
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012345",
                    "end": "imsi-123456789012345"
                }]
            }
        }
    }'

    # Create-24: Invalid SupiRangeFormat
    run_test "create-24" "Invalid SupiRangeFormat" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "supi": "imsi-123456789012345",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "123456789012345",
                    "end": "imsi-123456789012345"
                }]
            }
        }
    }'

    # Create-25: Invalid SnssaiSd
    run_test "create-25" "Invalid SnssaiSd" "400" "POST" "/nnssaaf/v1/aiw" '{
        "gpsi": "msisdn-12345678901",
        "supi": "imsi-123456789012345",
        "nssaaInfo": {
            "authSchemes": ["EAP_TLS"],
            "supiRange": {
                "supiRanges": [{
                    "start": "imsi-123456789012345",
                    "end": "imsi-123456789012345"
                }]
            },
            "nssai": {
                "nssai": [{
                    "sst": 1,
                    "sd": "12345"
                }]
            }
        }
    }'
}

# Query and Confirm Scenarios
run_query_confirm() {
    log_section "3. QUERY & CONFIRM — Scenarios"

    # Query-01: Query By Gpsi
    run_test "query-01" "Query By Gpsi" "200" "GET" "/nnssaaf/v1/aiw?gpsi=msisdn-12345678901"

    # Query-02: Query By Supi
    run_test "query-02" "Query By Supi" "200" "GET" "/nnssaaf/v1/aiw?supi=imsi-123456789012345"

    # Query-03: Query Not Found
    run_test "query-03" "Query Not Found" "404" "GET" "/nnssaaf/v1/aiw?gpsi=msisdn-99999999999"

    # Query-04: Confirm Success (depends on having a valid session)
    run_test "query-04" "Confirm Success" "200" "POST" "/nnssaaf/v1/aiw/msisdn-12345678901/confirm" '{
        "nssaaResult": "AUTHENTICATION_SUCCESS"
    }'

    # Query-05: Confirm Not Found
    run_test "query-05" "Confirm Not Found" "404" "POST" "/nnssaaf/v1/aiw/msisdn-99999999999/confirm" '{
        "nssaaResult": "AUTHENTICATION_SUCCESS"
    }'

    # Query-06: Query All
    run_test "query-06" "Query All" "200" "GET" "/nnssaaf/v1/aiw"
}

# Delete Scenarios
run_delete() {
    log_section "4. DELETE — Scenarios"

    # Delete-01: Delete By Gpsi
    run_test "delete-01" "Delete By Gpsi" "204" "DELETE" "/nnssaaf/v1/aiw/msisdn-12345678901"

    # Delete-02: Delete Not Found
    run_test "delete-02" "Delete Not Found" "404" "DELETE" "/nnssaaf/v1/aiw/msisdn-99999999999"

    # Delete-03: Delete All
    run_test "delete-03" "Delete All" "204" "DELETE" "/nnssaaf/v1/aiw"
}

# Error Handling Scenarios
run_error_handling() {
    log_section "5. ERROR HANDLING — Scenarios"

    # Error-01: Invalid JSON
    run_test "error-01" "Invalid Json" "400" "POST" "/nnssaaf/v1/aiw" '{invalid json}'

    # Error-02: Invalid Content-Type
    local -a curl_args=(
        -sk -X POST
        "${HTTP_GATEWAY_URL}/nnssaaf/v1/aiw"
        -H "Content-Type: text/plain"
        -H "Accept: application/json"
        -H "X-Request-ID: test-error-02-$$"
    )
    if [[ "$AUTH_DISABLED" != "1" ]] && [[ -n "$AUTH_TOKEN" ]]; then
        curl_args+=(-H "Authorization: Bearer ${AUTH_TOKEN}")
    fi
    local response
    response=$(curl "${curl_args[@]}" -d '{}' 2>/dev/null) || true
    local status_code
    status_code=$(echo "$response" | tail -n1)
    if [[ "$status_code" == "415" ]]; then
        log_pass "error-02 - Invalid ContentType (expected 415, got $status_code)"
    else
        log_fail "error-02 - Invalid ContentType (expected 415, got $status_code)"
    fi

    # Error-03: Missing Required Header (X-Request-ID is required)
    curl_args=(
        -sk -X POST
        "${HTTP_GATEWAY_URL}/nnssaaf/v1/aiw"
        -H "Content-Type: application/json"
        -H "Accept: application/json"
    )
    if [[ "$AUTH_DISABLED" != "1" ]] && [[ -n "$AUTH_TOKEN" ]]; then
        curl_args+=(-H "Authorization: Bearer ${AUTH_TOKEN}")
    fi
    response=$(curl "${curl_args[@]}" -d '{}' 2>/dev/null) || true
    status_code=$(echo "$response" | tail -n1)
    if [[ "$status_code" == "400" ]]; then
        log_pass "error-03 - Missing X-Request-ID (expected 400, got $status_code)"
    else
        log_fail "error-03 - Missing X-Request-ID (expected 400, got $status_code)"
    fi

    # Error-04: Invalid Accept Header
    curl_args=(
        -sk -X GET
        "${HTTP_GATEWAY_URL}/nnssaaf/v1/aiw"
        -H "Content-Type: application/json"
        -H "Accept: text/html"
        -H "X-Request-ID: test-error-04-$$"
    )
    if [[ "$AUTH_DISABLED" != "1" ]] && [[ -n "$AUTH_TOKEN" ]]; then
        curl_args+=(-H "Authorization: Bearer ${AUTH_TOKEN}")
    fi
    response=$(curl "${curl_args[@]}" 2>/dev/null) || true
    status_code=$(echo "$response" | tail -n1)
    if [[ "$status_code" == "406" ]]; then
        log_pass "error-04 - Invalid Accept (expected 406, got $status_code)"
    else
        log_fail "error-04 - Invalid Accept (expected 406, got $status_code)"
    fi
}

# ─── Run All Tests ────────────────────────────────────────────────────────────

if [[ -z "$SCENARIO_FILTER" ]]; then
    run_create_positive
    run_create_negative
    run_query_confirm
    run_delete
    run_error_handling
else
    case "$SCENARIO_FILTER" in
        create)
            run_create_positive
            run_create_negative
            ;;
        query)
            run_query_confirm
            ;;
        delete)
            run_delete
            ;;
        error)
            run_error_handling
            ;;
        *)
            log_fail "Unknown scenario filter: $SCENARIO_FILTER"
            exit 1
            ;;
    esac
fi

# ─── Summary ──────────────────────────────────────────────────────────────────

print_summary
