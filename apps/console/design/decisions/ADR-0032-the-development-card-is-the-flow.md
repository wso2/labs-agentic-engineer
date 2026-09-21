# ADR-0032: The Development card reads as the flow — deployed, validated, promoted

- **Status:** Accepted
- **Date:** 2026-09-14 (the Deployments Try-it Flow design, turn 9, artboards
  9a–9c; feature
  [#775](https://github.com/wso2/labs-agentic-engineer/issues/775))
- **Amends:** [ADR-0027](./ADR-0027-deployments-is-an-environment-board.md) —
  decision 1 (the Development card's contents) and decision 6 (the
  Connections card under the ledger, and the Test users panel on the card).
  Decisions 2–5 stand: the ledger, the environment page, "a row is what the
  environment runs NOW", and no contract surface.

## Context

ADR-0027's Development card stated its facts as a pile: the running line,
the verdict banner, the promote button, the test users. Every fact was true
and the card still did not answer the question a reader has the moment a
build merges — *what do I do now, what is still happening, and what comes
next?* The Try-it flow design answers it by drawing the card as the path the
version is on: **deployed → validated → promoted**, top to bottom, with the
one action that matters at each step. It also surfaced a state the page had
no words for: a deployment **on hold** for a connection value, which today
only the Builds page names.

## Decisions

1. **The Development card is a vertical rail of three numbered steps.**
   *Deployed* holds the rollout sentence, the components and the
   connections as two grouped lists, and a primary **Try it now** that opens
   the environment page. *Validation* holds the shared verdict sentence
   (`verdict.ts`, the Validation tile's own words) and a status chip with the
   last known counts; nothing started reads *Runs automatically after
   deployment*. *Promote to Production* holds the promote button with its
   reason, and — once validation allows — one blocker line per connection
   still missing a production value, with Configure inline. A step's mark
   says where the version is: a check for done, a ring for the active step,
   grey for not yet. The rail replaces the banner-plus-button pile; the
   facts are the same facts.

2. **A deployment on hold is said on the Deployments page.** The newest run
   parked at the deploy gate (`MilestoneRunView.state = waiting`,
   `waitingReason = external-values`) is the one read that says a deployment
   is waiting on a value. When it does, the card's chip reads *Waiting for
   configuration*, step 1 reads *Deploy · On hold* with a notice naming the
   value and the component that depends on it (from the design-dependencies
   read the page already makes), Configure opens the development values
   dialog, Try it now is disabled, and steps 2 and 3 are inactive with a
   one-line reason each. Nothing new is fetched for it: the run story is
   `useBuildRuns` on the build version, which the validation evidence hook
   reads already.

3. **Connections live on the cards; the Connections card is retired.** Each
   environment lists its own connections — *Set*, *Provisioned*, *Missing*
   with Configure — beside the components they serve, so the thing that
   blocks a step sits on the step. Development's Configure re-collects dev
   values (the #395 surface, unchanged); Production's Configure opens the
   promote dialog, which is where production values are entered and where
   they stay — page state, because the contract has no promote surface
   (ADR-0027 decision 5 still holds). Promote is disabled until every value
   is set.

4. **The environment page is where a version is TRIED.** *Try it now* is
   captioned "Opens the deployment view: app, endpoints, test users", and the
   page's Components card becomes **Try it out** (the Deployment Detail
   design, turn 1): every component as a panel a person can act on, the web
   applications first — they are what a person opens. A web
   application is visited, carries its URL with a copy control, names the
   components it talks to (the design's component edges), and holds the
   **test users** that sign in to it — inline, filterable, folded past five
   rows. A service lists its **endpoints** off the contract the platform
   serves (`get-component-openapi`, parsed by `@aep/ui-openapi-view`):
   searchable, filterable by method, a **curl** per row for the deployed URL
   with an `Authorization: Bearer <token>` placeholder, and *Try* into the
   contract viewer. The **Connections** table sits on the version page, not
   here — what a version runs it with belongs beside what it runs: each
   dependency with its type, who uses it, its config keys — masked, always,
   because nothing reads a value back — and the readiness word; *Edit*
   re-collects a Project External's development values, and a
   platform-provisioned connection carries no action of its own. Production
   shows the same table with no Edit: nothing collects values there.

5. **What the designs drew and this does not build.** On the board: a
   *Past deployments* row (no deployment record — ADR-0027 decision 4,
   unchanged), a validation ETA and a live progress bar (the card would read
   a second stream for a summary the Validation page already gives),
   per-service endpoint counts on the card (the page counts them, where the
   contract is read anyway). On the environment page: a health line with a
   latency (nothing probes), *Get token*, *Open app as*, a token picker and
   *Run* (the platform mints no token for a test user), a connection's
   VALUE (nothing reads one back), and *Compare with Production* (production
   values are the promote dialog's page state). Each is left out rather than
   faked; the token surfaces are a backend handshake if wanted.

6. **Review round (2026-09-15, on the local plane).** Cards keep the
   components list's own order — the platform's, which is the design's —
   rather than a name sort (the API was landing above the app it serves).
   An empty Production card stays an empty state: the sentence and the gate,
   no lists of components that are not there. The Development card's
   connections carry a gear, not a pill — the group is a readout. A curl
   opens under its own row. Each step holds a skeleton for the read behind
   it (the design's connections and readiness, the validation evidence, the
   run story) rather than filling in one section at a time. And the build
   page's **Go to Deployments** becomes the primary action — a contained
   button — shown only once the aggregate reports a rollout of that version
   (live or converging): it is the way into this flow, so it must land on
   one. A merge alone no longer shows it.

   The second round, on the pull request: steps 2 and 3 are about the
   version the card names. The aggregate's `validation` answers for the
   BUILD version's milestone, so while a newer build runs, the deployed
   version's validation is read off its own run story (`deployedValidation`)
   rather than the newer version's word — the card labelled v1 no longer
   reads v2's verdict, nor offers or withholds v1's promotion on v2's
   account. A failed run-story read says so, with Retry, rather than drawing
   the ordinary state over a park it cannot see. A park that named no values
   is still said as on hold, without a count. The Production card's sentence
   carries the environment's own status word — *Running* only when it is.
   The environment page enables test users off the row's own status, so live
   bindings under a `none` aggregate count; its Connections table waits for
   the readiness read and names a failed one over the table, where Unknown
   is then the honest word; and a Registered External is matched by name
   regardless of case.

7. **Two pages under an environment, and a ledger of versions
   ([#779](https://github.com/wso2/labs-agentic-engineer/issues/779)).** The
   environment page split: **Try Out** at `/deployments/$env/try-out` (the
   panels, the endpoints, the test users, the connections — "Deployment Try
   Out", the project and environment under it) and **a page per deployed
   version** at `/deployments/$env/$version` — the milestone, when it was
   built, the commit, the verdict with its report, a link to the build, and
   what it runs there now when it is the live version, or the sentence that it
   was superseded and by what. The bare `/deployments/$env` redirects to Try
   Out. The board's ledger lists **every version that reached development**,
   newest first — *Deployed* (the live one, with the binding's stamp) /
   *Superseded* / *Building* / *Build failed* / *Cancelled* off the version
   ledger — and production's current row; each opens its page. Every fact is a
   read the console already makes (`list-builds`, `list-build-runs`, the
   component/binding join). ADR-0027 decision 4 still holds: no rollout dates
   are claimed for a past version, no Duration, no Redeploy — a deployment
   RECORD remains a backend handshake, and these pages gain its rows without
   being redone.

## Consequences

- `EnvironmentCards` becomes the flow; `ConnectionsCard` is mounted by
  nothing and goes. `DeploymentTryOutPage` (the former `DeploymentDetailPage`)
  composes `TryItOutCard` (the panels, the endpoints, the inline test users)
  and `ConnectionsTable`; `DeploymentVersionPage` carries the summary card;
  `versionLedgerRows` feeds the board's ledger;
  `ProjectSignInPanel` and `TestUsersDialog` stay for the row they share.
  `VerdictBanner` keeps its sentence and its link and moves inside step 2.
- The Builds page and the Deployments page now both say *on hold* from the
  same run row, so they cannot disagree about it.
- **No BE handshake.** The feature changes no contract.
