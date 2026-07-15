#!/bin/bash
# Trigger full NSSAA flow: Create + Confirm
# Usage: ./scripts/trigger-nssaa-flow.sh [GPSI] [SST] [SD]
#
# Defaults:
#   GPSI = msisdn-208046999999002
#   SST  = 1
#   SD   = 000001

set -e

GPSI="${1:-msisdn-208046999999002}"
SST="${2:-1}"
SD="${3:-000001}"
BASE_URL="${NSSAAF_URL:-https://localhost:8443}"

echo "=== NSSAA Full Flow Trigger ==="
echo "GPSI: $GPSI"
echo "Snssai: sst=$SST, sd=$SD"
echo "Base URL: $BASE_URL"
echo ""

# Step 1: Trigger Create
echo ">>> Step 1: Create (POST /slice-authentications)"
RESPONSE=$(curl -sk -X POST "$BASE_URL/nnssaaf-nssaa/v1/slice-authentications" \
  -H "Content-Type: application/json" \
  -H "X-Request-ID: test-full-flow-$(date +%s)" \
  -d "{\"gpsi\":\"$GPSI\",\"snssai\":{\"sst\":$SST,\"sd\":\"$SD\"},\"eapIdRsp\":\"dGVzdA==\"}")

echo "Create Response: $RESPONSE"

AUTH_CTX_ID=$(echo "$RESPONSE" | jq -r '.authCtxId // .authCtxId')
EAP_MSG=$(echo "$RESPONSE" | jq -r '.eapMessage')

if [ -z "$AUTH_CTX_ID" ] || [ "$AUTH_CTX_ID" = "null" ]; then
  echo "ERROR: Failed to get authCtxId from response"
  exit 1
fi

echo ""
echo "Extracted:"
echo "  AuthCtxID: $AUTH_CTX_ID"
echo "  EAPMsg: $EAP_MSG"

# Step 2: Trigger Confirm
sleep 1
echo ""
echo ">>> Step 2: Confirm (PUT /slice-authentications/$AUTH_CTX_ID)"
CONFIRM_RESPONSE=$(curl -sk -X PUT "$BASE_URL/nnssaaf-nssaa/v1/slice-authentications/$AUTH_CTX_ID" \
  -H "Content-Type: application/json" \
  -H "X-Request-ID: test-full-flow-confirm-$(date +%s)" \
  -d "{\"gpsi\":\"$GPSI\",\"snssai\":{\"sst\":$SST,\"sd\":\"$SD\"},\"eapMessage\":\"$EAP_MSG\"}")

echo "Confirm Response: $CONFIRM_RESPONSE"
echo ""
echo "=== Flow Complete ==="
