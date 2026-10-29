#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"

echo "=========================================================="
echo " PayFlow: End-to-End Simulation & Verification Script"
echo " Target URL: $BASE_URL"
echo "=========================================================="

# 1. Health check
echo -e "\n[1] Checking service health..."
curl -s -f "$BASE_URL/healthz" | grep "ok" && echo "Service is healthy!"

# 2. Create Merchant
echo -e "\n[2] Creating Merchant 'Acme Coffee'..."
MERCHANT_RESP=$(curl -s -X POST "$BASE_URL/v1/merchants" \
  -H "Content-Type: application/json" \
  -d '{"name": "Acme Coffee"}')
echo "Response: $MERCHANT_RESP"

MERCHANT_ID=$(echo "$MERCHANT_RESP" | grep -o '"id":"[^"]*' | cut -d'"' -f4)
API_KEY=$(echo "$MERCHANT_RESP" | grep -o '"api_key":"[^"]*' | cut -d'"' -f4)
echo "Merchant ID: $MERCHANT_ID"
echo "API Key: $API_KEY"

# 3. Create Customer
echo -e "\n[3] Creating Customer 'Jane Cardholder'..."
CUSTOMER_RESP=$(curl -s -X POST "$BASE_URL/v1/customers" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"email": "jane@example.com", "name": "Jane Cardholder"}')
echo "Response: $CUSTOMER_RESP"
CUSTOMER_ID=$(echo "$CUSTOMER_RESP" | grep -o '"id":"[^"]*' | cut -d'"' -f4)

# 4. Create Payment Intent ($100.00 = 10000 cents) with Idempotency Key
IDEM_KEY="demo-pay-$(date +%s)"
echo -e "\n[4] Creating Payment Intent for \$100.00 (Idempotency-Key: $IDEM_KEY)..."
PAYMENT_RESP=$(curl -s -X POST "$BASE_URL/v1/payments" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Idempotency-Key: $IDEM_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"customer_id\": \"$CUSTOMER_ID\", \"amount\": 10000, \"currency\": \"USD\", \"description\": \"Specialty Espresso Subscription\"}")
echo "Response: $PAYMENT_RESP"
PAYMENT_ID=$(echo "$PAYMENT_RESP" | grep -o '"id":"[^"]*' | cut -d'"' -f4)

# 5. Verify Idempotent Replay
echo -e "\n[5] Replaying identical payment creation request (verifying idempotency)..."
REPLAY_RESP=$(curl -s -i -X POST "$BASE_URL/v1/payments" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Idempotency-Key: $IDEM_KEY" \
  -H "Content-Type: application/json" \
  -d "{\"customer_id\": \"$CUSTOMER_ID\", \"amount\": 10000, \"currency\": \"USD\", \"description\": \"Specialty Espresso Subscription\"}")
echo "$REPLAY_RESP" | grep -i "Idempotent-Replayed: true" && echo "Idempotent Replay Verified!"

# 6. Authorize Payment
echo -e "\n[6] Confirming/Authorizing payment $PAYMENT_ID..."
AUTH_RESP=$(curl -s -X POST "$BASE_URL/v1/payments/$PAYMENT_ID/confirm" \
  -H "Authorization: Bearer $API_KEY")
echo "Response: $AUTH_RESP"

# 7. Capture Payment
echo -e "\n[7] Capturing payment $PAYMENT_ID..."
CAP_RESP=$(curl -s -X POST "$BASE_URL/v1/payments/$PAYMENT_ID/capture" \
  -H "Authorization: Bearer $API_KEY")
echo "Response: $CAP_RESP"

# 8. Inspect Ledger Accounts and Entries
echo -e "\n[8] Inspecting Double-Entry Ledger Accounts..."
curl -s -X GET "$BASE_URL/v1/ledger/accounts" \
  -H "Authorization: Bearer $API_KEY"
echo ""

# 9. Issue Partial Refund ($40.00 = 4000 cents)
echo -e "\n[9] Issuing partial refund of \$40.00 (4000 cents)..."
REFUND1_RESP=$(curl -s -X POST "$BASE_URL/v1/payments/$PAYMENT_ID/refunds" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"amount": 4000, "reason": "Customer returned partial order"}')
echo "Response: $REFUND1_RESP"

# 10. Issue Final Refund ($60.00 = 6000 cents)
echo -e "\n[10] Issuing final refund of \$60.00 (6000 cents) to reach full refund..."
REFUND2_RESP=$(curl -s -X POST "$BASE_URL/v1/payments/$PAYMENT_ID/refunds" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"amount": 6000, "reason": "Goodwill courtesy refund"}')
echo "Response: $REFUND2_RESP"

# 11. Verify payment status is now 'refunded'
echo -e "\n[11] Verifying payment status is now 'refunded'..."
curl -s -X GET "$BASE_URL/v1/payments/$PAYMENT_ID" \
  -H "Authorization: Bearer $API_KEY"
echo ""

# 12. Run Reconciliation Engine
echo -e "\n[12] Triggering Independent Reconciliation Engine..."
RECON_RESP=$(curl -s -X POST "$BASE_URL/internal/reconciliation/runs")
echo "Reconciliation Response: $RECON_RESP"

echo -e "\n=========================================================="
echo " PayFlow Demo Completed Successfully!"
echo "=========================================================="
