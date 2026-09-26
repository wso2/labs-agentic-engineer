# Installing WSO2 Agent Manager beside AEP

Inputs for putting Agent Manager onto a cluster that already runs AEP — built by
`deployments/scripts/setup-env-for-aectl.sh` and installed by
`aectl platform install`.

`deployments/scripts/setup-agent-manager.sh` is what applies all of this, and
`make dev-env` runs it as its last step — a local cluster comes up with both
products on it, because a cluster with only AEP cannot exercise anything the two
share. `WITH_AGENT_MANAGER=0 make dev-env` stops after AEP.

Its own last step runs `setup-environment-aigateway.sh`, which provisions the
environment's LLM proxy and writes the `aep.wso2.com/aigateway-*` annotations
`aep-api` resolves it by. Without those an `ai-agent` reaches Anthropic directly
— nothing fails, there is just no governed path to route onto, so no guardrail
can be attached to a running agent. `WITH_AI_GATEWAY=0` skips that step alone.

The files here are that script's inputs, kept separate from it because they are
the decided answers rather than the procedure — reviewable on their own, and
re-derivable against a newer chart without reading the script.

| | |
|---|---|
| `amp-values.yaml` | Values for the `wso2-amp-platform-resources-extension` chart — the promotion graph, the shared environment, and the project name |
| `thunder-bootstrap/` | The composed ThunderID bootstrap documents, for the settings that exist once per server and cannot be held by two publishers |
| `prefix-forked-workflow-templates.py` | Helm post-renderer for step 5, renaming Agent Manager's forked build templates so OpenChoreo keeps its own |

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

## Why Agent Manager's build templates are renamed

`setup-agent-manager.sh` does this in step 5, as a Helm post-renderer.

The platform-resources chart forks five of OpenChoreo's build templates and
names them after OpenChoreo's own: `checkout-source`, `publish-image`,
`containerfile-build`, `ballerina-buildpack-build` and `gcp-buildpacks-build`.
OpenChoreo puts those five on the cluster first — `setup-env-for-aectl.sh`
applies them from the getting-started samples, client-side — so they hold no
Helm ownership metadata and the install stops on the first one with
`invalid ownership metadata`.

Annotating the five over to the release is the other way past that error, and it
is the wrong one. The forks are not equivalent:

- Agent Manager's `checkout-source` handles basic auth only. OpenChoreo's also
  handles `ssh-privatekey`. On the fork a private SSH repo fails after a
  *warning*, not an error, so the build failure reads as something else.
- The git image moves from `alpine/git:v2.52.0` to `alpine/git`.
- The fork deletes `.git` from the workspace after cloning.

So Agent Manager's copies move to `amp-*` and OpenChoreo's are left alone. This
is the chart's own convention — it already names its generate-workload fork
`amp-generate-workload` — applied to the five it does not. The upstream fix is
for the chart to name them that way itself, and on the day it does the renamer
finds nothing, fails, and can be deleted.

### What may move, and what may not

Each name appears in three roles inside one `ClusterWorkflow`, and only one of
them names the object being renamed:

```yaml
- name: publish-image           # the step
  templateRef:
    name: publish-image         # the ClusterWorkflowTemplate   <- only this
    template: publish-image     # a template inside that object
```

The step name is what the workflow's own `steps.publish-image.outputs...`
references resolve against, and the likelier source of the task names a
`WorkflowRun` reports — which `aep-api` matches on. The step and the template
share that name there, so nothing in the code settles which it reads; leaving
both alone means it does not have to be settled.
A textual substitution would rewrite all three, so the renamer walks the
structure instead.

### Why nothing else has to change

Nothing outside the release names these five. `ComponentType`s list
`allowedWorkflows` by `kind: ClusterWorkflow`, and Agent Manager's own service
addresses build workflows by `ClusterWorkflow` name — `amp-docker`,
`amp-ballerina-buildpack`, `amp-google-cloud-buildpacks`, in
`agent-manager-service/clients/openchoreosvc/client/constants.go`. None of those
move.

One consequence for anyone following Agent Manager's own docs: its Rancher
Desktop / Podman guide patches `publish-image` and `gcp-buildpacks-build` by
name. On a converged cluster those names belong to OpenChoreo, and Agent
Manager's are `amp-` prefixed.

### Verification

The renamer refuses to emit anything unless it makes exactly 5 renames and 9
`templateRef` rewrites, so a chart that changes shape fails at render rather
than installing a surprise. After the install the script reads the cluster:
every `amp-*` template must exist, and none of OpenChoreo's five may have been
adopted into the release. That second check is the one that matters — an
adopted template changes how AEP builds, and nothing else would report it.

## Chart versions

Everything here is composed against Agent Manager 1.0.0-rc2 and OpenChoreo
1.2.5. A chart bump invalidates these files rather than being absorbed by them:
re-render, re-compose, re-check. Each file's own header says what to re-derive.
