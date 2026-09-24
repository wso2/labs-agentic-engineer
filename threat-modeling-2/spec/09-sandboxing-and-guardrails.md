# 09 Sandboxing and guardrails

What stops a misbehaving `ae-design-agent` or coding agent from freely using secrets, the network, or the cluster API.

**Pod controls are the control. SDK hooks are a help.** gVisor is intended, not required.

## Resource `ae-studio` (design agent pod)

### Required

- `ae-design-agent` mounts the **Default key only**. It does not mount the gitpat, the org HMAC or the publisher client. For a Room-mode turn, the agent Room token (this Room, until the turn ends) sits in memory.
- File tools stay inside the snapshot. Paths with `..`, and symlinks that leave the snapshot, are refused.
- `ae-design-agent` has **no URL-fetch tool**. Fetching an external spec stays on `aep-api`.
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
  - The Files API of `ae-studio-tools` listens on a Unix socket in an emptyDir mounted only into `ae-collab` and `ae-studio-tools`. No token. `ae-design-agent` cannot reach it.
  - The platform MCP tools of `ae-studio-tools` listen on a second Unix socket in its own emptyDir, mounted only into `ae-design-agent` and `ae-studio-tools`. No token. `ae-collab` cannot reach it. It serves a fixed allow-list of eleven read-only tools and refuses anything else ([07-identity-and-tokens.md](07-identity-and-tokens.md)).
  - `ae-design-agent` joins a Room on `ae-collab` with the agent Room token ([07-identity-and-tokens.md](07-identity-and-tokens.md)).
  - The API of `ae-studio-tools` never returns gitpat or HMAC bytes.
- **Listeners.** Only the listeners for flows 2, 3, 4 and 5 are Resource endpoints. An ingress NetworkPolicy lets other pods in only through the org kgateway.

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
- `ae-coding-agent` mounts the Anthropic key for the run: the Coding agent key when the org has one, otherwise the Default key. It does not mount the gitpat, the publisher client or the HMAC. It keeps Bash, the build tools and the workspace.
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
- **Tokenless `localhost` for the Files API.** `ae-design-agent` shares the pod network and could call `files/apply` and flush directly, skipping the Room and review.
- **gVisor as required.** Not every cluster has the RuntimeClass; missing it is a tagged gap, not a failure.
- **A hostname allow-list for egress.** Out of scope. The control is the rule that denies private, link-local, metadata and Kubernetes API targets.
- **A per-run push limit of one branch, or a GitHub App.** Out of scope for this change; the gitpat stays the GitHub path.
