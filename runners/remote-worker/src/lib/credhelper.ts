/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import { shellQuote } from "./shell.js";

// Templates for the workspace credential scripts.
//
// ONE mechanism, ONE protocol. Every authenticated git operation in a run goes
// through `credhelper.sh` acting as a git credential helper — including the
// provisioning clone, which wires it in with `git -c credential.<origin>.helper`
// (see git_clone.ts) before `.git/config` exists to hold it. Nothing uses
// GIT_ASKPASS, and no GitHub token is ever handed to git through an env var, a
// URL, or argv.
//
// That single-path property is load-bearing, and was learned the hard way: an
// earlier version of this file carried BOTH protocols in one script and chose
// between them with `[ -n "$1" ]`, on the belief that only GIT_ASKPASS passes an
// argument. Git invokes a credential helper as `credhelper.sh get`, so `$1` is
// non-empty in both modes; the helper branch was unreachable and every git
// operation the agent attempted failed to authenticate, while the clone (real
// GIT_ASKPASS, prompt text in `$1`) kept working and hid the breakage. Putting
// the helper on the clone too means the mechanism the agent depends on is
// exercised before the agent starts: a break is now a provisioning failure, not
// a mystery an hour into a run.
//
// Two scripts live inside each task's `.aep/` directory:
//
//   - `credhelper.sh`: the git credential helper. On every `get` it POSTs to
//     the execution-scoped credentials/refresh endpoint for a fresh GitHub
//     token. Phase 0's long-lived PAT makes the refresh trivial; Phase 2's
//     short-lived App tokens use the same code path, so token expiry mid-run
//     needs no special handling. It also enforces the PR D §6.6 anti-misroute
//     tripwire (response.taskId must match the workspace's task) and rewrites
//     .git/config user fields when the credential's identity drifted on the
//     server side mid-task.
//
//   - `gh` (a wrapper): pre-flights every `gh` invocation by rewriting
//     `$GH_CONFIG_DIR/hosts.yml` with a fresh token before exec'ing the real
//     binary. It obtains that token BY CALLING `credhelper.sh get`, so there is
//     exactly one implementation of the exchange.
//
// Both scripts read the per-task bearer from a chmod-600 file rather than from
// env, so transcripts and process listings can't leak it. taskId, workspace
// path and the refresh URL are baked in at provisioning time so the agent
// process doesn't need to thread them through env.

export interface CredHelperParams {
  // Per-task identifier baked into the script for the anti-misroute
  // check. The credhelper refuses if response.taskId doesn't match.
  taskId: string;
  // Absolute workspace path for `.git/config` rewrites on identity drift.
  workspaceDir: string;
  // Fully-resolved credentials/refresh endpoint. Baked in rather than rebuilt
  // from env inside bash: workspace.ts already owns this URL for its own use
  // (resolveRefreshUrl), and one owner means the clone and the agent cannot
  // end up pointed at different endpoints.
  refreshUrl: string;
}

// The script's filename inside `.aep/`. The gh wrapper locates it relative to
// its own path, so the two must stay siblings.
export const CREDHELPER_FILE = "credhelper.sh";

export function credHelperScript(params: CredHelperParams): string {
  const { taskId, workspaceDir, refreshUrl } = params;
  return `#!/usr/bin/env bash
# Git credential helper for AEP platform-managed repos. This is the ONLY way a
# GitHub token reaches a git process in a run: the provisioning clone wires it in
# with \`git -c credential.<origin>.helper=…\`, and the workspace's .git/config
# carries it for every operation the agent performs afterwards.
#
# Authenticates to the platform with the org publisher client_credentials token
# (POST /internal/v1/executions/{executionId}/credentials/refresh). When
# PUBLISHER_CLIENT_ID/SECRET/TOKEN_URL are set, mint a Thunder access token.
# If that mint fails, read the CC snapshot oneshot wrote to $AEP_BEARER_FILE.
#
# Diagnostics go to stderr deliberately. An earlier version stayed silent on
# every failure "so git's own error message reaches the user"; git's message for
# a helper that returns nothing is \`could not read Username for …\`, which says
# nothing about why, and the resulting debugging spiral is what motivated this
# rewrite. Git relays helper stderr, and the BFF scrubs it before it reaches the
# build log, so naming the failure costs nothing.
#
# Phase 2 PR D §6.6 anti-misroute: refuses if the refresh response's taskId
# doesn't match this script's bound task — defends against a bearer mistakenly
# mounted in the wrong workspace from rewriting this task's identity or
# borrowing this task's credential.
#
# Phase 2 PR D §6.6 identity drift: when the server-side credential's identity
# changed mid-task (PAT replaced with a different-user PAT, or App account
# renamed), rewrite .git/config user.name / user.email so subsequent commits
# attribute correctly. The first in-flight commit may still carry the old
# identity (best-effort, not transactional).
set -e
expected_task_id=${shellQuote(taskId)}
workspace_dir=${shellQuote(workspaceDir)}
refresh_url=${shellQuote(refreshUrl)}

# Drain the credential description git writes to our stdin. We don't need it —
# the helper is config-scoped to a single origin, so there is nothing to match
# on — but leaving it unread risks git taking EPIPE on a long description.
cat >/dev/null 2>&1 || true

# Protocol dispatch, before any work. Git invokes a credential helper as
# \`credhelper.sh <action>\` with the credential description on stdin, where
# action is exactly one of get / store / erase (verified against git 2.54).
# Only \`get\` wants a credential.
#
# Everything else returns immediately, without a refresh round-trip: store and
# erase have nothing to persist (the token is re-minted per operation and never
# cached), and an unknown action is one gitcredentials(7) tells helpers to
# ignore — a future git that probes for a new action must not turn into an
# authentication failure here. Exit 0, silently, for both.
case "\${1:-get}" in
  get) ;;
  *) exit 0 ;;
esac

corr_header=()
if [ -n "$AEP_CORRELATION_ID" ]; then
  corr_header=(-H "X-Correlation-ID: $AEP_CORRELATION_ID")
fi

# Tier 1 — the platform credential. Prefer publisher client_credentials, fall
# back to the staged bearer. Minted per invocation, so a long run never serves
# git an expired token.
bearer=""
if [ -n "$PUBLISHER_CLIENT_ID" ] && [ -n "$PUBLISHER_CLIENT_SECRET" ] && [ -n "$PUBLISHER_TOKEN_URL" ]; then
  cc_resp="$(curl -fsS -X POST \\
    -u "$PUBLISHER_CLIENT_ID:$PUBLISHER_CLIENT_SECRET" \\
    -H "Content-Type: application/x-www-form-urlencoded" \\
    -d 'grant_type=client_credentials' \\
    "$PUBLISHER_TOKEN_URL" 2>/dev/null || true)"
  if [ -n "$cc_resp" ]; then
    if command -v python3 >/dev/null 2>&1; then
      bearer="$(printf '%s' "$cc_resp" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("access_token",""))' 2>/dev/null || true)"
    else
      bearer="$(printf '%s' "$cc_resp" | sed -n 's/.*"access_token":"\\([^"]*\\)".*/\\1/p')"
    fi
  fi
  if [ -z "$bearer" ]; then
    echo "credhelper: publisher client_credentials mint failed; trying \\$AEP_BEARER_FILE (CC snapshot)" >&2
  fi
fi
if [ -z "$bearer" ]; then
  bearer="$(cat "$AEP_BEARER_FILE" 2>/dev/null || true)"
fi
if [ -z "$bearer" ]; then
  if [ -n "$PUBLISHER_CLIENT_ID" ] && [ -n "$PUBLISHER_CLIENT_SECRET" ] && [ -n "$PUBLISHER_TOKEN_URL" ]; then
    echo "credhelper: no platform credential — PUBLISHER_* mint failed and \\$AEP_BEARER_FILE is missing or empty" >&2
  else
    echo "credhelper: no platform credential — PUBLISHER_* unset and \\$AEP_BEARER_FILE is missing or empty" >&2
  fi
  exit 1
fi

# Tier 2 — exchange the platform credential for a GitHub token.
resp="$(curl -fsS -X POST \\
  -H "Authorization: Bearer $bearer" \\
  -H "Content-Type: application/json" \\
  "\${corr_header[@]}" \\
  -d '{}' \\
  "$refresh_url" 2>/dev/null || true)"
if [ -z "$resp" ]; then
  echo "credhelper: credential refresh failed — POST $refresh_url returned nothing" >&2
  exit 1
fi

# Parse the JSON response in a single python3 process — five fields, one line
# each, read in order. Falls back to sed if python3 is missing, which covers
# the two required fields only; identity drift needs the structured parse.
resp_token=""
resp_task_id=""
resp_login=""
resp_name=""
resp_email=""
if command -v python3 >/dev/null 2>&1; then
  {
    read -r resp_token || true
    read -r resp_task_id || true
    read -r resp_login || true
    read -r resp_name || true
    read -r resp_email || true
  } < <(printf '%s' "$resp" | python3 -c '
import json, sys
d = json.load(sys.stdin)
i = d.get("identity") or d.get("Identity") or {}
def pick(upper, lower):
    # Identity field names are CAPITALIZED on the wire (contract
    # packages/contracts/api/internal/v1 Identity) while the envelope keys are
    # lowercase. Accept either case: reading only one of them is what left the
    # drift rewrite below dead for the whole of its first life.
    return i.get(upper) or i.get(lower) or ""
print(d.get("token", ""))
print(d.get("taskId", ""))
print(pick("Login", "login"))
print(pick("Name", "name"))
print(pick("Email", "email"))
' 2>/dev/null)
else
  resp_token="$(printf '%s' "$resp" | sed -n 's/.*"token":"\\([^"]*\\)".*/\\1/p')"
  resp_task_id="$(printf '%s' "$resp" | sed -n 's/.*"taskId":"\\([^"]*\\)".*/\\1/p')"
fi

if [ -z "$resp_token" ]; then
  echo "credhelper: refresh response carried no token" >&2
  exit 1
fi

# Anti-misroute tripwire (PR D §6.6). Empty taskId in the response is
# tolerated — older git-service versions may not echo it; in that mode the
# workspace bearer's signature is the only credential check.
if [ -n "$resp_task_id" ] && [ "$resp_task_id" != "$expected_task_id" ]; then
  echo "credhelper: refusing — response.taskId ($resp_task_id) != bound task ($expected_task_id)" >&2
  exit 1
fi

# Identity drift rewrite (PR D §6.6). Only applies when the response actually
# carries identity fields and they differ from what's in .git/config. Soft-fails
# — the git op continues with the credential even if the rewrite fails
# (subsequent commits would still attribute under the old identity in that case,
# surfaced in audit later). No-ops during the provisioning clone, where
# $workspace_dir does not exist yet.
if [ -n "$resp_login" ] && [ -d "$workspace_dir/.git" ]; then
  current_name="$(git -C "$workspace_dir" config user.name 2>/dev/null || true)"
  current_email="$(git -C "$workspace_dir" config user.email 2>/dev/null || true)"
  new_name="\${resp_name:-$resp_login}"
  new_email="\${resp_email:-$resp_login@users.noreply.github.com}"
  if [ "$current_name" != "$new_name" ] || [ "$current_email" != "$new_email" ]; then
    echo "credhelper: identity drift detected ($current_name → $new_name); rewriting .git/config" >&2
    git -C "$workspace_dir" config user.name "$new_name" >/dev/null 2>&1 || true
    git -C "$workspace_dir" config user.email "$new_email" >/dev/null 2>&1 || true
  fi
fi

# The credential-helper \`get\` response: key=value lines on stdout. A bare token
# here is silently useless — git answers it with \`warning: invalid credential
# line\` and then fails the operation for want of a username.
echo "username=x-access-token"
echo "password=$resp_token"
`;
}

export function ghWrapperScript(realGhPath: string): string {
  return `#!/usr/bin/env bash
# gh CLI wrapper. Rewrites $GH_CONFIG_DIR/hosts.yml with a fresh GitHub token on
# every invocation, then execs the real binary. Phase 0's long-lived PAT makes
# that redundant; Phase 2's 1h App tokens require it for any task that runs > 1h
# between gh calls.
#
# The token comes from credhelper.sh through its own \`get\` contract rather than
# from a second copy of the exchange. One script owns the refresh, so git and
# \`gh\` share the anti-misroute tripwire without it being reimplemented. The
# duplicate this replaces read $AEP_BEARER_FILE only, so it served whatever
# token was minted at pod start and degraded silently once that expired, while
# \`git\` kept working.
#
# A refresh failure is not fatal: hosts.yml from an earlier call may still be
# valid, so we warn and let gh report its own auth error if it isn't.
set -e
helper="$(dirname "\${BASH_SOURCE[0]}")/${CREDHELPER_FILE}"

creds="$("$helper" get </dev/null || true)"
token="$(printf '%s\\n' "$creds" | sed -n 's/^password=//p')"
if [ -n "$token" ]; then
  mkdir -p "$GH_CONFIG_DIR"
  cat > "$GH_CONFIG_DIR/hosts.yml" <<EOF
github.com:
    oauth_token: $token
    user: x-access-token
    git_protocol: https
EOF
else
  echo "gh: credential refresh failed — proceeding with the auth already on disk" >&2
fi
exec ${JSON.stringify(realGhPath)} "$@"
`;
}
