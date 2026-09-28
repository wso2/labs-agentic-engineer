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

# Adds Agent Manager's ThunderID bootstrap documents to a bootstrap folder that
# has not been installed yet, and removes the AEP documents they supersede.
#
#   bash deployments/scripts/publish-amp-thunder-documents.sh <bootstrap-dir>
#
# ── Why this runs before the IdP exists ─────────────────────────────────────
#
# ThunderID reads its bootstrap folder exactly once, from the chart's
# pre-install setup Job. The running server mounts no bootstrap volume, so a
# document added afterwards is never read and restarting the pod re-imports
# nothing. Whoever installs second cannot add anything — which is why both
# products' documents are published together, here, before ThunderID installs.
#
# ── This file is meant to be deleted ────────────────────────────────────────
#
# This script, deployments/agent-manager/thunder-bootstrap/, and the single
# line in setup-env-for-aectl.sh that calls it are the whole of Agent Manager's
# presence in AEP's installer. Deleting those three leaves AEP's own bootstrap
# exactly as it was — nothing else references them, and AEP's documents are
# restored by the same delete because this script only ever removes them from
# the caller's temporary folder, never from the repository.
#
# They go when each product registers its own identity configuration through
# ThunderID's admin API instead (aectl already does this for AEP's OAuth
# clients — tools/aectl/internal/thunder, EnsureApplication). See
# deployments/agent-manager/thunder-bootstrap/README.md.

set -euo pipefail

BOOTSTRAP_DIR="${1:?usage: publish-amp-thunder-documents.sh <bootstrap-dir>}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DOCS_DIR="${AMP_THUNDER_DOCS_DIR:-$REPO_ROOT/deployments/agent-manager/thunder-bootstrap}"

fail() { echo "❌ $1" >&2; [ $# -gt 1 ] && echo "   $2" >&2; exit 1; }

[ -d "$BOOTSTRAP_DIR" ] || fail "Bootstrap directory '$BOOTSTRAP_DIR' does not exist."
[ -d "$DOCS_DIR" ] \
    || fail "Missing $DOCS_DIR." "It holds Agent Manager's frozen documents; see its README."
command -v python3 >/dev/null \
    || fail "python3 not found on PATH." "Used to check the assembled bundle for duplicates."
python3 -c 'import yaml' 2>/dev/null \
    || fail "python3 cannot import yaml (PyYAML)." "Used to check the assembled bundle for duplicates."

# AEP documents that a composed document replaces. Removed from the caller's
# folder only — the composed copies carry BOTH products' needs, so dropping
# AEP's originals loses nothing, and they come straight back if this script is
# deleted. Each pairing and its reasoning is in DOCS_DIR's README.
SUPERSEDED=(
    71-fix-system-resource-server-identifier.yaml   # -> 99-composed-system-resource-server.yaml
    95-cors.yaml                                    # -> 99-composed-cors.yaml
    87-ae-install-client-admin-role.yaml            # -> 99-composed-administrator-role.yaml
)

removed=0
for doc in "${SUPERSEDED[@]}"; do
    if [ -f "${BOOTSTRAP_DIR}/${doc}" ]; then
        rm -f "${BOOTSTRAP_DIR}/${doc}"
        removed=$((removed + 1))
    fi
done

added=0
for doc in "${DOCS_DIR}"/*.yaml; do
    [ -e "$doc" ] || fail "No documents found in ${DOCS_DIR}."
    cp "$doc" "${BOOTSTRAP_DIR}/"
    added=$((added + 1))
done

# ThunderID creates applications BY NAME and aborts the WHOLE import on the
# first duplicate, so an unresolved pair does not degrade the install, it stops
# it — and the failure lands inside a pre-install hook Job that deletes itself,
# leaving an IdP that is missing everything and says nothing. Sharing an id is
# fine (that upserts); sharing only a name is fatal. Checking here costs
# nothing and is the difference between a clear message and that.
python3 - "$BOOTSTRAP_DIR" <<'PY'
import sys, pathlib, yaml
from collections import defaultdict

bootstrap = pathlib.Path(sys.argv[1])
entries = []
for path in sorted(bootstrap.glob("*.yaml")):
    try:
        for doc in yaml.safe_load_all(path.read_text()):
            if isinstance(doc, dict):
                entries.append((path.name, doc))
    except yaml.YAMLError as exc:
        sys.exit(f"❌ {path.name} is not valid YAML: {exc}")

problems = []
for field in ("name", "id"):
    groups = defaultdict(set)
    for name, doc in entries:
        key = (doc.get("resource_type"), doc.get(field))
        if key[0] and key[1] is not None:
            groups[key].add(name)
    for (kind, value), files in sorted(groups.items()):
        if len(files) > 1:
            problems.append(f"   {kind} {field}={value!r} in {', '.join(sorted(files))}")

if problems:
    print("❌ The assembled bootstrap bundle declares the same object twice:", file=sys.stderr)
    print("\n".join(problems), file=sys.stderr)
    print("   ThunderID would abort the entire import on the first of these.", file=sys.stderr)
    print("   Resolve it in deployments/agent-manager/thunder-bootstrap/ — see its README.", file=sys.stderr)
    sys.exit(1)

print(f"   {len(list(bootstrap.glob('*.yaml')))} file(s), {len(entries)} document(s), no duplicates")
PY

echo "   ✅ Agent Manager documents published (${added} added, ${removed} AEP document(s) superseded)"
