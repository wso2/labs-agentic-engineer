# Dataplane secrets and authoring runtime: architecture spec

This spec describes the intended end state of Agentic Engineer (AE) after two changes:

- Organization secrets leave the control-plane database. They are written once through the SM API into vault, and only dataplane containers read them.
- The design agent, live collaboration and git work move out of the shared control plane into each organization's dataplane.

There is one architecture for a local install and for WSO2 Cloud. Where the two differ, [11-local-vs-cloud.md](11-local-vs-cloud.md) lists every difference.

## Status

**Locked on 2026-09-24.** This is the spec the WSO2 Cloud threat model is written from. A change after the lock needs a new decision; do not edit this spec in place. The open items O-2 to O-10 in [12-gaps-and-open-items.md](12-gaps-and-open-items.md) are named, not decided. The threat model tags them.

This spec does not cover the console UI. It is not an implementation plan.

## Files

| File | What it covers |
|---|---|
| [01-overview.md](01-overview.md) | Goal, scope, the words you need first, problem → fix |
| [02-today-and-problems.md](02-today-and-problems.md) | How AE works today and its seven problems |
| [03-components.md](03-components.md) | Every runtime: its job, what it mounts, what it exposes |
| [04-flows.md](04-flows.md) | Flows 1–11 and the calls inside each pod |
| [05-lifecycle.md](05-lifecycle.md) | gitpat submit, key writes, Ensure, upgrade, coding Job start |
| [06-secrets.md](06-secrets.md) | Write-only SM API, the ESO read path, which secret lands where |
| [07-identity-and-tokens.md](07-identity-and-tokens.md) | Every token: issuer, audience, lifetime, who checks it |
| [08-git-and-github.md](08-git-and-github.md) | Where each git operation runs, the webhook path |
| [09-sandboxing-and-guardrails.md](09-sandboxing-and-guardrails.md) | Pod controls, egress, tool rules for both agent pods |
| [10-cloud-trust-boundaries.md](10-cloud-trust-boundaries.md) | TB-1 to TB-9 in WSO2 Cloud |
| [11-local-vs-cloud.md](11-local-vs-cloud.md) | What differs in a local install |
| [12-gaps-and-open-items.md](12-gaps-and-open-items.md) | GAP-1 to GAP-3, accepted risks, open items |
| [13-change-inventory.md](13-change-inventory.md) | Per component: what is added, changed, removed |
| [14-glossary.md](14-glossary.md) | Every term this spec uses |
| [diagrams/](diagrams/) | PNG and Excalidraw source for every picture |

## Reading order

Everyone: `01`, then `14` as needed.

| Reader | Read |
|---|---|
| New to AE | `01`, `02`, `03` |
| Threat-model author (WSO2 Cloud) | `03`, `04`, `06`, `07`, `10`, `12` |
| Implementer | `02`, `03`, `05`, `06`, `07`, `08`, `09`, `11`, `13` |
| Security reviewer | `01`, `06`, `07`, `09`, `10`, `12` |

## Rules this spec follows

- It states the end state only. It does not tell the story of how each choice was made.
- Decision chapters (`03`, `06`, `07`, `08`, `09`) end with a short "Not chosen, and why" list.
- Flow numbers 1–11 and boundary numbers TB-1 to TB-9 are the same in every file and every picture.
- Where a picture and its Mermaid version differ, the picture (Excalidraw) wins.
- Where a glossary word in `14-glossary.md` differs from the repository's `CONTEXT.md`, `CONTEXT.md` wins.
