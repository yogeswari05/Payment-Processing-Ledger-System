# PayFlow Windows PowerShell Demo Script
$ErrorActionPreference = "Stop"
$BaseUrl = if ($env:BASE_URL) { $env:BASE_URL } else { "http://localhost:8080" }

Write-Host "==========================================================" -ForegroundColor Cyan
Write-Host " PayFlow: End-to-End Simulation & Verification Script" -ForegroundColor Cyan
Write-Host " Target URL: $BaseUrl" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan

# 1. Health check
Write-Host "`n[1] Checking service health..." -ForegroundColor Yellow
$health = Invoke-RestMethod -Uri "$BaseUrl/healthz" -Method Get
Write-Host "Health: $($health.status)" -ForegroundColor Green

# 2. Create Merchant
Write-Host "`n[2] Creating Merchant 'Acme Coffee'..." -ForegroundColor Yellow
$merchantBody = @{ name = "Acme Coffee" } | ConvertTo-Json
$merchant = Invoke-RestMethod -Uri "$BaseUrl/v1/merchants" -Method Post -Body $merchantBody -ContentType "application/json"
$merchantId = $merchant.id
$apiKey = $merchant.api_key
Write-Host "Merchant ID: $merchantId" -ForegroundColor Green
Write-Host "API Key: $apiKey" -ForegroundColor Green

$headers = @{
    "Authorization" = "Bearer $apiKey"
    "Content-Type"  = "application/json"
}

# 3. Create Customer
Write-Host "`n[3] Creating Customer 'Jane Cardholder'..." -ForegroundColor Yellow
$custBody = @{ email = "jane@example.com"; name = "Jane Cardholder" } | ConvertTo-Json
$customer = Invoke-RestMethod -Uri "$BaseUrl/v1/customers" -Method Post -Headers $headers -Body $custBody
$customerId = $customer.id
Write-Host "Customer ID: $customerId" -ForegroundColor Green

# 4. Create Payment ($100.00 = 10000 cents)
$idemKey = "demo-pay-" + [guid]::NewGuid().ToString().Substring(0, 8)
Write-Host "`n[4] Creating Payment Intent for `$100.00 (Idempotency-Key: $idemKey)..." -ForegroundColor Yellow
$payHeaders = @{
    "Authorization"   = "Bearer $apiKey"
    "Idempotency-Key" = $idemKey
    "Content-Type"    = "application/json"
}
$payBody = @{
    customer_id = $customerId
    amount      = 10000
    currency    = "USD"
    description = "Specialty Espresso Subscription"
} | ConvertTo-Json

$payment = Invoke-RestMethod -Uri "$BaseUrl/v1/payments" -Method Post -Headers $payHeaders -Body $payBody
$paymentId = $payment.id
Write-Host "Payment ID: $paymentId | Status: $($payment.status) | Amount: $($payment.amount)" -ForegroundColor Green

# 5. Confirm Payment
Write-Host "`n[5] Confirming/Authorizing payment $paymentId..." -ForegroundColor Yellow
$authPay = Invoke-RestMethod -Uri "$BaseUrl/v1/payments/$paymentId/confirm" -Method Post -Headers $headers
Write-Host "Payment Status: $($authPay.status)" -ForegroundColor Green

# 6. Capture Payment
Write-Host "`n[6] Capturing payment $paymentId..." -ForegroundColor Yellow
$capPay = Invoke-RestMethod -Uri "$BaseUrl/v1/payments/$paymentId/capture" -Method Post -Headers $headers
Write-Host "Payment Captured! Status: $($capPay.status) | Captured: $($capPay.captured_amount)" -ForegroundColor Green

# 7. Check Ledger
Write-Host "`n[7] Querying Ledger Accounts..." -ForegroundColor Yellow
$accounts = Invoke-RestMethod -Uri "$BaseUrl/v1/ledger/accounts" -Method Get -Headers $headers
$accounts | Format-Table -Property name, account_type, currency

# 8. Partial Refund ($40.00)
Write-Host "`n[8] Issuing partial refund of `$40.00 (4000 cents)..." -ForegroundColor Yellow
$ref1Body = @{ amount = 4000; reason = "Partial return" } | ConvertTo-Json
$refund1 = Invoke-RestMethod -Uri "$BaseUrl/v1/payments/$paymentId/refunds" -Method Post -Headers $headers -Body $ref1Body
Write-Host "Refund 1 Status: $($refund1.status) | Amount: $($refund1.amount)" -ForegroundColor Green

# 9. Second Refund ($60.00)
Write-Host "`n[9] Issuing final refund of `$60.00 (6000 cents)..." -ForegroundColor Yellow
$ref2Body = @{ amount = 6000; reason = "Full return" } | ConvertTo-Json
$refund2 = Invoke-RestMethod -Uri "$BaseUrl/v1/payments/$paymentId/refunds" -Method Post -Headers $headers -Body $ref2Body
Write-Host "Refund 2 Status: $($refund2.status) | Amount: $($refund2.amount)" -ForegroundColor Green

# 10. Verify Payment State
Write-Host "`n[10] Checking final payment state..." -ForegroundColor Yellow
$finalPay = Invoke-RestMethod -Uri "$BaseUrl/v1/payments/$paymentId" -Method Get -Headers $headers
Write-Host "Payment Status: $($finalPay.status) | Refunded: $($finalPay.refunded_amount)/$($finalPay.captured_amount)" -ForegroundColor Green

# 11. Run Reconciliation
Write-Host "`n[11] Triggering Reconciliation Run..." -ForegroundColor Yellow
$recon = Invoke-RestMethod -Uri "$BaseUrl/internal/reconciliation/runs" -Method Post
Write-Host "Reconciliation Run: $($recon.id) | Status: $($recon.status) | Anomalies: $($recon.total_anomalies)" -ForegroundColor Green

Write-Host "`n==========================================================" -ForegroundColor Cyan
Write-Host " PayFlow Demo Completed Successfully!" -ForegroundColor Cyan
Write-Host "==========================================================" -ForegroundColor Cyan
