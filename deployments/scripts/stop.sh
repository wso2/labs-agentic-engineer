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

# Stop all ASDLC services.
# Usage: cd deployments && bash scripts/stop.sh
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$SCRIPT_DIR/.."

echo "=== Stopping ASDLC Platform ==="

echo "🛑 Stopping OpenBao port-forward..."
pkill -f "port-forward.*openbao.*8200" 2>/dev/null && echo "   Stopped" || echo "   Not running"

echo "🐳 Stopping Docker services..."
cd "$DEPLOY_DIR"
docker compose down

echo ""
echo "✅ All services stopped"
echo "   (k3d cluster is still running — use 'k3d cluster delete openchoreo' to remove)"
