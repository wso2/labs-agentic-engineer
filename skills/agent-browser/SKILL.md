---
name: agent-browser
description: "The browser CLI a coding run drives a web page with — opening a URL, clicking through a screen, reading what actually rendered. Load before the first `agent-browser` command. For a web application this run built, load `mock-verification` first: that skill is the procedure, this one is only the tool. For an e2e test suite against a deployed system, `playwright-cli` and `aep-validation` own that instead."
metadata:
  aep:
    kind: platform
    audience: [coding]
---

# agent-browser

A CLI that drives Chromium and answers with the page's **accessibility tree** —
a compact outline of what a person would see, with a short `@eN` reference on
every element you can act on. That is the whole reason it suits an agent: a
screenshot has to be looked at, an outline can be read, and the refs make the
next click unambiguous.

## Load the guide from the CLI, not from here

```bash
agent-browser skills get core
```

That is the first command of any browser job. The CLI serves the guide for the
version that is actually installed, so the verbs, flags and output formats are
always the ones you have — which is why this file carries none of them.
`agent-browser skills get core --full` adds the complete command reference.

## What is already true on this platform

- **The CLI and its browser are installed.** Confirm it once, with
  `command -v agent-browser`. If that fails, the CLI is not provisioned for
  this run: say so in one line, record the walk as not run, and move on. Do not
  search the filesystem for it, try `npx`, or install it — there is no
  `agent-browser install` step to reach for, the browser is a chromium the
  image already baked and points the CLI at, and a hunt for a binary that is
  not there costs more than the walk it was for.
- **Chromium runs with `--no-sandbox`, and the environment already says so.**
  `AGENT_BROWSER_ARGS=--no-sandbox` is set in the image, so a command written
  straight out of `agent-browser skills get core` works as written. You do not
  add the flag and you do not remove it: the container has no usable chromium
  sandbox to keep (no setuid helper, and the unprivileged user namespace the
  browser falls back to is denied to it), so without the flag the browser does
  not start at all. What contains it is the container — one-shot, unprivileged,
  discarded with the run — not chromium's own boundary inside it.

  If you ever meet an environment where that variable is NOT set, the symptom is
  every browser command failing at launch, and the fix is to set it for the
  session rather than per command:

  ```bash
  export AGENT_BROWSER_ARGS=--no-sandbox
  ```

  The CLI's `--args "--no-sandbox"` flag does the same thing for ONE invocation.
  Prefer the export: a browser job is dozens of invocations against a daemon
  that outlives each one, and the flag you forget is the command that fails.

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| **Every** command fails, starting with the first `open`, and nothing ever renders | The browser is not launching at all. On this platform that is the sandbox: see "What is already true" above | `export AGENT_BROWSER_ARGS=--no-sandbox`, once, then re-run the command unchanged. Do not start varying the command itself — the second and third guesses cost more than the export. |
| An `@eN` ref no longer works | Refs belong to the snapshot that produced them, and the page moved on | Take a fresh snapshot and use its refs. |
| A snapshot is empty, or an element that is plainly on the screen is "not found" — right after an `open` or a click that navigated | The command returned when the document did; a single-page app paints after that, so you read the page one moment too early | Wait for the network to settle before you snapshot. Believe the second reading, never the first — a blank page reported as a defect is a finding that costs somebody a fixing round. |
| A click never returns, and every command after it hangs or times out | The click opened a **native** dialog — `confirm`, `alert`, `prompt`. It blocks the page, so nothing else can run until the dialog is answered | Read the dialog section of `agent-browser skills get core --full` **before** the click that opens one, and drive it the way that reference says. Guessing at this costs tens of calls: the click that hangs is also the click you cannot undo. |
