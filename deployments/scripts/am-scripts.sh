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

# Staging of Agent Manager's environment-Thunder scripts — sourced by the setup
# scripts that provision or bind a per-environment Thunder. Requires env.sh
# (AMP_VERSION) and utils.sh (fetch_gh_raw) to be sourced first.
#
# Agent Manager's scripts are FETCHED at the release ref, not vendored: the
# provisioning script and its two helper libraries (thunder-naming.sh,
# ams-auth.sh) are versioned together with the charts they provision against,
# and a stale copy here would drift from them. thunder-naming.sh in particular
# is the single source of truth for the release-name / namespace / host
# derivation (53-char cap, truncate-to-46 plus a sha256-6 suffix) and says so
# in its header — no copy of that algorithm may exist in this repo.
#
# All four files are staged as SIBLINGS in one directory. add-environment-thunder.sh
# and remove-environment-thunder.sh source thunder-naming.sh and ams-auth.sh from
# beside themselves when they can, and only fetch a second copy from
# SCRIPT_BASE_URL when they cannot; staging them together means every caller in
# a run derives names from the same file.
#
# AM_SCRIPTS_DIR, when set, is a local Agent Manager checkout's
# deployments/scripts — the way to run an unreleased change to these scripts
# against a cluster before it exists at any fetchable ref.

AM_REF="amp/v${AMP_VERSION}"
AM_SCRIPT_BASE="https://raw.githubusercontent.com/wso2/agent-manager/${AM_REF}/deployments/scripts"
AM_STAGED_SCRIPTS=(
    thunder-naming.sh
    ams-auth.sh
    add-environment-thunder.sh
    remove-environment-thunder.sh
)

# stage_agent_manager_scripts <dir> — copies (AM_SCRIPTS_DIR) or fetches (AM_REF)
# Agent Manager's environment-Thunder scripts into <dir>. The caller owns <dir>
# and its cleanup.
stage_agent_manager_scripts() {
    local dir="$1" name
    [ -d "$dir" ] || { echo "❌ stage_agent_manager_scripts: '$dir' is not a directory" >&2; return 1; }
    if [ -n "${AM_SCRIPTS_DIR:-}" ]; then
        echo "   ℹ️  AM_SCRIPTS_DIR set — using local Agent Manager scripts from ${AM_SCRIPTS_DIR}"
        for name in "${AM_STAGED_SCRIPTS[@]}"; do
            cp "${AM_SCRIPTS_DIR}/${name}" "${dir}/${name}"
        done
    else
        for name in "${AM_STAGED_SCRIPTS[@]}"; do
            fetch_gh_raw "${AM_SCRIPT_BASE}/${name}" "${dir}/${name}" "$AM_REF"
        done
    fi
}
