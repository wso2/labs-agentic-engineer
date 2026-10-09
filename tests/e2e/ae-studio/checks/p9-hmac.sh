#!/usr/bin/env bash
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

# P9 bad HMAC (scenario 9.1 to 9.3). Three POSTs the tools pod refuses before
# any state is written (401, 401, 413). The wrong signing key is a random
# throwaway (WRONG_KEY_FILE if set, else generated here), never a real secret.
# shellcheck source=tests/e2e/ae-studio/checks/lib.sh
. "$(dirname "$0")/lib.sh"

need TOOLS 9.x "the tools URL" || finish
id="e2e-bad-${RUN_ID:-$RANDOM$RANDOM}"
url="$TOOLS/webhooks/github"
body='{"zen":"e2e bad hmac"}'

if [ -n "${WRONG_KEY_FILE:-}" ] && [ -r "$WRONG_KEY_FILE" ]; then key=$(<"$WRONG_KEY_FILE"); else key=$(openssl rand -hex 32); fi
sig=$(printf '%s' "$body" | openssl dgst -sha256 -hmac "$key" | awk '{ print $NF }')
unset key

# 9.1: a signature made with the wrong key.
expect_eq 9.1 401 "$(http_code POST "$url" - -H 'X-GitHub-Event: ping' -H "X-GitHub-Delivery: $id" -H "X-Hub-Signature-256: sha256=$sig" -H 'Content-Type: application/json' -d "$body")" "wrong-key signature status"
if logs=$(tools_logs 2>/dev/null); then
  expect_eq 9.1 0 "$(printf '%s\n' "$logs" | log_hits '"msg":"webhook.forwarded"' "\"delivery\":\"$id\"")" "webhook.forwarded lines for $id"
  expect_ge 9.1 1 "$(printf '%s\n' "$logs" | log_hits '"msg":"webhook.rejected"' "\"delivery\":\"$id\"" '"reason":"signature_invalid"')" "webhook.rejected signature_invalid lines for $id"
else
  fail 9.1 "could not read the ae-studio-tools log"
fi
if n=$(psql_q "select count(*) from webhook_deliveries where delivery_id = '$id'" 2>/dev/null); then
  expect_eq 9.1 0 "$n" "webhook_deliveries rows for $id"
else
  fail 9.1 "psql on postgres-0 failed"
fi

# 9.2: no signature header.
expect_eq 9.2 401 "$(http_code POST "$url" - -H 'X-GitHub-Event: ping' -H "X-GitHub-Delivery: $id-nosig" -H 'Content-Type: application/json' -d "$body")" "unsigned status"

# 9.3: a body one byte over 25 MiB.
code=$(head -c $((25 * 1024 * 1024 + 1)) /dev/zero | http_code POST "$url" - -H 'X-GitHub-Event: ping' -H "X-GitHub-Delivery: $id-big" -H 'Content-Type: application/json' -H 'Expect:' --data-binary @-)
expect_eq 9.3 413 "$code" "oversized body status"

finish
