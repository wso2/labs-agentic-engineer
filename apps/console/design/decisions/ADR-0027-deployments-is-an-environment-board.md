# ADR-0027: Deployments is an environment board, and an environment's deployment is one click down

- **Status:** Accepted
- **Date:** 2026-09-03 (the Build-and-Deploy design handoff's deployment
  artboards, 1c and 1d)
- **Amends:** [ADR-0021](./ADR-0021-builds-is-a-version-ledger.md) — its
  *What this ADR does NOT cover* clause said Deployments keeps the one-story
  rail of [#401](https://github.com/wso2/labs-agentic-engineer/issues/401)
  unchanged. It no longer does. The clause's REASON stands, and shapes what
  this ADR builds: see decision 4.

## Context

The Deployments page was one numbered rail — Development → Validation →
Production — with a side panel restating the same facts. It answered "where is
my app on the path" but made the reader scan three expanded stages to find the
one that moved, and it put the environment's own facts (version, rollout, when
it last changed) in a panel beside the story rather than on the environment.

The design handoff drew the page as **two environment cards over a
deployments table**, and a **deployment detail page** with the components and
their runtime logs. ADR-0021 declined both because they need a deployment
RECORD — every deployment across every environment, failed and superseded
ones included — and the platform keeps none: an OpenChoreo release binding is
current state, overwritten on redeploy. That is still true. What this ADR
settles is how much of the design stands on the reads that DO exist.

## Decisions

1. **Deployments is an environment board: a card per environment, then a
   ledger.** The Development card carries the running version, the rollout
   count, the validation verdict (the same shared sentence the Validation
   page's tile reads) and the promotion; the Production card carries the gate
   ("only a version whose validation has passed can be promoted here") and how
   much of the live configuration a promotion needs is already set. The rail's
   Validation stage becomes the Development card's banner — validation is a
   check on the dev deployment, not a place of its own.

2. **The ledger has one row per environment that runs something, in the
   Builds ledger's own table.** Version · Milestone · Environment · Status ·
   Validation · Deployed, every row clickable. A converging row tints and its
   status dot pulses, the way a live build row does. Development's row exists
   once anything is bound there; Production's once anything is bound there; an
   empty board is the page's empty state, not a row reading *Nothing deployed*.

3. **An environment's deployment is its own page, keyed by ENVIRONMENT**
   (`/projects/$projectName/deployments/$environment`). A summary card
   (Deployed · Milestone · Validation · Commit, and *View the build that
   shipped this*), then the components running there — each with its release,
   its state and its way in: **Visit** for a web application, **Try API** for a
   service (the overview's in-app contract viewer), and its URL on a second
   line. The segment is the environment because that is the only deployment
   identity the platform keeps: there is exactly one deployment per environment
   to show, and no earlier one to name.

4. **A row is what the environment runs NOW, and the design's history has no
   source.** This is ADR-0021's reason, carried forward as the shape of the
   feature. The design drew superseded and failed past deployments, a
   Duration column, a Redeploy action, and a per-component runtime log with a
   time range and download. None of them has a read behind it:

   | Drawn | Why it is absent |
   |---|---|
   | Past deployments (superseded, failed) | No deployment record; bindings are overwritten on redeploy |
   | Duration | No rollout start/end is recorded anywhere |
   | Redeploy | No endpoint in the contract |
   | Runtime log | No workload-log read in the contract (`LogSurface` was built for it and waits) |
   | Production's version | `DeployStage.version` names the DEV version only |

   Each is left out rather than faked: a Redeploy button that does nothing and
   a Duration that is a guess would both be the console lying about the
   platform. When the record exists — a table and a writer that observes
   rollouts, a backend feature with a data-model decision under it — the
   ledger gains rows and the detail page gains a log section, and nothing
   here has to be undone.

5. **The board adds NO contract surface.** Every cell comes from a read the
   console already makes:

   | Cell | Source |
   |---|---|
   | Version, Status (development), Validation | `ProjectStatus.deploy`, already polled by the layout |
   | Status (production), live count, Deployed | the component/binding join (`list-deployments` per component) |
   | Milestone | `BuildSummary.milestoneNumber` off the version ledger |
   | Commit (detail page) | the version's newest merged cycle (`list-build-runs`, tag-scoped, DB-only) — `mergedCycle`, the same rule the build page's logs use |

   Development's Status is the deploy AGGREGATE's word when it has one — the
   same fact the Builds ledger reads as *Deployed to development* — so the two
   ledgers cannot disagree about the version running in dev. Production, which
   the aggregate never names, folds its bindings: any error → *Deploy failed*;
   any still converging → *Deploying*; all intentionally undeployed →
   *Undeployed*; otherwise *Deployed*.

6. **Connections keep their surface.** The side panel's Connections section —
   Configure on a Project External, *provisioned* and *platform-managed* on the
   rest — was the one thing the panel said that nothing else on the page does,
   so it becomes a card under the ledger. The Test users panel stays on the
   Development card, shown only when every component is live, as before.

## Consequences

- **The stage rail leaves Deployments too.** `StageRow` and `deploymentStory.ts`
  are mounted by nothing after this ADR; `deploymentStory.ts` and its tests go
  with the rail, since the stage derivations described a surface that no longer
  exists.
- **Redeploy, runtime logs and deployment history are a backend handshake, not
  a console follow-up.** The issue to open, when it is opened, is for the
  record and the log read; the console's shape is settled here.
- **No BE handshake.** The feature changes no contract, which is what lets it
  merge on its own.
