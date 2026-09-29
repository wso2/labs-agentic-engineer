# 09 Sandboxing and guardrails

What stops a misbehaving `ae-design-agent` or coding agent from freely using secrets, the network, or the cluster API.

**Pod controls are the control. SDK hooks are a help.** gVisor is intended, not required.

## Resource `ae-studio` (design agent pod)

### Required

- `ae-design-agent` mounts the **Default key only**, and it never receives the AE-only M2M token. It does not mount the gitpat, the org HMAC, the publisher client or the `ae-studio-<org>` client secret. For a Room join, the `ae-studio-<org>` token that `ae-studio-tools` hands over sits in memory for that connection.
- Each container of the pod checks every request it serves against the Platform IdP JWKS, with the org and role rule ([07-identity-and-tokens.md](07-identity-and-tokens.md)). The org kgateway checks no identity.
- File tools stay inside the snapshot. Paths with `..`, and symlinks that leave the snapshot, are refused.
- `ae-design-agent` has **no URL-fetch tool**. Fetching an external spec stays on `aep-api`.
- Web search runs at Anthropic as a model tool, at most four uses a turn. It does not leave from the pod.
- The whole pod (`ae-design-agent`, `ae-collab`, `ae-studio-tools`):
  - runs as non-root;
  - has a read-only root filesystem;
  - drops all capabilities;
  - allows no privilege escalation;
  - uses seccomp `RuntimeDefault`;
  - has **no ServiceAccount token** and no path to the Kubernetes API;
  - has only named emptyDirs as writable mounts;
  - does **not** share a process namespace (`shareProcessNamespace: false`). The Unix sockets below keep containers apart only because one container cannot reach another's files through `/proc`.
- **Egress** allows DNS and public ports 80 and 443. It denies private addresses, link-local, metadata addresses and the Kubernetes API. `ae-design-agent` shares this egress with `ae-studio-tools`.
- **In-pod channels.** All containers in a pod share one network, so a `localhost` port cannot keep `ae-design-agent` out.
  - The Files API of `ae-studio-tools` listens on a Unix socket in an emptyDir mounted only into `ae-collab` and `ae-studio-tools`. No token. `ae-design-agent` cannot reach it. It carries `files/bundle`, `files/apply`, seed, flush and the project-known lookup. A save writes only files under `specs/`, at most 5 MiB a file, as today.
  - The platform MCP tools of `ae-studio-tools` listen on a second Unix socket in its own emptyDir, mounted only into `ae-design-agent` and `ae-studio-tools`. No token. `ae-collab` cannot reach it. It serves four things and refuses any other method or tool name: a fixed allow-list of eleven read-only tools, the Room-join token request, the hand-off of finished-turn usage records, and the project-known lookup ([07-identity-and-tokens.md](07-identity-and-tokens.md)). Its Room-join request returns a token, never the client secret.
  - The turn socket is a third Unix socket, served by `ae-design-agent`, in the same emptyDir as the MCP socket, so only `ae-design-agent` and `ae-studio-tools` can reach it. No token. `ae-studio-tools` uses it to start a server-started turn and get its result (flow 2). `ae-collab` cannot reach it, and the model has no tool that calls it.
  - `ae-design-agent` joins a Room on the `localhost` listener of `ae-collab` with the `ae-studio-<org>` token, sent in the Hocuspocus auth message on connect, never in a URL ([07-identity-and-tokens.md](07-identity-and-tokens.md)). `ae-collab` checks it for org only. Its public Room listener refuses this token.
  - The API of `ae-studio-tools` never returns gitpat or HMAC bytes.
- **Listeners.** Only the listeners for flows 2, 3, 4, 5, 13 and 14 are Resource endpoints. `ae-design-agent` serves flow 13 only; flows 2, 3, 5 and 14 end on `ae-studio-tools`. An ingress NetworkPolicy lets other pods in only through the org kgateway.

### Intended

- gVisor, when the cluster has that RuntimeClass, on local and on WSO2 Cloud. A missing RuntimeClass is GAP-3. The spec does not fail without it.

### Out of scope

- Kata.
- An AI gateway that holds the Default key.
- A prompt-injection filter. Prompt injection is an accepted risk.
- A hostname allow-list as the network control.
- A token on an in-pod channel that only the intended containers can reach.
- Merging `ae-collab` into `ae-studio-tools`. It would put the public Room WebSocket on the container that holds the gitpat. It needs its own decision.
- A separate pod so `ae-design-agent` egress can differ.
- Namespace Pod Security labels. Those stay platform work.

## Coding agent Job

### Required

- Two containers in the Job pod: `ae-coding-agent` runs the coding agent; `ae-coding-tools` runs no model.
- `ae-coding-agent` mounts the Anthropic key for the run: the Coding agent token (the org's Claude subscription token, ADR-0036) when the org has one, otherwise the Default key. It does not mount the gitpat, the publisher client or the HMAC. It keeps Bash, the build tools and the workspace.
- `ae-coding-tools` mounts the gitpat and the publisher client. The coding agent calls it on a `127.0.0.1` listener that is not an endpoint, with no token. The agent is the only other container, and the run's scope is fixed when the Job is created. It never returns those values and never writes them into the shared workspace.
- Git and GitHub actions only for **this run's repository**. The publisher client only for **this run's** calls to the platform, including the platform MCP tools. `ae-coding-tools` serves the remote-git tools itself. Other repositories and other platform calls are refused.
- The same pod controls, the same egress and the same ingress rule as `ae-studio`. Writable emptyDirs are the workspace, `/tmp`, `/dev/shm`, and the home directories for tool caches.
- Chromium runs with `--no-sandbox`. The pod controls contain the browser.
- Keep the Write/Edit path jail, the WebFetch hook and the WebSearch hook. These hooks help. The pod controls and the egress rules are the control.

### Intended

- gVisor, same rule as `ae-studio`.

### Out of scope

- Kata.
- Using `ae-studio-tools` for the coding run.
- A hostname allow-list as the network control.
- A GitHub App to narrow the gitpat.
- A limit of one branch for push.
- SDK hooks as the SSRF control.
- Chromium's own sandbox.

## Both agents

No prompt-injection filter on either agent. The threat model records that as an accepted risk. The design limits what an injected agent can reach: a model container holds only an Anthropic key, and the tools containers act only within their own scope.

## Not chosen, and why

- **In-process tools on the model container.** Puts the gitpat and the publisher client in the model's environment (today's problem 4).
- **Copy the agent-manager single-container sandbox as is.** Its one `main` container has no secrets split; AE copies its pod hardening, no ServiceAccount token and optional gVisor, not its secret story.
- **One socket, or `localhost` TCP, for both the Files API and the MCP tools.** Every container could reach both: `ae-design-agent` could call `files/apply`, and `ae-collab` could call the tools.
- **A separate socket for the Room-join token request and the usage hand-off.** The MCP socket already reaches only `ae-design-agent` and `ae-studio-tools`. The token request returns only a token, and the usage hand-off only carries records.
- **No token for the agent's Room join, because it is on `localhost`.** Any container in the pod reaches any `localhost` port, and `ae-collab` serves every Room of the org.
- **Tokenless `localhost` for the Files API.** `ae-design-agent` shares the pod network and could call `files/apply` and flush directly, skipping the Room.
- **gVisor as required.** Not every cluster has the RuntimeClass; missing it is a tagged gap, not a failure.
- **A hostname allow-list for egress.** Out of scope. The control is the rule that denies private, link-local, metadata and Kubernetes API targets.
- **A per-run push limit of one branch, or a GitHub App.** Out of scope for this change; the gitpat stays the GitHub path.
