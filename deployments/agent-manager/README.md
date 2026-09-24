# Installing WSO2 Agent Manager beside AEP

Inputs for putting Agent Manager onto a cluster that already runs AEP — built by
`deployments/scripts/setup-env-for-aectl.sh` and installed by
`aectl platform install`.

`deployments/scripts/setup-agent-manager.sh` is what applies all of this. The
files here are its inputs, kept separate from it because they are the decided
answers rather than the procedure — reviewable on their own, and re-derivable
against a newer chart without reading the script.

| | |
|---|---|
| `amp-values.yaml` | Values for the `wso2-amp-platform-resources-extension` chart — the promotion graph, the shared environment, and the project name |
| `thunder-bootstrap/` | The composed ThunderID bootstrap documents, for the settings that exist once per server and cannot be held by two publishers |

## Why the Prometheus operator's CPU limit is raised

`setup-agent-manager.sh` does this in step 2, before any Agent Manager chart
lands. It is here because the reason is not obvious from the line that does it.

The chart caps the operator at `limits.cpu: 40m` and gives it a liveness probe
with `timeoutSeconds: 1`. One platform's worth of CRDs fits inside that; two
does not. The operator throttles at the ceiling, the probe times out, the
kubelet restarts the container, and it throttles again — and because the
container exits 0 the pod reads as `Completed` rather than crash-looping, so
nothing in `kubectl get pods` says what is wrong.

Only the limit moves. The 20m request is what the scheduler places on, and it is
adequate; the ceiling is what throttles.

Helm accepts an unknown `--set` path silently, so the script reads the value
back off the live Deployment and fails if it is not `300m` — a moved value path
in a newer chart would otherwise leave the ceiling where it was, with the
restart loop only appearing later and reading as `Completed`.

## Chart versions

Everything here is composed against Agent Manager 1.0.0-rc2 and OpenChoreo
1.2.5. A chart bump invalidates these files rather than being absorbed by them:
re-render, re-compose, re-check. Each file's own header says what to re-derive.
