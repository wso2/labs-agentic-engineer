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

# The live walk: Mark at Acme Corp builds Acme Expenses, from a fresh sign-in
# to v1 built and validated, driven with agent-browser against a developer's
# dev-env and real agents. It grows a step at a time as each screen is wired
# to aep-api; today it covers sign-in and New project (steps 1 and 2).
#
# Prints one line per passed check and exits non-zero at the first failure
# with "step N failed: <what>". Prerequisites and env vars: README.md.

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8090}"
# The ThunderID admin account deployments/scripts/setup-env-for-aectl.sh
# creates on every dev-env (the root README documents it): a dev default, not
# a secret.
E2E_USERNAME="${E2E_USERNAME:-admin}"
E2E_PASSWORD="${E2E_PASSWORD:-Admin@123}"
# Pressing Create project makes a real GitHub repository and starts a real
# agent turn (model calls), so the walk goes past the details form only when
# asked to.
E2E_CREATE="${E2E_CREATE:-0}"
E2E_SESSION="${E2E_SESSION:-acme-expenses-walk}"
E2E_BROWSER_ARGS="${E2E_BROWSER_ARGS:---no-sandbox}"

PROMPT="An expense tracker for our staff"
SUGGESTED_NAME="expense-tracker-staff"
FIXTURE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/fixtures/expense-policy.pdf"

# Set once Create project is pressed, so the exit trap deletes the project
# even when a later check fails.
CREATED_PROJECT=""

ab() { agent-browser --session "$E2E_SESSION" "$@"; }

fail() {
  echo "step $1 failed: $2" >&2
  exit 1
}

pass() { echo "ok   step $1: $2"; }

# retry SECONDS CMD...: runs CMD every half second until it succeeds or the
# time runs out. agent-browser's `find` does not wait, so every check on
# something the page renders asynchronously goes through here.
retry() {
  local deadline=$((SECONDS + $1))
  shift
  until "$@" >/dev/null 2>&1; do
    ((SECONDS < deadline)) || return 1
    sleep 0.5
  done
}

# has_role ROLE NAME: an element with that role and exactly that accessible name.
has_role() { ab find role "$1" text --name "$2" --exact; }

# ref_for ROLE NAME: the snapshot ref of the element with that role and
# accessible name (case-insensitive: MUI's uppercase buttons are named in
# capitals), for the commands that take a ref rather than a locator.
ref_for() {
  ab snapshot -i --json | jq -r --arg role "$1" --arg name "$2" '
    .data.refs | to_entries[]
    | select(.value.role == $role and (.value.name | ascii_downcase) == ($name | ascii_downcase))
    | "@" + .key' | head -n 1
}

# value_of NAME: the current value of the textbox with that accessible name.
value_of() {
  local ref
  ref="$(ref_for textbox "$1")"
  [[ -n "$ref" ]] || return 1
  ab get value "$ref"
}

# The conversation log's text, oldest row first.
conversation() { ab find role log text --name Conversation; }

# The conversation opens on the user's prompt.
prompt_first() { [[ "$(conversation | head -n 1)" == "$PROMPT" ]]; }

kickoff_started() {
  local log
  log="$(conversation)"
  # The agent is working, or has already said something (each agent row is
  # read out as "Agent: ...").
  [[ "$log" == *"Working…"* || "$log" == *"Agent:"* ]]
}

# Deletes the created project through aep-api (the contract's delete-project)
# as the signed-in user, via the app's own proxy. The GitHub repository is
# deliberately left standing by the platform; see README.
delete_project() {
  local status
  status="$(ab eval --stdin <<EOF
(async () => {
  const key = Object.keys(sessionStorage).find((k) => k.startsWith("oidc.user:"));
  const token = key ? JSON.parse(sessionStorage.getItem(key)).access_token : null;
  const res = await fetch("/aep-api-service/api/v1/projects/${CREATED_PROJECT}", {
    method: "DELETE",
    headers: { Authorization: "Bearer " + token },
  });
  return res.status;
})()
EOF
)"
  # 404: the create never got as far as making it.
  if [[ "$status" == "204" || "$status" == "404" ]]; then
    echo "cleanup: deleted project ${CREATED_PROJECT} (${status}); its GitHub repository is left standing"
  else
    echo "cleanup: deleting project ${CREATED_PROJECT} returned ${status}; delete it by hand" >&2
  fi
}

on_exit() {
  local code=$?
  if [[ -n "$CREATED_PROJECT" ]]; then
    delete_project || echo "cleanup: could not delete project ${CREATED_PROJECT}; delete it by hand" >&2
  fi
  ab close >/dev/null 2>&1 || true
  exit "$code"
}

command -v agent-browser >/dev/null 2>&1 || { echo "agent-browser is not installed" >&2; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "jq is not installed" >&2; exit 1; }

session_active() {
  agent-browser session list --json | jq -e --arg s "$E2E_SESSION" '.data.sessions | index($s)' >/dev/null
}
session_gone() { ! session_active; }

# A fresh browser: no session left over from an earlier, aborted run. Closed
# only when it exists: `close` on an absent session starts a daemon just to
# stop it, and an `open` straight after races that daemon's shutdown.
if session_active; then
  ab close >/dev/null 2>&1 || true
  retry 10 session_gone || { echo "could not close the leftover $E2E_SESSION browser session" >&2; exit 1; }
fi
trap on_exit EXIT

# ---------------------------------------------------------------------------
# Step 1: sign in through Thunder and land in the app.

ab --args "$E2E_BROWSER_ARGS" open "$BASE_URL" >/dev/null 2>&1 ||
  fail 1 "could not open $BASE_URL (is the app running in real mode?)"
retry 30 has_role heading "Sign In" || fail 1 "Thunder's sign-in page did not show"
pass 1 "the app sent the browser to Thunder's sign-in"

ab find role textbox fill "$E2E_USERNAME" --name Username --exact >/dev/null 2>&1 || fail 1 "no Username field"
ab find role textbox fill "$E2E_PASSWORD" --name Password --exact >/dev/null 2>&1 || fail 1 "no Password field"
ab find role button click --name "Sign In" --exact >/dev/null 2>&1 || fail 1 "no Sign In button"
ab wait --url "$BASE_URL/**" --timeout 30000 >/dev/null 2>&1 ||
  fail 1 "sign-in did not return to the app (wrong credentials, or :8090 missing from the client's redirect URIs?): $(ab get url)"
# The dev-env org is already set up, so onboarding passes straight through,
# to the Dashboard (or to New project, for an org with no projects yet).
landed() { has_role heading Dashboard || has_role heading "What do you want to build?"; }
if ! retry 30 landed; then
  if ab wait --text "Welcome to Agentic Engineer" --timeout 1000 >/dev/null 2>&1; then
    fail 1 "signed in, but the onboarding wizard showed instead of the Dashboard: the dev-env org is not set up"
  fi
  fail 1 "signed in, but did not land on the Dashboard: $(ab get url)"
fi
pass 1 "signed in as $E2E_USERNAME and landed on the Dashboard"

# ---------------------------------------------------------------------------
# Step 2: New project, up to the details form.

ab find role link click --name "New project" --exact >/dev/null 2>&1 || fail 2 "no New project link in the rail"
retry 15 has_role heading "What do you want to build?" || fail 2 "New project did not open the prompt page"
pass 2 "New project opened the prompt page"

ab find role textbox fill "$PROMPT" --name "What do you want to build?" --exact >/dev/null 2>&1 || fail 2 "no prompt box"
# The file input is hidden inside the "Attach a document" button's label and
# has no role of its own, so it is the one element reached by CSS.
ab upload 'input[type=file]' "$FIXTURE" >/dev/null 2>&1 || fail 2 "could not attach $FIXTURE"
retry 10 has_role button "Remove expense-policy.pdf" || fail 2 "expense-policy.pdf was not attached"
pass 2 "typed the prompt and attached expense-policy.pdf"

ab find role button click --name Continue >/dev/null 2>&1 || fail 2 "no Continue button"
retry 15 has_role heading "New project" || fail 2 "Continue did not open the details form"
ab wait --text "Prompt: $PROMPT" --timeout 5000 >/dev/null 2>&1 || fail 2 "the details form does not echo the prompt"
name="$(value_of "Project name")" || fail 2 "no Project name field"
[[ "$name" == "$SUGGESTED_NAME" ]] || fail 2 "suggested project name is '$name', expected '$SUGGESTED_NAME'"
repo="$(value_of "Repository name")" || fail 2 "no Repository name field"
[[ "$repo" == "$SUGGESTED_NAME" ]] || fail 2 "repository name is '$repo', expected it to follow the project name"
ab wait --text "expense-policy.pdf · PDF" --timeout 5000 >/dev/null 2>&1 ||
  fail 2 "the details form does not list expense-policy.pdf"
create_ref="$(ref_for button "Create project")"
[[ -n "$create_ref" && "$(ab is enabled "$create_ref")" == "true" ]] || fail 2 "Create project is not enabled"
pass 2 "the details form suggests '$SUGGESTED_NAME' and lists the reference document"

if [[ "$E2E_CREATE" != "1" ]]; then
  echo "stopped before Create project (set E2E_CREATE=1 to create a real project)"
  exit 0
fi

# ---------------------------------------------------------------------------
# Step 2, continued: create the project; the overview opens on the kickoff.

project="acme-expenses-$(date +%s)"
ab find role textbox fill "$project" --name "Project name" --exact >/dev/null 2>&1 || fail 2 "could not rename the project"
[[ "$(value_of "Repository name")" == "$project" ]] || fail 2 "the repository name did not follow '$project'"

CREATED_PROJECT="$project"
ab click "$(ref_for button "Create project")" >/dev/null 2>&1 || fail 2 "could not press Create project"
# Creating makes the GitHub repository before the overview opens.
ab wait --url "**/projects/$project**" --timeout 120000 >/dev/null 2>&1 ||
  fail 2 "Create project did not open the overview of $project"
retry 30 has_role heading "$project" || fail 2 "the overview does not show $project"
pass 2 "created $project and its overview opened"

# The platform starts the kickoff itself once the reference document has
# landed; the chat opens with the prompt as the first user message.
retry 60 has_role complementary "Agent chat" || fail 2 "the agent chat is not open on the overview"
retry 60 prompt_first || fail 2 "the chat's first message is not the prompt: '$(conversation | head -n 1)'"
pass 2 "the chat opens with the prompt as the first message"
retry 60 kickoff_started || fail 2 "the kickoff turn did not start"
pass 2 "the kickoff turn started"
