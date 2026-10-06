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

# Shared helpers for the read-only AE Studio scenario checks.
#
# Rules: no secret on argv, no secret in output, no secret in an xtrace.
# A token is read from the 0600 file run.env names and written to a 0600
# header file that curl reads with -H @file; the write runs with xtrace off.
# `bash -x` on any check therefore shows no token (the leak test in the task
# that added these scripts counts it).
#
# No `set -e`: a failed read must end in a FAIL line and the summary, never in
# a silent abort. Every count is taken with awk or `|| true`, so a count of 0
# under pipefail is a result, not an exit.
#
# Usage of a check: RUN_DIR=<run folder> bash tests/e2e/ae-studio/checks/<p>.sh
# Output: PASS|FAIL|SKIP <id> <text> per check, a summary line, exit 0 only
# when nothing failed (STRICT=1 also fails on a SKIP).
set -uo pipefail

: "${RUN_DIR:?set RUN_DIR to the run folder that holds run.env}"
if [ ! -r "$RUN_DIR/run.env" ]; then
  echo "FAIL setup $RUN_DIR/run.env is not readable"
  exit 2
fi
# xtrace_off / xtrace_on_again: keep a capability (the hook URL) out of a
# `bash -x` trace. xtrace_off notes whether xtrace was on; xtrace_on_again
# turns it back on only then.
XTRACE_WAS_ON=0
xtrace_off() {
  case $- in *x*) XTRACE_WAS_ON=1 ;; *) XTRACE_WAS_ON=0 ;; esac
  { set +x; } 2>/dev/null
}
xtrace_on_again() {
  if [ "$XTRACE_WAS_ON" = 1 ]; then set -x; fi
}

# run.env sources the hook URL (a capability): no trace while it is read.
xtrace_off
# shellcheck source=/dev/null
. "$RUN_DIR/run.env"
xtrace_on_again

NS_AEP="${NS_AEP:-wso2-aep}"
ORG="${ORG:-default}"
PASS=0
FAIL=0
SKIP=0

umask 077
SCRATCH=$(mktemp -d)
BG_PID=""
cleanup() {
  [ -z "$BG_PID" ] || kill "$BG_PID" 2>/dev/null
  rm -rf "$SCRATCH"
}
trap cleanup EXIT

pass() { printf 'PASS %s %s\n' "$1" "$2"; PASS=$((PASS + 1)); }
fail() { printf 'FAIL %s %s\n' "$1" "$2"; FAIL=$((FAIL + 1)); }
skip() { printf 'SKIP %s %s\n' "$1" "$2"; SKIP=$((SKIP + 1)); }

# need VAR ID REASON: true when run.env (or the environment) holds VAR; else a
# SKIP line and false. Use for values only a later step of the run creates.
need() {
  if [ -n "${!1:-}" ]; then return 0; fi
  skip "$2" "$3 ($1 is not set)"
  return 1
}

# fresh_org_only ID: true on a fresh org; with FRESH_ORG=0 (a cluster that
# already has a connected org) a SKIP line and false.
fresh_org_only() {
  if [ "${FRESH_ORG:-1}" != 0 ]; then return 0; fi
  skip "$1" "asserts a fresh org (FRESH_ORG=0)"
  return 1
}

# need_values ID: the values file is set, readable, non-empty and holds no
# empty line (an empty line matches everything). A missing file would make a
# value grep report a false 0, so it SKIPs instead.
need_values() {
  if [ -z "${VALUES_FILE:-}" ] || [ ! -s "$VALUES_FILE" ]; then
    skip "$1" "VALUES_FILE is unset, missing or empty"
    return 1
  fi
  if [ "$(grep -c '^$' "$VALUES_FILE" || true)" != 0 ]; then
    fail "$1" "VALUES_FILE holds an empty line"
    return 1
  fi
  return 0
}

expect_eq() { # id want got what
  if [ "$3" = "$2" ]; then pass "$1" "$4 = $3"; else fail "$1" "$4: want $2, got $3"; fi
}

expect_ge() { # id min got what
  case "$3" in '' | *[!0-9]*) fail "$1" "$4: not a number: $3"; return ;; esac
  if [ "$3" -ge "$2" ]; then pass "$1" "$4 = $3 (>= $2)"; else fail "$1" "$4: want >= $2, got $3"; fi
}

# n_lines: count the non-empty lines on stdin.
n_lines() { awk 'NF { n++ } END { print n + 0 }'; }

# log_hits PATTERN...: count the stdin lines that contain every fixed PATTERN.
log_hits() {
  awk 'BEGIN { for (i = 1; i < ARGC; i++) { p[i] = ARGV[i]; ARGV[i] = "" } n = ARGC - 1 }
       { ok = 1; for (i = 1; i <= n; i++) if (index($0, p[i]) == 0) { ok = 0; break } hits += ok }
       END { print hits + 0 }' "$@"
}

# value_scan: reads stdin, prints "<lines> <lines holding a value>" where the
# values are the lines of $VALUES_FILE. The values stay inside awk (the file
# path is the only argument); the line count tells a dead producer (0 lines)
# from a clean one. Callers run need_values first.
value_scan() {
  awk -v vf="$VALUES_FILE" '
    BEGIN { while ((getline v < vf) > 0) if (v != "") vals[++n] = v }
    { l++; for (i = 1; i <= n; i++) if (index($0, vals[i])) { h++; break } }
    END { print l + 0, h + 0 }'
}

# safe_ident ID NAME VALUE: true when VALUE is a plain identifier (letters,
# digits, dot, dash, underscore), so it can sit inside a SQL literal or a
# path. Anything else is a FAIL line and false.
safe_ident() {
  if [[ $3 =~ ^[A-Za-z0-9._-]+$ ]]; then return 0; fi
  fail "$1" "$2 is empty or has characters outside [A-Za-z0-9._-]"
  return 1
}

# pg_dump_scan: pg_dumpall of the whole server inside postgres-0, run through
# value_scan: prints "<lines> <lines holding a value>". Nothing is stored.
pg_dump_scan() {
  # shellcheck disable=SC2016 # the pod's shell expands POSTGRES_USER
  kubectl -n "$NS_AEP" exec postgres-0 -- sh -c 'pg_dumpall -U "$POSTGRES_USER"' 2>/dev/null | value_scan
}

# psql inside postgres-0 with the pod's own POSTGRES_USER/POSTGRES_DB: no
# credential leaves the pod. SQL on stdin; one value per line, | separated.
psql_q() {
  # shellcheck disable=SC2016 # the pod's shell expands POSTGRES_USER and POSTGRES_DB
  kubectl -n "$NS_AEP" exec -i postgres-0 -- \
    sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At -v ON_ERROR_STOP=1' <<<"$1"
}

# auth_header_file TOKEN_FILE: prints the path of a 0600 file holding
# "Authorization: Bearer <token>". xtrace is off while the token is expanded
# and restored after, so no trace line carries it.
auth_header_file() {
  local tok=$1 hdr xt=0
  [ -r "$tok" ] || return 1
  case $- in *x*) xt=1 ;; esac
  { set +x; } 2>/dev/null
  hdr=$(mktemp "$SCRATCH/hdr.XXXXXX")
  printf 'Authorization: Bearer %s\n' "$(<"$tok")" >"$hdr"
  if [ "$xt" = 1 ]; then set -x; fi
  printf '%s\n' "$hdr"
}

# curl_auth TOKEN_FILE|- CURL_ARGS...: curl with the bearer header read from a
# header file (never argv). "-" sends no Authorization header.
curl_auth() {
  local tok=$1 hdr
  shift
  if [ "$tok" = "-" ]; then
    curl -s --max-time 30 "$@"
    return
  fi
  if ! hdr=$(auth_header_file "$tok"); then
    echo "curl_auth: token file is not readable" >&2
    return 1
  fi
  curl -s --max-time 30 -H @"$hdr" "$@"
}

# http_code METHOD URL TOKEN_FILE|- [curl args...]: the status code, 000 when
# nothing answered.
http_code() {
  local method=$1 url=$2 tok=$3 out
  shift 3
  out=$(curl_auth "$tok" -o /dev/null -w '%{http_code}' -X "$method" "$@" "$url") || true
  printf '%s' "${out:-000}"
}

# http_body METHOD URL TOKEN_FILE|- [curl args...]: the response body.
http_body() {
  local method=$1 url=$2 tok=$3
  shift 3
  curl_auth "$tok" -X "$method" "$@" "$url"
}

# http_headers METHOD URL TOKEN_FILE|- [curl args...]: the response headers,
# names lower-cased, CR removed.
http_headers() {
  local method=$1 url=$2 tok=$3
  shift 3
  curl_auth "$tok" -D - -o /dev/null -X "$method" "$@" "$url" | tr -d '\r' | awk -F: 'NR > 1 && NF { k = tolower($1); sub(/^[^:]*:[ ]*/, ""); print k ": " $0 }'
}

# resolve_studio: sets DPNS and DNAME to the org's ae-studio Deployment (found
# by its OpenChoreo labels, as ae-studio-check does). False when not exactly one.
resolve_studio() {
  [ -z "${DNAME:-}" ] || return 0
  local out
  out=$(kubectl get deploy -A -l "openchoreo.dev/resource=ae-studio,openchoreo.dev/namespace=$ORG" \
    -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}{"\n"}{end}' 2>/dev/null) || return 1
  [ "$(printf '%s\n' "$out" | n_lines)" = 1 ] || return 1
  DPNS=${out%%/*}
  DNAME=${out##*/}
}

# tools_logs / aep_logs: the pod logs the log-event checks grep (JSON slog).
tools_logs() {
  resolve_studio || return 1
  kubectl -n "$DPNS" logs "deploy/$DNAME" -c ae-studio-tools --tail=-1 2>/dev/null
}
aep_logs() { kubectl -n "$NS_AEP" logs deploy/aep-api --tail=-1 2>/dev/null; }

# temporal ARGS...: the Temporal CLI inside the Temporal pod (no local install).
temporal() {
  kubectl -n "$NS_AEP" exec deploy/temporal-frontend -c temporal -- \
    temporal --address temporal-frontend:7233 "$@"
}

finish() {
  printf '%s: %d passed, %d failed, %d skipped\n' "$(basename "$0")" "$PASS" "$FAIL" "$SKIP"
  if [ "$FAIL" -gt 0 ]; then exit 1; fi
  if [ "${STRICT:-0}" = 1 ] && [ "$SKIP" -gt 0 ]; then
    echo "STRICT=1: $SKIP check(s) skipped"
    exit 1
  fi
  exit 0
}
