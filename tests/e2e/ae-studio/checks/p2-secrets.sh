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

# P2 secrets are references only (scenario 2.2, 2.5, 2.6, 2.7). Read-only.
# SECRETREF_PREFIX defaults to ORG (local names are default-<secret>-<hex>).
# shellcheck source=tests/e2e/ae-studio/checks/lib.sh
. "$(dirname "$0")/lib.sh"

prefix=${SECRETREF_PREFIX:-$ORG}

if refs=$(kubectl get secretreference -n "$ORG" -o name 2>/dev/null); then
  for s in github-pat github-webhook-secret ae-publisher-client ae-studio-client; do
    n=$(printf '%s\n' "$refs" | grep -c -E "/$prefix-$s-[0-9a-f]+\$" || true)
    expect_ge 2.2 1 "$n" "SecretReference $prefix-$s-<hex>"
  done
else
  fail 2.2 "kubectl get secretreference failed"
fi

# 2.5: the six org_secrets rows, each naming a live SecretReference.
if org_secrets_scope 2.5; then
  if rows=$(psql_q "select secret, secret_ref_name from org_secrets $OC_WHERE order by secret" 2>/dev/null); then
    expect_eq 2.5 "ae-publisher-client ae-studio-client coding-agent-key default-key github-pat github-webhook-secret" \
      "$(printf '%s\n' "$rows" | awk -F'|' 'NF { printf "%s%s", sep, $1; sep = " " }')" "org_secrets.secret values"
    missing=0
    while IFS='|' read -r secret ref; do
      [ -n "$secret" ] || continue
      if ! printf '%s\n' "$refs" | grep -q -E "/$ref\$"; then
        missing=$((missing + 1))
        fail 2.5 "$secret: secret_ref_name $ref is not a SecretReference in $ORG"
      fi
    done <<<"$rows"
    [ "$missing" -gt 0 ] || pass 2.5 "every secret_ref_name is a live SecretReference"
  else
    fail 2.5 "psql on postgres-0 failed"
  fi
fi

# 2.6: the column shape. org_secrets is exactly four columns; the only
# secret-looking columns are reference names, the sealed test-user password and
# org_secrets.secret itself; the dropped value-bearing columns are gone.
if cols=$(psql_q "select column_name from information_schema.columns where table_schema = 'public' and table_name = 'org_secrets' order by column_name" 2>/dev/null); then
  expect_eq 2.6 "oc_org_id secret secret_ref_name written_at" "$(printf '%s\n' "$cols" | paste -sd' ' -)" "org_secrets columns"
else
  fail 2.6 "psql on postgres-0 failed (org_secrets columns)"
fi
if odd=$(psql_q \
  "select table_name || '.' || column_name from information_schema.columns where table_schema = 'public' and column_name ~ '(secret|key_preview|key_prefix|key_last4|sealed|^value\$)' order by 1" 2>/dev/null); then
  extra=$(printf '%s\n' "$odd" | grep -v -x -E 'org_secrets\.(secret|secret_ref_name)|git_repositories\.oc_secret_ref_name|organization_idp_profiles\.admin_creds_secret_ref|test_users\.password_sealed' || true)
  expect_eq 2.6 "" "$extra" "secret-looking columns beyond the allow-list"
else
  fail 2.6 "psql on postgres-0 failed (allow/deny query)"
fi
if gone=$(psql_q "select table_name || '.' || column_name from information_schema.columns where table_schema = 'public' and (column_name in ('publisher_client_secret','publisher_secret_ref','webhook_secrets','pat_secret_ref','secret_ref_kv_path','secret_ref_property','key_preview','key_prefix','key_last4') or (table_name = 'org_secrets' and column_name = 'value'))" 2>/dev/null); then
  expect_eq 2.6 "" "$gone" "dropped value-bearing columns still present"
else
  fail 2.6 "psql on postgres-0 failed (dropped columns)"
fi

# 2.7: no secret value anywhere in the server (Temporal history included).
if need_values 2.7; then
  read -r lines hits <<<"$(pg_dump_scan)"
  if [ "${lines:-0}" -gt 0 ]; then
    expect_eq 2.7 0 "${hits:-?}" "pg_dumpall lines holding a secret value (of $lines)"
  else
    fail 2.7 "pg_dumpall produced no output"
  fi
fi

finish
