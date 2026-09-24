# 09 Sandboxing and guardrails

What stops a misbehaving `ae-design-agent` or coding agent from freely using secrets, the network, or the cluster API.

**Pod controls are the control. SDK hooks are a help.** gVisor is intended, not required.

## Resource `ae-studio` (design agent pod)

### Required

- `ae-design-agent` mounts the **Default key only**. It does not mount the gitpat, the org HMAC or the publisher client. A short-lived token that `aep-api` puts on one turn may sit in memory.
- File tools stay inside the snapshot. Paths with `..`, and symlinks that leave the snapshot, are refused.
- `ae-design-agent` has **no URL-fetch tool**. Fetching an external spec stays on `aep-api`.
- The whole pod (`ae-design-agent`, `ae-collab`, `ae-studio-tools`):
  - runs as non-root;
  - has a read-only root filesystem;
  - drops all capabilities;
  - allows no privilege escalation;
  - uses seccomp `RuntimeDefault`;
  - has **no ServiceAccount token** and no path to the Kubernetes API;
  - has only named emptyDirs as writable mounts.
- **Egress** allows DNS and public ports 80 and 443. It denies private addresses, link-local, metadata addresses and the Kubernetes API. `ae-design-agent` shares this egress with `ae-studio-tools`.
- `ae-studio-tools` on `localhost` needs no extra token. Its API never returns gitpat or HMAC bytes.

### Intended

- gVisor, when the cluster has that RuntimeClass, on local and on WSO2 Cloud. A missing RuntimeClass is GAP-3. The spec does not fail without it.

### Out of scope

- Kata.
- An AI gateway that holds the Default key.
- A prompt-injection filter. Prompt injection is an accepted risk.
- A hostname allow-list as the network control.
- A new token for `localhost`.
- A separate pod so `ae-design-agent` egress can differ.
- Namespace Pod Security labels. Those stay platform work.

## Coding agent Job

### Required

- Two containers in the Job pod: `ae-coding-agent` runs the coding agent; `ae-coding-tools` runs no model.
- `ae-coding-agent` mounts the Anthropic key for the run: the Coding agent key when the org has one, otherwise the Default key. It does not mount the gitpat, the publisher client or the HMAC. It keeps Bash, the build tools and the workspace.
- `ae-coding-tools` mounts the gitpat and the publisher client. The coding agent calls it on `localhost`, with no extra token. It never returns those values and never writes them into the shared workspace.
- Git and GitHub actions only for **this run's repository**. The publisher client only for **this run's** calls to the platform. Other repositories and other platform calls are refused.
- The same pod controls and the same egress as `ae-studio`. Writable emptyDirs are the workspace, `/tmp`, `/dev/shm`, and the home directories for tool caches.
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
- **gVisor as required.** Not every cluster has the RuntimeClass; missing it is a tagged gap, not a failure.
- **A hostname allow-list for egress.** Out of scope. The control is the rule that denies private, link-local, metadata and Kubernetes API targets.
- **A per-run push limit of one branch, or a GitHub App.** Out of scope for this change; the gitpat stays the GitHub path.
