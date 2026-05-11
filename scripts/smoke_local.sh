#!/bin/bash
# Smoke test for local development
# Requires: docker compose up -d (postgres running)
# Usage: ./scripts/smoke_local.sh
# NOTE: mark this script executable in Unix-like environments, e.g. `chmod +x ./scripts/smoke_local.sh`.
set -e

API_URL="http://localhost:8080"
echo "=== brain-context smoke test ==="

# 1. Register tenant
echo "1. Registering tenant..."
TENANT_RESP=$(curl -sf -X POST "$API_URL/api/tenants/register" \
  -H "Content-Type: application/json" \
  -d '{"name":"smoke-tenant"}')
TENANT_TOKEN=$(echo "$TENANT_RESP" | grep -o '"token":"[^"]*"' | cut -d'"' -f4)
echo "   Tenant token: ${TENANT_TOKEN:0:20}..."

# 2. Create project
echo "2. Creating project..."
PROJECT_RESP=$(curl -sf -X POST "$API_URL/api/projects" \
  -H "Authorization: Bearer $TENANT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"smoke-project","embed_model":"gemini/text-embedding-004","embed_dimensions":768}')
PROJECT_ID=$(echo "$PROJECT_RESP" | grep -o '"id":"[^"]*"' | cut -d'"' -f4)
echo "   Project ID: $PROJECT_ID"

# 3. Issue project tokens
echo "3. Issuing project tokens..."
TOKEN_RESP=$(curl -sf -X POST "$API_URL/api/projects/$PROJECT_ID/tokens" \
  -H "Authorization: Bearer $TENANT_TOKEN")
echo "   Tokens issued OK"

echo ""
echo "=== Smoke test PASSED ==="
