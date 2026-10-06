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

# Evidence scan (scenario 10.6). Read-only: no secret value anywhere under
# RUN_DIR, no HAR and no token file in it. VALUES_FILE lives outside RUN_DIR.
# shellcheck source=tests/e2e/ae-studio/checks/lib.sh
. "$(dirname "$0")/lib.sh"

if need_values 10.6; then
  case $(cd "$(dirname "$VALUES_FILE")" && pwd) in
    "$(cd "$RUN_DIR" && pwd)" | "$(cd "$RUN_DIR" && pwd)"/*) fail 10.6 "VALUES_FILE is under RUN_DIR (it would count itself)" ;;
    *)
      # grep exits 2 when it cannot read VALUES_FILE or any file under RUN_DIR:
      # an unscanned file is a FAIL, never a zero count.
      counts=$(grep -r -s -c -F -f "$VALUES_FILE" "$RUN_DIR")
      rc=$?
      if [ "$rc" -gt 1 ]; then
        fail 10.6 "grep could not read VALUES_FILE or a file under RUN_DIR (exit $rc)"
      else
        hits=$(printf '%s\n' "$counts" | awk -F: '{ s += $NF } END { print s + 0 }')
        expect_eq 10.6 0 "$hits" "lines under RUN_DIR holding a secret value"
      fi
      ;;
  esac
fi

stray=$(find "$RUN_DIR" \( -name '*.har' -o -name 'tok-*' \) | n_lines)
expect_eq 10.6 0 "$stray" "HAR or tok-* files under RUN_DIR"

finish
