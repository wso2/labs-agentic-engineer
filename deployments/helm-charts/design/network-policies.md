# NetworkPolicies on the platform chart

Final-state notes for the default-deny NetworkPolicy model added to
`deployments/helm-charts/platform`. Previously the chart had none — every pod
in the release namespace could reach every other pod, and any pod could reach
anything off-cluster.

## Model

Default-deny + explicit allow, toggled by `networkPolicies.enabled` (default
`true`):

- `templates/networkpolicy-default-deny.yaml` — `podSelector: {}` over both
  `Ingress` and `Egress`, denying everything for every pod in the namespace.
- `templates/networkpolicy-allow-dns.yaml` — namespace-wide egress to
  `kube-system` on 53/UDP+TCP. Without this the deny-all breaks name
  resolution for everything, including in-cluster Service DNS.
- One `networkpolicy.yaml` per component directory (`aep-api/`,
  `agent-service/`, `collab/`, `console/`, `aep-mcp-server/`, `postgres/`,
  `temporal/`, `smee-client/`), each allowing only that service's real
  ingress/egress edges. NetworkPolicies are additive per pod, so these punch
  holes in the deny-all rather than replacing it.

Cross-namespace peers (Thunder/`platform-idp`, the OpenChoreo control-plane
API, the Observer, OpenBao) are matched by `namespaceSelector` on the
Kubernetes-managed `kubernetes.io/metadata.name` label, with the namespace and
port both derived from the same `values.yaml` URLs the Deployments already use
(`{{ include "aep.urlPort" ... }}` in `_helpers.tpl`) — so overriding one of
those URLs moves the policy with it instead of silently drifting.

Internet egress (GitHub for aep-api, the org's model provider for aep-agents,
smee.io for smee-client) has no fixed host to pin, so it's scoped to "the
public internet" via `aep.networkPolicy.internetEgress` in `_helpers.tpl`: an
`ipBlock: 0.0.0.0/0` with `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`
excepted, on 80/443. The exception matters: k3d's pod (`10.42.0.0/16`) and
service (`10.43.0.0/16`) CIDRs, and most cloud VPC ranges, live inside
`10.0.0.0/8`, so this rule alone can never double as a path to another
pod/service — those still need their own selector-based rule.

## Known gaps / deliberate holes

- **`aep-mcp-server` ingress is open cluster-wide** (`namespaceSelector: {}` +
  `podSelector: {}`). It's called by the OpenChoreo SRE/RCA agent, which is a
  separate addon (`aectl sre install`, see `sre-agent-install.md`) installed
  into a namespace this chart doesn't own or control — there's no stable label
  to scope a `namespaceSelector` to. This is a routing hole, not an authz one:
  every request still forwards the caller's bearer to `aep-api`, which
  enforces org-scoped JWT auth.
- **Kubelet liveness/readiness probes** aren't explicitly allowed anywhere.
  Most CNIs that enforce NetworkPolicy (Calico, Cilium, cloud-managed NPMs)
  exempt node-to-pod probe traffic by default, but this varies by CNI and
  isn't something a portable chart can guarantee — verify probes still pass
  after installing on a CNI other than the ones tested.
- **k3d's default CNI (flannel) does not enforce NetworkPolicy at all.** These
  policies render and apply cleanly on a local k3d install but are inert there
  — no traffic is actually blocked. They only take effect on a
  policy-enforcing CNI (Calico, Cilium, Azure NPM, GKE's default, ...). This
  was the reason for the `networkPolicies.enabled` flag: turn it off if a
  target CNI can't enforce these, or to rule NetworkPolicy out while debugging
  connectivity.
- **Scope is `deployments/helm-charts/platform` only.** The
  `thunder-app-operator-system` namespace this chart creates (see
  `templates/thunder/operator-namespace.yaml`) and the operator chart that
  deploys into it are out of scope — that chart should own its own policies.
  The coding-agent runner Jobs OpenChoreo renders into per-project
  `dp-…` dataplane namespaces are also out of scope; they're not part of this
  release at all.
