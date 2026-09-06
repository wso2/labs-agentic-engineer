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

echo "=== Platform IdP ${THUNDER_RELEASE} (ThunderID via wso2-amp-thunder-extension ${AMP_VERSION}) ==="

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

# Render Agent Manager's chart to READ the bootstrap documents it ships.
#
# Only the two overrides that change those documents are passed. The install
# below passes eleven (THUNDER_SET_ARGS), and the difference is not laziness:
# most of them describe the RELEASE (public URL, gate client, admin password)
# rather than the bundle, and the three that would matter here name the merged
# ConfigMap this step is being run to produce — they cannot exist yet. The one
# value that does leak into the documents and cannot be passed, Agent Manager's
# hardcoded Thunder public URL, is rewritten by hand further down; see the
# comment there for why that is the honest fix rather than a redeclaration.
AM_RENDER="$(mktemp)"
trap 'rm -f "$AM_RENDER"' EXIT
helm template "${THUNDER_RELEASE}" "${AMP_REGISTRY}/wso2-amp-thunder-extension" \
    --version "${AMP_VERSION}" \
    --namespace "${THUNDER_NS}" \
    --set-string "thunder.fullnameOverride=${THUNDER_RELEASE}" \
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
import json, os, pathlib, re, sys
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
am_names = set(data)
for path in sorted(pathlib.Path(aep_dir).glob("*.yaml")):
    if path.name in data:
        sys.exit(f"AEP bootstrap file {path.name} collides with an Agent Manager "
                 f"document of the same name — renumber it (AEP uses 80+)")
    data[path.name] = path.read_text()

# ── Compose the server_config singletons ────────────────────────────────────
# ThunderID keeps exactly ONE server_config per name: a redeclaration replaces
# it, so whichever product's document imports last owns the whole value. Three
# of them are server-wide — `cors`, `defaultResourceServer`, `csp` — and none of
# them belongs to a publisher. The platform composes each one instead: AEP's
# bundle carries a document that sorts AFTER Agent Manager's, and the value that
# lands in it is computed here. No publisher's file is the last word.
#
# What "composed" means differs per singleton, because what each product knows
# differs. CORS is a union — each product knows its own browser origins, and the
# result is a superset that cannot drop one. The other two are ADOPTED: the
# value is Agent Manager's, unchanged, because changing the platform IdP's
# default resource server or its CSP would change amp-console and amp-api
# behaviour. The point of owning them is the ownership, not the value.
def load_doc(body):
    try:
        return yaml.safe_load(body)
    except yaml.YAMLError:
        return None

# Everything above the first non-comment line of a document: the file's own
# explanation of what it is, which survives having its value rewritten.
HEADER = re.compile(r'\A(?:#[^\n]*\n|[ \t]*\n)*')

def singleton(config_name):
    """The Agent Manager documents and the one AEP document for a server_config.

    Guards both halves of the contract: AEP publishes exactly one document per
    singleton, and it must sort after every Agent Manager document of the same
    name or the composed value is the one that gets overwritten on import.
    """
    docs = {n: d for n, d in ((n, load_doc(b)) for n, b in data.items())
            if isinstance(d, dict)
            and d.get("resource_type") == "server_config"
            and d.get("name") == config_name}
    am = [n for n in sorted(docs) if n in am_names]
    aep = [n for n in sorted(docs) if n not in am_names]
    if len(aep) != 1:
        sys.exit(f"expected exactly one AEP `{config_name}` server_config in "
                 f"thunder-resources/, found {aep or 'none'}")
    if am and aep[0] < max(am):
        sys.exit(f"{aep[0]} must sort after Agent Manager's {max(am)} or the composed "
                 f"`{config_name}` is overwritten on import")
    return am, aep[0], docs

def emit(name, doc):
    """Rewrite an AEP document's body with the composed value, header kept."""
    header = HEADER.match(data[name]).group(0)
    data[name] = header + yaml.safe_dump(doc, default_flow_style=False,
                                         sort_keys=False, width=10**6)

# `cors` — the union, Agent Manager's origins first, de-duplicated.
am_cors, cors_file, cors_docs = singleton("cors")
composed_cors = cors_docs[cors_file]
origins = []
for name in am_cors + [cors_file]:
    for origin in (cors_docs[name].get("value") or {}).get("allowedOrigins") or []:
        if origin not in origins:
            origins.append(origin)
composed_cors.setdefault("value", {})["allowedOrigins"] = origins
emit(cors_file, composed_cors)
print(f"   composed the CORS allow-list into {cors_file}: {len(origins)} origins "
      f"from {len(am_cors)} Agent Manager document(s) + AEP's")

# `defaultResourceServer` and `csp` — adopted from Agent Manager's declaration.
# A singleton Agent Manager does not declare is DROPPED from the bundle: the
# platform owns the document, not a policy of its own to fall back on.
for config_name in ("defaultResourceServer", "csp"):
    am_docs, aep_file, docs = singleton(config_name)
    if not am_docs:
        del data[aep_file]
        print(f"   no Agent Manager `{config_name}` document — {aep_file} omitted")
        continue
    # The last-sorting one is the one that would have won on import.
    adopted = docs[am_docs[-1]]
    composed = docs[aep_file]
    composed["value"] = adopted.get("value")
    emit(aep_file, composed)
    print(f"   composed `{config_name}` into {aep_file} from Agent Manager's "
          f"{am_docs[-1]}")

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

# CORS is NOT set here. ThunderID 1.0.0's static deployment.yaml has no CORS
# section, so `--set thunder.configuration.cors.*` writes a key nothing reads
# and reports success — the one failure mode worth calling out, because the
# resulting cluster looks correctly configured and still refuses every
# cross-origin call. The only thing that sets allowedOrigins is the `cors`
# server_config bootstrap document, which step 1 composes from Agent Manager's
# declaration and AEP's (thunder-resources/89-platform-cors-config.yaml).
#
# THUNDER_FORCE_UPGRADE=1 re-drives an already-deployed release. Callers use it
# to mean "the values changed, converge it" — apply_public_urls_to_cluster in
# utils.sh is the one that does.
#
# One list, used by BOTH the install and the bootstrap re-import below. They
# have to render the same chart the same way, or the Job the re-import runs is
# not the Job this release installed.
# The chart is a wrapper around the `thunderid` subchart (alias `thunder`), and
# the subchart names its objects the usual Helm way: `<release>` when the
# release name contains the chart name, `<release>-thunder` otherwise. Every
# address in this repo derives from `${THUNDER_RELEASE}-service` (env.sh), and the
# wrapper's own HTTPS route points at that name too — so a release called
# `platform-idp` would get a Deployment and Service named `platform-idp-thunder-*`
# that nothing reaches. Pin the subchart's fullname to the release so the names
# the scripts derive are the names the chart creates.
THUNDER_SET_ARGS=(
    --set-string "thunder.fullnameOverride=${THUNDER_RELEASE}"
    --set "thunder.ocIngress.hostname=${THUNDER_HOSTNAME}"
    --set "thunder.configuration.server.publicUrl=${PUBLIC_THUNDER_URL}"
    --set "thunder.configuration.jwt.issuer=${PUBLIC_THUNDER_URL}"
    --set "thunder.configuration.gateClient.hostname=${THUNDER_HOSTNAME}"
    --set "thunder.configuration.gateClient.port=${PUBLIC_THUNDER_PORT}"
    --set "thunder.configuration.gateClient.scheme=${PUBLIC_THUNDER_SCHEME}"
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

# ── 2b. The IdP's HTTPS front, reachable in-cluster by its public name ──────
# The chart also serves the IdP over TLS on a dedicated Gateway in the control
# plane (Certificate `<release>-local-tls`, chained to the chart's root CA).
# Environment Thunders — Agent Manager's second tier — trust this IdP as an
# issuer and fetch its JWKS over HTTPS at the PUBLIC hostname; ThunderID accepts
# nothing less for a trusted issuer. Two things make that work from a pod:
#
#   * the Certificate has to be issued — waited on here, so that a
#     setup-agent-manager-env.sh run straight after this one does not race
#     cert-manager. Agent Manager's own script used to wait on this object under
#     its chart's default name; with the neutral release name it no longer finds
#     it and reads the root CA directly, so this is where the wait now lives.
#   * the public hostname has to resolve, inside the cluster, to that Gateway's
#     Service rather than to the data-plane gateway (which has no 8443 for it).
#     ensure_platform_idp_in_coredns (utils.sh) installs that rewrite.
echo "⏳ Waiting for the IdP's TLS certificate..."
kubectl wait -n openchoreo-control-plane --context "${CLUSTER_CONTEXT}" \
    --for=condition=Ready --timeout=300s "certificate/${THUNDER_RELEASE}-local-tls" >/dev/null \
    || echo "⚠️  certificate/${THUNDER_RELEASE}-local-tls not Ready — environment Thunders cannot trust this IdP until it is" >&2
ensure_platform_idp_in_coredns

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
    # The ThunderID chart names its Deployment "<release>-deployment".
    local deploy="${THUNDER_RELEASE}-deployment"
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
                 # Thunder reads `server_config` documents into its runtime
                 # configuration once, at startup. Re-importing them updates the
                 # database and changes nothing that is serving traffic, so
                 # without this restart a corrected `cors` or
                 # `defaultResourceServer` imports "successfully" and the
                 # running Thunder keeps the old value — the failure looks like
                 # the document was wrong rather than never loaded.
                 echo "   restarting Thunder to load the re-imported server config..."
                 kubectl --context "${CLUSTER_CONTEXT}" -n "${THUNDER_NS}" \
                     rollout restart deploy "$deploy" >/dev/null
                 kubectl --context "${CLUSTER_CONTEXT}" -n "${THUNDER_NS}" \
                     rollout status deploy "$deploy" --timeout=300s >/dev/null
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
