# Orchestration diagrams

Excalidraw diagrams for the Temporal-native development-flow orchestration (see the approved plan for
detail). Open the `.excalidraw` files at [excalidraw.com](https://excalidraw.com) (File → Open) or via
the VS Code "Excalidraw" extension.

**Start here:** `00-overview.md` — the design doc (why, architecture, sequence, advantages, complexities).

| File | Shows |
|---|---|
| `00-overview.md` | **Design overview** — why a workflow engine, the architecture (Mermaid component + state diagrams), the cycle sequence, advantages, and complexities with mitigations. |
| `01-user-flow.excalidraw` | The user-facing development cycle: requirement → design → implement → merge → complete, with iterate-back edges, the GitHub-issue fast-path, and the human/auto gate modes (autonomous). |
| `02-workflow-topology.excalidraw` | The workflow tiers and state machines: `DevelopmentFlowWorkflow` phase loop, the per-task `TaskLifecycleWorkflow` state machine, child-spawn + DAG (`deployed`-gated) scheduling, and the per-org limiter. |
| `03-interactions.excalidraw` | Runtime interactions across Console · aep-api · Temporal · orchestrator · GitHub — start / signal / query / webhook→signal, plus activities and the read-only state poll (high-level lane view). |
| `07-dbos-vs-temporal.md` | **Do we need Temporal? DBOS vs Temporal** — whether a workflow engine is needed at all, a DBOS↔Temporal comparison, and the recommendation for this project (Temporal default; DBOS if ops-simplicity dominates). |
| `06-argo-vs-temporal.md` | **Argo vs Temporal** — why Temporal drives the (long, human-paced, looping) dev flow while Argo still runs the build/deploy pods; plus why an idle/waiting Temporal workflow costs ~zero compute. |
| `05-configuration.md` | **Local dev vs production config** — the env-var contract, the dev/prod matrix, docker-compose dev stack, and the in-cluster (Helm + Postgres + gateway) prod setup. Same code, config-only delta. |
| `04-sequence-full-flow.md` | **Full end-to-end sequence** (Mermaid, renders on GitHub): start → requirements/design gates (human/auto) → IMPLEMENT spawning task children (DAG + per-org cap) → per-task PR/build/deploy via webhook→signal → merge → complete, plus the loop-back (iterate) sequence. |

Source generator: regenerated from `/tmp/gen_excalidraw.py` (kept out of VCS); edit the `.excalidraw`
files directly in Excalidraw once they diverge.
