# ADR-0045 — Design work runs in the organization's AE Studio

**Status:** Accepted · 2026-10-06
**Supersedes:** [ADR-0021](ADR-0021-the-fold-applies-accepted-writes-only.md)
(the fold is deleted; the Room's committer commits a turn's edits)
**Related:** [ADR-0039](ADR-0039-the-write-target-is-a-per-project-fact-resolved-at-use.md)
(the write target) ·
[ADR-0046](ADR-0046-ae-studio-checks-platform-idp-tokens-itself.md) (who checks
tokens) · [ADR-0047](ADR-0047-an-org-secrets-value-lives-only-in-vault.md)
(where its secrets come from) ·
[ADR-0048](ADR-0048-github-delivers-each-repositorys-webhooks-to-ae-studio.md)
(its webhook route)
**Detail:** [`components/dataplane/ae-system-project/ae-studio/design/README.md`](../../components/dataplane/ae-system-project/ae-studio/design/README.md)

## Context

The design agent, the collaboration server and every git operation ran as
install-wide services in the control plane, beside `aep-api`. One process held
every org's GitHub token, cloned every org's repositories onto one shared
volume and served every org's design turns. A turn's file edits reached git
through a second path: `aep-api` replayed the agent's stream through its own
fold (the agentfold package) and committed the result. The control plane was
the one place where a single fault reached every organization's code and
credentials.

## Decision

**Each organization's design work runs in its own AE Studio: one `ae-studio`
Resource in the org's dataplane, installed and kept current by `aep-api`.**

1. **One Resource per org.** The Resource `ae-studio` of the Project
   `ae-system`, in the org's own namespace, bound to the root of that
   project's pipeline (`WriteTargets.Resolve`, ADR-0039). One pod, three
   containers: `ae-design-agent` (turns and conversations), `ae-collab` (the
   Room) and `ae-studio-tools` (git, GitHub, the skills mirror, the webhook
   route). A local install adds the optional `webhook-relay` container
   (ADR-0048).

2. **`aep-api` installs the ResourceType per org.** The `ae-studio`
   ResourceType is created in each org's namespace, because the Cloud
   platform API forces every write into the org's namespace. Its source is
   `components/dataplane/ae-system-project/ae-studio/resourcetype.yaml`;
   `aep-api` embeds a byte copy. Parameters are what changes per release or
   org (images, org, secret references and revisions, the model connection);
   environment configs are install facts. A parameter change cuts a new
   ResourceRelease and `aep-api` re-pins the binding to it.

3. **Converged single-flight, upgraded on visit.** `GET /api/v1/ae-studio`
   answers the state and the three URLs and starts a converge when what is
   installed differs from what should be. One converge per org runs at a
   time. Each step reads first and writes only what differs: Project, its
   binding, ResourceType, Resource, release, binding pin. A new release
   reaches an org the first time someone opens its console.

4. **One host per container.** Each container gets its own host, routes only
   the paths it lists, and carries CORS on its browser routes only. There are
   no path rewrites.

5. **The pod is disposable.** The Deployment is `Recreate`. It rolls on every
   write of an org secret the pod mounts (all but `coding-agent-key`, which
   only coding Jobs read), every model-connection edit and every release,
   because each changes a pinned parameter. `studio-data` is a disk emptyDir and a
   cache of GitHub: a roll empties it and the next read re-clones.

6. **The Room commits a turn's edits.** Every file-writing turn edits the
   Room's live document. The committer in `ae-collab` writes the document to
   git through `ae-studio-tools`. Nothing in `aep-api` replays a turn's
   stream, and the agentfold package is deleted.

7. **gVisor is off, and `AE_STUDIO_RUNTIME_CLASS_NAME` is the switch.**
   `aep-api` passes that setting to the pod as its `runtimeClassName`; it is
   empty on both installs (the chart's `aeStudio.runtimeClassName`, unset on
   Cloud), so the pod runs on the default runtime. The
   containers talk over Unix socket files on shared emptyDirs, and the mount
   is the gate (ADR-0046). Under Cloud gVisor a socket file does not cross
   containers. Turning gVisor on therefore needs one of: runsc honouring the
   pod's `dev.gvisor.spec.mount` annotations (the Cloud runsc setting
   pod_annotations must admit dev.gvisor.* keys), which keeps the file
   sockets; or abstract sockets plus a peer check, which replaces the mount
   gate. The pod already carries those annotations.

## Consequences

- An org's GitHub token, clones and design turns live only in its own pod.
  A fault in one org's AE Studio stays in that org.
- The control plane holds no repository and runs no git.
- A roll costs a cold re-clone (a few seconds per repository) and interrupts
  turns and Room sessions in flight. The console holds the whole page only on
  a session's first provisioning; a later roll shows a banner.
- A first install on Cloud was measured at about 15 minutes, most of it
  OpenChoreo's first converge.
- The pod runs without a sandboxed runtime. The design agent has no shell, no
  arbitrary URL fetch and no file access outside its snapshots: `web_search`
  goes to the model connection's host (Anthropic's server tool, or the
  connection's Ollama search API), and `fetch_openapi_spec` runs in `aep-api`
  over the MCP socket. The threat model records gVisor as an open gap.

## Alternatives considered

- **One host per pod with path rewrites.** Rejected: every route needs a
  rewrite, and one CORS rule would cover every path of every container.
- **One shared install-wide service.** Rejected: it keeps every org's token
  and clones in one process, which is the isolation this decision exists for.
- **gVisor now.** Rejected: socket files do not cross containers under Cloud
  gVisor, which breaks the mount-gated sockets.
