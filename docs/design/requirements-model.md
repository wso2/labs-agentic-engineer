# The requirements model — overview

A product's requirements are a product page, one file per feature and a
product-wide page, with stable IDs (`F2`, `F2.3`, `P4`). Features are
interviewed, designed, built and validated one at a time. ADRs: 0039 (features
and IDs), 0040 (design out of date per feature), 0041 (a build is a selection).
Terms: `CONTEXT.md` § Requirements model.

## The flow, by skill

| Step | Skill | Writes |
|---|---|---|
| Kickoff | `start` | `prd.md`, `product-wide.md`, a stub per feature, `sources/*.md` per attached document |
| Interview a feature | `interview` (`/interview F<n>`) | that feature's file |
| Any later change | `refine` (`/feature`, `/actor`, `/amend`, `/settle`) | wherever the change belongs |
| Design | `design` (`/design F1 F2`) | the design, acceptance per feature |
| Plan a build | `task-planning` | one Task per feature per component, plus foundation Tasks |
| Validate | `validation-task` | the report, for the version's built scope |

`prd-contract` defines every file and line; `grilling` owns the questions.

## Where each part lives

- aep-api: `services/aep-api/design/requirements-model.md`.
- console: `apps/console/design/spec-and-design-from-the-room.md`.
- The shared fixture both readers are held to:
  `packages/contracts/requirements/acme-expenses`.
- Evals: `evals/spec-agents` (requirements, design, tasks; a scenario with an
  attached document, and a per-feature design fixture).
