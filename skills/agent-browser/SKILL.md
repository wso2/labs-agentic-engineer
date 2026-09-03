---
name: agent-browser
description: The browser CLI a coding run drives a web page with — opening a URL, clicking through a screen, reading what actually rendered. Load before the first `agent-browser` command; `mock-verification` names it. For an e2e test suite against a deployed system, `playwright-cli` and `aep-validation` own that instead.
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

- **The CLI and its browser are installed.** 

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| An `@eN` ref no longer works | Refs belong to the snapshot that produced them, and the page moved on | Take a fresh snapshot and use its refs. |
| A snapshot is empty, or an element that is plainly on the screen is "not found" — right after an `open` or a click that navigated | The command returned when the document did; a single-page app paints after that, so you read the page one moment too early | Wait for the network to settle before you snapshot. Believe the second reading, never the first — a blank page reported as a defect is a finding that costs somebody a fixing round. |
| A click never returns, and every command after it hangs or times out | The click opened a **native** dialog — `confirm`, `alert`, `prompt`. It blocks the page, so nothing else can run until the dialog is answered | Read the dialog section of `agent-browser skills get core --full` **before** the click that opens one, and drive it the way that reference says. Guessing at this costs tens of calls: the click that hangs is also the click you cannot undo. |
