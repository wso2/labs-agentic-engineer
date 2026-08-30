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

# Installs the ONE platform IdP both AEP and Agent Manager authenticate against.
#
# ── Why this replaced the old `thunder` release ──────────────────────────────
#
# OpenChoreo's control plane has exactly one `security.oidc` issuer. One
# cluster means one control plane means one IdP — there is no configuration in
# which AEP's Thunder and Agent Manager's both survive. So both move onto Agent
# Manager's chart, which wraps ThunderID 1.0.0 and already carries the
# HTTPRoute, the TLS ClusterIssuer that Agent Manager's per-environment
# Thunders need, and Agent Manager's own client bootstrap.
#
# This runs UNCONDITIONALLY, not behind ENABLE_AGENT_MANAGER. Switching IdP
# release means a different PVC and a different issuer, so making it a toggle
# would mean every flip invalidated every login. The cost is that AEP's base
# pulls one chart from Agent Manager's release line, and Thunder carries Agent
# Manager's ~100 unused `amp:*` scopes.
#
# ── Why the bootstrap ConfigMap is merged here ───────────────────────────────
#
# wso2-amp-thunder-extension pins `thunder.bootstrap.configMap.name` to its own
# chart-owned ConfigMap, and ThunderID's setup Job template FAILS THE RENDER if
# both `bootstrap.scripts` and `bootstrap.configMap` are set. There is exactly
# one bootstrap channel and Agent Manager has it, so AEP cannot add a second
# source — it has to merge into the one.
#
# The merge reads Agent Manager's half straight out of `helm template` at the
# pinned AMP_VERSION, so bumping that version needs no edit here. AEP's half is
# single-cluster/thunder-resources/ (see its README).
#
# NOTE: Thunder's token subject moved from `sub` to `client_id` in this
# release. setup-openchoreo.sh rewrites OpenChoreo's entitlement claims to
# match; the two changes must land together or every service account 403s.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

AEP_RESOURCE_DIR="${SCRIPT_DIR}/../single-cluster/thunder-resources"
BOOTSTRAP_CM="aep-thunder-bootstrap"

echo "=== Platform IdP (ThunderID via wso2-amp-thunder-extension ${AMP_VERSION}) ==="

load_public_urls "$SCRIPT_DIR/../.env"

# The chart's own defaults publish Thunder at thunder.amp.localhost. Override
# every hostname-bearing key to AEP's existing public URL instead — far cheaper
# than re-pointing the 20-plus references to it across this repo, the console
# and the compose stack.
#
# There are FOUR such keys, not three. `configuration.jwt.issuer` is separate
# from `configuration.server.publicUrl` and does not follow it: miss it and
# Thunder happily serves on the right host while stamping `iss:
# http://thunder.amp.localhost:8080` into every token, which OpenChoreo then
# rejects because values-cp.yaml pins security.oidc.issuer to the public URL.
# The failure is a 401 on every authenticated call with nothing in Thunder's
# own logs to explain it.
THUNDER_HOSTNAME="${PUBLIC_THUNDER_HOST}"

# ── 1. Build the merged bootstrap ConfigMap ─────────────────────────────────
echo ""
echo "1️⃣  Merging bootstrap resources (Agent Manager's + AEP's)"

# Render Agent Manager's chart with the SAME overrides the install below uses,
# so the documents in the ConfigMap agree with the release they bootstrap.
AM_RENDER="$(mktemp)"
trap 'rm -f "$AM_RENDER"' EXIT
helm template amp-thunder-extension "${AMP_REGISTRY}/wso2-amp-thunder-extension" \
    --version "${AMP_VERSION}" \
    --namespace "${THUNDER_NS}" \
    --set "thunder.ocIngress.hostname=${THUNDER_HOSTNAME}" \
    > "$AM_RENDER"

MERGED_CM="$(mktemp)"
BOOTSTRAP_FILES_JSON="$(mktemp)"
trap 'rm -f "$AM_RENDER" "$MERGED_CM" "$BOOTSTRAP_FILES_JSON"' EXIT

# envsubst the AEP documents (they carry ${PUBLIC_CONSOLE_URL}) before merging.
AEP_RENDERED_DIR="$(mktemp -d)"
trap 'rm -f "$AM_RENDER" "$MERGED_CM" "$BOOTSTRAP_FILES_JSON"; rm -rf "$AEP_RENDERED_DIR"' EXIT
for f in "${AEP_RESOURCE_DIR}"/*.yaml; do
    envsubst '${PUBLIC_CONSOLE_URL} ${PUBLIC_THUNDER_URL} ${PUBLIC_THUNDER_HOST}' \
        < "$f" > "${AEP_RENDERED_DIR}/$(basename "$f")"
done

# De-duplicate inline JSON string arrays after substitution.
#
# The console document lists both the fixed http://localhost:8090 forms and the
# ${PUBLIC_CONSOLE_URL} ones, so that overriding the public URL ADDS an origin
# rather than replacing the local one. When the override is absent — the common
# case, since the default IS http://localhost:8090 — the two collapse onto each
# other and the rendered list carries every URI twice.
python3 - "$AEP_RENDERED_DIR" <<'PY'
import json, pathlib, re, sys

ARRAY_LINE = re.compile(r'^(\s*[A-Za-z]\w*:\s*)(\[.*\])\s*$')

for path in pathlib.Path(sys.argv[1]).glob("*.yaml"):
    out, changed = [], False
    for line in path.read_text().splitlines():
        m = ARRAY_LINE.match(line)
        if m:
            try:
                items = json.loads(m.group(2))
            except ValueError:
                items = None
            if isinstance(items, list) and all(isinstance(i, str) for i in items):
                deduped = list(dict.fromkeys(items))
                if deduped != items:
                    line = m.group(1) + json.dumps(deduped, separators=(",", ""))
                    changed = True
        out.append(line)
    if changed:
        path.write_text("\n".join(out) + "\n")
PY

PUBLIC_CONSOLE_URL="$PUBLIC_CONSOLE_URL" PUBLIC_THUNDER_URL="$PUBLIC_THUNDER_URL" \
BOOTSTRAP_CM="$BOOTSTRAP_CM" THUNDER_NS="$THUNDER_NS" \
python3 - "$AM_RENDER" "$AEP_RENDERED_DIR" "$MERGED_CM" "$BOOTSTRAP_FILES_JSON" <<'PY'
import json, os, pathlib, sys
import yaml

am_render, aep_dir, out_cm, out_files = sys.argv[1:5]

am_cm = None
for doc in yaml.safe_load_all(open(am_render)):
    if doc and doc.get("kind") == "ConfigMap" and doc["metadata"]["name"] == "amp-thunder-bootstrap":
        am_cm = doc
        break
if am_cm is None:
    sys.exit("could not find amp-thunder-bootstrap in the rendered chart — "
             "the chart's ConfigMap name changed, update setup-thunder.sh")

data = dict(am_cm["data"])

# Agent Manager's documents name the platform Thunder by ITS default public URL
# in one place the chart does not template: 70-fix-thunder-system-rs-identifier
# sets the identifier of ThunderID's own "System" resource server, which is
# conventionally "<thunder public url>/mcp".
#
# That identifier is load-bearing twice over. ThunderID's native console derives
# its own resource_identifier from configuration.server.publicUrl, so a mismatch
# makes every native-console login bounce with "invalid_target". And any client
# asking for the `system` scope has to name this exact string as its resource
# indicator, or the scope is dropped silently and every admin call 403s.
#
# This deployment publishes that same Thunder on AEP's hostname, so the
# identifier has to follow. Rewriting Agent Manager's own document is the
# honest fix: it is what the chart would do if it templated the value, and the
# alternative — re-declaring the whole resource server, resources tree and all,
# in an AEP document — would silently drift from theirs on every version bump.
am_public = "http://thunder.amp.localhost:8080"
our_public = os.environ["PUBLIC_THUNDER_URL"].rstrip("/")
if am_public != our_public:
    rewritten = 0
    for name, body in list(data.items()):
        if am_public in body:
            data[name] = body.replace(am_public, our_public)
            rewritten += 1
    print(f"   rewrote Agent Manager's Thunder public URL in {rewritten} document(s) "
          f"-> {our_public}")
for path in sorted(pathlib.Path(aep_dir).glob("*.yaml")):
    if path.name in data:
        sys.exit(f"AEP bootstrap file {path.name} collides with an Agent Manager "
                 f"document of the same name — renumber it (AEP uses 80+)")
    data[path.name] = path.read_text()

merged = {
    "apiVersion": "v1",
    "kind": "ConfigMap",
    "metadata": {
        "name": os.environ["BOOTSTRAP_CM"],
        "namespace": os.environ["THUNDER_NS"],
        "labels": {"app.kubernetes.io/component": "thunder-bootstrap"},
    },
    "data": data,
}
with open(out_cm, "w") as fh:
    yaml.safe_dump(merged, fh, default_flow_style=False, width=10**6)

# The chart mounts only the files named in bootstrap.configMap.files, so the
# list has to enumerate the union. Sorted, because ThunderID imports in
# lexical order and the numeric prefixes exist to encode dependencies.
with open(out_files, "w") as fh:
    json.dump(sorted(data.keys()), fh)

print(f"   {len(am_cm['data'])} Agent Manager + "
      f"{len(data) - len(am_cm['data'])} AEP documents")
PY

kubectl --context "${CLUSTER_CONTEXT}" create namespace "${THUNDER_NS}" \
    --dry-run=client -o yaml | kubectl --context "${CLUSTER_CONTEXT}" apply -f - >/dev/null
# Whether the documents changed decides if the importer has to be re-run below —
# it is a ~40s Job, not worth paying on every setup.
BOOTSTRAP_CHANGED=1
if kubectl --context "${CLUSTER_CONTEXT}" -n "${THUNDER_NS}" get cm "${BOOTSTRAP_CM}" \
        -o json 2>/dev/null | python3 -c "
import json, sys, yaml
live = json.load(sys.stdin)['data']
want = yaml.safe_load(open('$MERGED_CM'))['data']
sys.exit(0 if live == want else 1)" 2>/dev/null; then
    BOOTSTRAP_CHANGED=0
fi
kubectl --context "${CLUSTER_CONTEXT}" apply -f "$MERGED_CM" >/dev/null
echo "   ✅ ConfigMap ${BOOTSTRAP_CM} applied in ${THUNDER_NS} (changed: ${BOOTSTRAP_CHANGED})"

# ── 2. Install ──────────────────────────────────────────────────────────────
echo ""
echo "2️⃣  Installing ${THUNDER_RELEASE}"

# CORS is configured natively on ThunderID rather than as a kgateway filter on
# the HTTPRoute. Thunder 0.34's chart route had no filters and Thunder itself
# sent no CORS headers, so the console's cross-origin preflight to
# POST /oauth2/token returned 405 and setup patched a filter in. ThunderID
# 1.0.0 has configuration.cors, which is where this belongs.
#
# Both console origins are listed unconditionally. Agent Manager's console is
# only reachable when ENABLE_AGENT_MANAGER=1, but an allowed origin that
# nothing calls from costs nothing, and making the list conditional would mean
# a Thunder restart every time the flag flips.
# THUNDER_FORCE_UPGRADE=1 re-drives an already-deployed release. Callers use it
# to mean "the values changed, converge it" — apply_public_urls_to_cluster in
# utils.sh is the one that does.
# One list, used by BOTH the install and the bootstrap re-import below. They
# have to render the same chart the same way, or the Job the re-import runs is
# not the Job this release installed.
THUNDER_SET_ARGS=(
    --set "thunder.ocIngress.hostname=${THUNDER_HOSTNAME}"
    --set "thunder.configuration.server.publicUrl=${PUBLIC_THUNDER_URL}"
    --set "thunder.configuration.jwt.issuer=${PUBLIC_THUNDER_URL}"
    --set "thunder.configuration.gateClient.hostname=${THUNDER_HOSTNAME}"
    --set "thunder.configuration.gateClient.port=${PUBLIC_THUNDER_PORT}"
    --set "thunder.configuration.gateClient.scheme=${PUBLIC_THUNDER_SCHEME}"
    --set "thunder.configuration.cors.allowedOrigins[0]=${PUBLIC_CONSOLE_URL}"
    --set "thunder.configuration.cors.allowedOrigins[1]=${PUBLIC_THUNDER_URL}"
    --set "thunder.configuration.cors.allowedOrigins[2]=http://localhost:19080"
    --set "thunder.configuration.cors.allowedOrigins[3]=http://console.amp.localhost:8080"
    --set "thunder.setup.admin.password=admin"
    --set "thunder.bootstrap.configMap.name=${BOOTSTRAP_CM}"
    --set-json "thunder.bootstrap.configMap.files=$(cat "$BOOTSTRAP_FILES_JSON")"
)

FRESH_INSTALL=0
if [ "${THUNDER_FORCE_UPGRADE:-0}" != "1" ] && helm_release_deployed "${THUNDER_RELEASE}" "${THUNDER_NS}"; then
    echo "⏭️  Already installed"
else
    helm_release_deployed "${THUNDER_RELEASE}" "${THUNDER_NS}" || FRESH_INSTALL=1
    helm upgrade --install "${THUNDER_RELEASE}" \
        "${AMP_REGISTRY}/wso2-amp-thunder-extension" \
        --version "${AMP_VERSION}" \
        --namespace "${THUNDER_NS}" --create-namespace \
        --kube-context "${CLUSTER_CONTEXT}" \
        "${THUNDER_SET_ARGS[@]}" \
        --timeout 10m || {
        echo "❌ Platform IdP installation failed." >&2
        echo "   Bootstrap job logs:  kubectl logs -n ${THUNDER_NS} job/${THUNDER_RELEASE}-setup" >&2
        exit 1
    }
fi

echo "⏳ Waiting for the platform IdP..."
kubectl wait -n "${THUNDER_NS}" --context "${CLUSTER_CONTEXT}" \
    --for=condition=available --timeout=300s deployment --all

# ── 3. Re-import the bootstrap when it changed ──────────────────────────────
# ThunderID's setup Job is a `helm.sh/hook: pre-install` hook — pre-install
# ONLY. A `helm upgrade` never re-runs it, so everything the bootstrap owns
# (OAuth clients, their scopes and redirect URIs, roles, the System resource
# server's identifier) is frozen at whatever the FIRST install imported. Editing
# a document and re-running helm changes the ConfigMap and nothing else, which
# looks like the edit landed and did not.
#
# So the Job is re-run explicitly, as a plain Job rather than a hook. Safe to
# repeat: the importer's documents are declarative upserts, which is the same
# property that lets the hook itself be re-entrant.
reimport_bootstrap() {
    local job="${THUNDER_RELEASE}-bootstrap-reimport"
    echo "   re-running the bootstrap importer..."
    kubectl --context "${CLUSTER_CONTEXT}" -n "${THUNDER_NS}" \
        delete job "$job" --ignore-not-found --wait=true >/dev/null 2>&1

    # Take the Job the chart would install and strip what makes it a hook.
    # backoffLimit 0 so a bad document fails on the first attempt with its logs
    # still readable, instead of retrying into a BackOff the operator has to
    # decode.
    helm template "${THUNDER_RELEASE}" "${AMP_REGISTRY}/wso2-amp-thunder-extension" \
        --version "${AMP_VERSION}" --namespace "${THUNDER_NS}" \
        "${THUNDER_SET_ARGS[@]}" \
        | JOB_NAME="$job" python3 -c "
import os, sys, yaml
docs = [d for d in yaml.safe_load_all(sys.stdin) if d]
jobs = [d for d in docs if d['kind'] == 'Job']
if not jobs:
    sys.exit('no setup Job in the rendered chart — the chart layout changed')
job = jobs[0]
job['metadata']['name'] = os.environ['JOB_NAME']
job['metadata'].pop('annotations', None)          # drop the helm hook markers
job['spec']['backoffLimit'] = 0
job['spec']['template']['spec']['restartPolicy'] = 'Never'
yaml.safe_dump(job, sys.stdout)
" | kubectl --context "${CLUSTER_CONTEXT}" apply -f - >/dev/null

    local i state
    for i in $(seq 1 90); do
        state="$(kubectl --context "${CLUSTER_CONTEXT}" -n "${THUNDER_NS}" get job "$job" \
            -o jsonpath='{.status.succeeded}/{.status.failed}' 2>/dev/null)"
        case "$state" in
            1/*) echo "   ✅ bootstrap re-imported"
                 kubectl --context "${CLUSTER_CONTEXT}" -n "${THUNDER_NS}" \
                     delete job "$job" --ignore-not-found --wait=false >/dev/null 2>&1
                 return 0 ;;
            */1) echo "❌ bootstrap import failed:" >&2
                 kubectl --context "${CLUSTER_CONTEXT}" -n "${THUNDER_NS}" \
                     logs "job/$job" --tail=40 >&2 2>/dev/null
                 return 1 ;;
        esac
        sleep 4
    done
    echo "❌ bootstrap import did not finish within 6 minutes" >&2
    return 1
}

# Only worth the ~40s when the documents actually changed. On a fresh install
# the hook already ran them, so this is skipped.
#
# The ConfigMap is a proxy for "the documents changed", not for "Thunder has
# them" — the two can diverge if an earlier run applied the ConfigMap and then
# failed. THUNDER_REIMPORT=1 forces the import regardless; it is always safe.
if [ "${THUNDER_REIMPORT:-0}" = "1" ] || { [ "$BOOTSTRAP_CHANGED" = "1" ] && [ "$FRESH_INSTALL" != "1" ]; }; then
    reimport_bootstrap
else
    echo "   bootstrap unchanged since the last import — nothing to re-run"
fi

echo "✅ Platform IdP ready at ${PUBLIC_THUNDER_URL} (admin / admin)"
