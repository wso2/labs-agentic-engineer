#!/bin/bash
# Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

fail() {
    echo "❌ $*" >&2
    exit 1
}

assert_file_contains() {
    local file="$1"
    local pattern="$2"
    grep -Fq "$pattern" "$file" || fail "$file does not contain: $pattern"
}

assert_file_not_contains() {
    local file="$1"
    local pattern="$2"
    if grep -Fq "$pattern" "$file"; then
        fail "$file unexpectedly contains: $pattern"
    fi
}

EXT_DIR="$REPO_DIR/deployments/sre-agent-extensions/remediation"
MCP_JSON="$EXT_DIR/mcp.json"
CONTEXT_MD="$EXT_DIR/CONTEXT.md"
HANDOFF_SKILL="$REPO_DIR/services/aep-mcp-server/skills/coding-agent-handoff/SKILL.md"
SETUP_SCRIPT="$REPO_DIR/deployments/scripts/setup-observability.sh"
FULL_SETUP_SCRIPT="$REPO_DIR/deployments/scripts/setup.sh"

[ -f "$MCP_JSON" ] || fail "missing $MCP_JSON"
[ -f "$CONTEXT_MD" ] || fail "missing $CONTEXT_MD"
[ -f "$HANDOFF_SKILL" ] || fail "missing $HANDOFF_SKILL"

assert_file_contains "$MCP_JSON" '"ae"'
assert_file_contains "$MCP_JSON" '"type": "http"'
assert_file_contains "$MCP_JSON" '${AEP_MCP_URL}'
assert_file_not_contains "$MCP_JSON" '"headers"'
assert_file_not_contains "$MCP_JSON" 'Bearer ${AEP_MCP_TOKEN}'

assert_file_contains "$CONTEXT_MD" "load_skill('coding-agent-handoff')"
assert_file_contains "$CONTEXT_MD" "ae_create_issue"
assert_file_contains "$CONTEXT_MD" "actionStatuses"
assert_file_contains "$HANDOFF_SKILL" "ae_search_related_issues"
assert_file_contains "$HANDOFF_SKILL" "ae_create_issue"

assert_file_contains "$SETUP_SCRIPT" "tharindulak/sre-agent"
assert_file_contains "$SETUP_SCRIPT" "v1.0.1-hotfix.1-anthropic"
assert_file_contains "$SETUP_SCRIPT" "RCA_LLM_API_KEY_FILE"
assert_file_contains "$SETUP_SCRIPT" "/etc/rca-agent/anthropic/RCA_LLM_API_KEY"
assert_file_contains "$SETUP_SCRIPT" "deployments/sre-agent-extensions/remediation"
assert_file_contains "$SETUP_SCRIPT" "coding-agent-handoff"
assert_file_contains "$SETUP_SCRIPT" "remediation/skills/coding-agent-handoff/SKILL.md"
assert_file_contains "$SETUP_SCRIPT" "AEP_MCP_URL=\"http://\${AEP_MCP_URL}\""
assert_file_contains "$SETUP_SCRIPT" "gsub(/\\$\\{AEP_MCP_URL\\}/, url)"
assert_file_contains "$SETUP_SCRIPT" "AEP_MCP_TOKEN"
assert_file_contains "$SETUP_SCRIPT" "AEP_MCP_DEFAULT_BEARER"
# The Anthropic secret volume is required only with the handoff on.
assert_file_contains "$SETUP_SCRIPT" 'optional: '"'"'"${ANTHROPIC_SECRET_OPTIONAL}"'
assert_file_contains "$SETUP_SCRIPT" "Ensure observability workloads are running"
assert_file_contains "$SETUP_SCRIPT" "park-observability.sh\" up"
assert_file_contains "$SETUP_SCRIPT" "reconcile-sre-anthropic-externalsecret.sh"
assert_file_contains "$FULL_SETUP_SCRIPT" "PARK_OBSERVABILITY_AFTER_SETUP"
assert_file_contains "$FULL_SETUP_SCRIPT" "ENABLE_AGENT_MANAGER=0"
assert_file_contains "$FULL_SETUP_SCRIPT" "alert → RCA → coding-agent"
assert_file_not_contains "$SETUP_SCRIPT" "services/aep-mcp-server/skills/issue-fix"

# The handoff bearer has no repository-known default: setup-aep.sh generates a
# random AEP_MCP_TOKEN, and an unset/empty value disables the fallback on both
# aep-api and aep-mcp-server.
COMPOSE_FILE="$REPO_DIR/deployments/docker-compose.yml"
SETUP_AEP_SCRIPT="$REPO_DIR/deployments/scripts/setup-aep.sh"
assert_file_not_contains "$COMPOSE_FILE" "local-dev-sre-handoff-secret"
assert_file_contains "$COMPOSE_FILE" 'SRE_HANDOFF_TOKEN: "${AEP_MCP_TOKEN:-}"'
assert_file_contains "$COMPOSE_FILE" 'AEP_MCP_DEFAULT_BEARER: "${AEP_MCP_TOKEN:+Bearer ${AEP_MCP_TOKEN}}"'
assert_file_contains "$SETUP_AEP_SCRIPT" 'AEP_MCP_TOKEN=${AEP_MCP_TOKEN_VAL}'
# An explicit empty AEP_MCP_TOKEN= disables the shortcut and must survive a
# re-run: the token is generated only when the key is absent from .env.
assert_file_contains "$SETUP_AEP_SCRIPT" 'env_has_key AEP_MCP_TOKEN || AEP_MCP_TOKEN_VAL="$(gen_hex32)"'
assert_file_not_contains "$SETUP_AEP_SCRIPT" '[ -z "$AEP_MCP_TOKEN_VAL" ] && AEP_MCP_TOKEN_VAL='
# An explicit ENABLE_AGENT_MANAGER wins over the saved .env value.
assert_file_contains "$SETUP_AEP_SCRIPT" 'ENABLE_AGENT_MANAGER_VAL="${ENABLE_AGENT_MANAGER:-$(existing_val ENABLE_AGENT_MANAGER)}"'

assert_file_contains "$REPO_DIR/deployments/scripts/start.sh" "reconcile-sre-anthropic-externalsecret.sh"
assert_file_contains "$REPO_DIR/deployments/scripts/repair-secrets.sh" "reconcile-sre-anthropic-externalsecret.sh"

echo "✅ setup-observability SRE extension assertions passed"
