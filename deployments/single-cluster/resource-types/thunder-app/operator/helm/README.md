# thunder-app-operator (Helm chart)

Installs the **thunder-app-operator**: a Kubernetes operator that reconciles
`aep.wso2.com/v1alpha1` `ThunderApplication` custom resources into OAuth2
clients on the platform Thunder IdP, publishing each assigned `client_id` back
into the cluster as a `<cr-name>-oauth` ConfigMap.

Single replica, leader election off (see `main.go` in the operator module one
level up).

## Optional reference implementation

This chart is **not** part of the platform install — it lives beside the
operator's Go source (upstream convention: chart-with-its-code, not
chart-with-every-other-chart) precisely so it reads as optional. Neither the
`wso2-ae-platform` chart nor the `wso2-agentic-engineer-bundle` chart installs
it, and no dependency-catalog machinery references it.

The thunder-app operator is one reference implementation of the `thunder-app`
`ClusterResourceType` contract — analogous to how the CNPG operator is one
reference implementation for `postgres-cnpg`. On the local dev stack, the
setup scripts (`deployments/scripts/setup-aep.sh`) install it, acting as the
cluster platform engineer (PE) would. Real clusters are free to install this
chart themselves, or bring any other backing (a different operator, a managed
service, a hand-rolled controller) that satisfies the same
`ClusterResourceType` contract — the PE authors and owns that choice, not the
platform.

## Install (local stack)

The local stack builds the image, imports it into k3d, and installs this chart
automatically — see `deployments/scripts/setup-aep.sh` (the block right after
the postgres-cnpg ClusterResourceType). To do it by hand:

```sh
docker build -t thunder-app-operator:local deployments/single-cluster/resource-types/thunder-app/operator
k3d image import thunder-app-operator:local -c openchoreo
helm upgrade --install thunder-app-operator \
  deployments/single-cluster/resource-types/thunder-app/operator/helm \
  -n thunder-app-operator-system --create-namespace \
  --set image.repository=thunder-app-operator \
  --set image.tag=local \
  --set image.pullPolicy=Never
```

## CRD

`crds/aep.wso2.com_thunderapplications.yaml` is **copied verbatim** from the
operator module's generated manifest at
`deployments/single-cluster/resource-types/thunder-app/operator/config/crd/aep.wso2.com_thunderapplications.yaml`.
It is the single source of truth — regenerate it from the `+kubebuilder` markers
and re-copy after any change to `api/v1alpha1`:

```sh
cd deployments/single-cluster/resource-types/thunder-app/operator && make generate
cp config/crd/aep.wso2.com_thunderapplications.yaml helm/crds/
```

Helm installs everything under `crds/` before the templated resources and never
deletes or upgrades it on `helm upgrade`; bumping the CRD schema requires a
manual `kubectl apply` of the regenerated file.

## Credentials

The operator authenticates to Thunder as a system OAuth2 client
(`client_credentials`, `scope=system`). By default the chart creates a Secret
from `thunder.systemClientID` / `thunder.systemClientSecret`, whose defaults are
the **local-dev** `aep-system-client` credentials. **Real clusters must
override these** (or point `thunder.existingSecret` at an externally managed
Secret with `client-id` / `client-secret` keys). See `values.yaml`.
