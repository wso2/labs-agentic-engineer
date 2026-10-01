# ops — Incident RCA

> **L2 · a domain.** Part of the [aep-api architecture](../../README.md).

Capture RCA-agent incident reports and correlate them with live Task executions, so the console's
Alerts bell and stepper show the *current* state rather than the write-time snapshot.

```mermaid
flowchart LR
  API(["/api/v1"]) --> SL
  INT(["/internal/v1/sre"]) -->|NewReport| CORE
  subgraph ops
    SL["slices — getreport · listreports"]
    CORE["report core<br/>model · NewReport · wire projection"]
    SL --> CORE
    CORE --> DB[("rca_agent_reports")]
  end
  SL -->|ExecutionReader| DEL[[delivery]]
```

## Slices
| Slice | Use-case | Entry |
|---|---|---|
| `getreport` | read one, reconciled against live executions | `GET /rca-agent/reports/{reportId}` |
| `listreports` | keyset page, newest first | `GET /rca-agent/reports` |

The write side is not a slice: the SRE handoff records a report through
`POST /internal/v1/sre/rca-reports` (`sre-create-rca-report`, served in
`internal/edge/internal_sre.go`), which validates with `NewReport` (`new_report.go`)
and persists through `Repository`.

## Ports
| Port | Dir | Peer · contract |
|---|---|---|
| `Repository` | needs | own store — `repository.go` (the only gorm file) |
| `ExecutionReader` | needs | `delivery` — latest execution per kind for a Task. **Optional**: nil disables correlation (satisfied by delivery's `execution.OpsExecutionReader`) |

## Owns
- `rca_agent_reports` — gorm in this domain (`repository.go` over `model.go`), single write-authority.
  Created by `internal/migrate`'s `phase10_rca_agent_reports` step, not AutoMigrate.

## Invariants — don't break
- **Correlation only promotes false→true**, and is best-effort: a lookup failure serves the stored
  snapshot rather than failing the read. `Deployed` requires a *succeeded* build (the "Verify Fix"
  threshold), not merely a build.
- **An empty page marshals to `"items":null`**, not `[]` — the contract marks `items` nullable and the
  mapping in `wire.go` preserves nil deliberately.
- `ExecutionFact` is ops' own vocabulary, not delivery's entity — the decoupling that lets ops read
  delivery through a port it owns.
- Platform-wide rules (tenant gate, secrets fence) → [../../README.md](../../README.md).
