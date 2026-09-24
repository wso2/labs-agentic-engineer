# Installing WSO2 Agent Manager beside AEP

Inputs for putting Agent Manager onto a cluster that already runs AEP — built by
`deployments/scripts/setup-env-for-aectl.sh` and installed by
`aectl platform install`.

Nothing here is executed by any script in this repo. The values and documents
are the decided answers; applying them is manual until an Agent Manager
installer exists.

| | |
|---|---|
| `amp-values.yaml` | Values for the `wso2-amp-platform-resources-extension` chart — the promotion graph, the shared environment, and the project name |
| `thunder-bootstrap/` | The composed ThunderID bootstrap documents, for the settings that exist once per server and cannot be held by two publishers |

## Cluster prerequisite: raise the Prometheus operator's CPU limit

Do this before installing Agent Manager.

```bash
helm upgrade observability-metrics-prometheus \
  oci://ghcr.io/openchoreo/helm-charts/observability-metrics-prometheus \
  --namespace openchoreo-observability-plane --version 0.6.1 --reuse-values \
  --set kube-prometheus-stack.prometheusOperator.resources.limits.cpu=300m
```

The chart caps the operator at `limits.cpu: 40m` and gives it a liveness probe
with `timeoutSeconds: 1`. One platform's worth of CRDs fits inside that; two
does not. The operator throttles at the ceiling, the probe times out, the
kubelet restarts the container, and it throttles again — and because the
container exits 0 the pod reads as `Completed` rather than crash-looping, so
nothing in `kubectl get pods` says what is wrong.

Only the limit moves. The 20m request is what the scheduler places on, and it is
adequate; the ceiling is what throttles.

`--reuse-values` because `setup-env-for-aectl.sh` installs this release as part
of the observability plane, and an upgrade without it drops anything that
install (or a later one) supplied.

Helm accepts an unknown `--set` path silently, so confirm the value landed
rather than assuming it:

```bash
kubectl get deploy prometheus-operator -n openchoreo-observability-plane \
  -o jsonpath='{.spec.template.spec.containers[0].resources.limits.cpu}{"\n"}'
```

## Chart versions

Everything here is composed against Agent Manager 1.0.0-rc2 and OpenChoreo
1.2.5. A chart bump invalidates these files rather than being absorbed by them:
re-render, re-compose, re-check. Each file's own header says what to re-derive.
