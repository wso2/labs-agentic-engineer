# ADR-0022 — Roles and test users are shared directory objects the BFF ensures at build

Status: accepted. Revisited: one `security.json` instead of a two-file split;
the identity model is unchanged. **Scope amended 2026-09-05** — see below.

> **Amendment · 2026-09-05 · the sharing scope is an ENVIRONMENT, not the
> cluster.** The decision below stands in every part: one `security.json`, the
> BFF ensures directory objects at build with no model in the loop, a row is the
> ownership marker, additive only, passwords sealed and published on the gate
> ticket. What changed is WHERE those objects live. AEP now runs a second
> identity tier — one ThunderID per `(org, environment)`, provisioned by
> `deployments/scripts/setup-environment-thunder.sh` and recorded on the
> OpenChoreo Environment as `aep.wso2.com/thunder-*` annotations — and a build's
> roles and test users are created on the identity provider of the environment
> that version is validated in, never on the platform IdP. The platform IdP is
> neutral infrastructure ([ADR-0028](ADR-0028-the-platform-idp-is-neutral-infrastructure.md))
> and holds no roles or test users at all.
>
> Read "cluster-wide" below as "within one `(org, environment)`". Concretely:
>
> * `idp_roles` and `test_users` are keyed by `(org_id, environment, name)` and
>   `(org_id, environment, username)`; `test_user_refs` gains `environment`.
>   Migration `phase15_identity_per_environment` DELETES the platform-IdP era's
>   rows — they name group and user ids on an instance no build provisions to,
>   and a row is the ownership marker, so a backfill would claim ownership of
>   objects that are not there. The next build's ensure recreates them on the
>   right directory and republishes the logins.
> * **Cross-org role visibility is CLOSED**, which is the resolution the
>   Consequences section below anticipated. `list_roles` reads the catalog of
>   the caller's own `(org, default)` directory, and the panel's
>   "referencing projects" column and its delete warning are now one query — an
>   account exists on exactly one org's directory, so every project that can
>   reference it is that org's.
> * **Cross-ORG password publication is closed too**; the shared-account edge
>   below now bounds to one org's projects in one environment. The remedy the
>   last paragraph of that item proposes — scoping the account name — is still
>   the fix if per-project isolation is wanted.
> * A published login now names its issuer. The gate comment says "Sign in at
>   `<issuer>` — the identity provider of the **<env>** environment", because a
>   username and password alone name no sign-in once there is one provider per
>   environment.
>
> The mechanism is documented where it is enforced:
> [`internal/identity/README.md`](../../services/aep-api/internal/identity/README.md).

The validation agent judged role-gated acceptance criteria against `admin`/`admin`
with `mock: true` and a note saying user provisioning did not exist. That login
may not be a valid user of the generated app at all, so every such verdict was
worth nothing. A role matrix living only in markdown could not be parsed, so
nothing could act on it.

Roles and test users are **not project-scoped**. Their scope is the identity
provider's — cluster-wide while one Thunder serves the cluster, narrower when it
becomes namespace-scoped. Two projects naming the same role mean the same role,
and a person who holds it holds it everywhere. What a role may DO is per-project;
the role itself is not. Terms: [CONTEXT.md](../../CONTEXT.md).

## Decision

**One `security.json`. The BFF ensures directory objects from that file at
build. The Thunder application is a different resource, on a different clock.**

`specs/design/security.json` declares which roles the project uses, what each
may do within it, its test users, and the Thunder application client fields
registered at create. There is no prose companion: `security.md` is gone. How a
token becomes a Role is coding guidance, not a second design file.

**Roles and Test users — the Roles gate.** The BFF writes the directory
directly, through `thundersvc`, which already holds Thunder `Administrator`. It
runs synchronously inside `ProvisionForBuild`, resolved by its own `provision`
gate ("Provision roles and test users", `aep:gate/roles`), minted per version.
**No model is in the loop below the version tag**: a model authors
`security.json` and reads the `list_roles` catalog, and everything from the tag
down is deterministic code. These calls mint credentials.

Passwords are platform-generated and sealed with `secrets.ColumnCipher` under
`credential-encryption-key` — the same framing as `publisher_client_secret` —
because Thunder will not give a password back. `GET /users/{id}` returns no
password field, so without a sealed copy a credential could be issued exactly
once and never served again.

**The gate ticket delivers the credentials.** Before it closes, the gate posts a
comment carrying every test account this project can sign in as at that version —
username, password, role, cold-start flag — as a table anchored by an
`<!-- aep:test-users -->` marker. Its own comment, so the whole content is the
table and nothing around it can be misread as a row, and so that failing to
publish is separable from failing to close: the first fails the build, because
credentials that never arrived mean validation cannot sign in; the second only
leaves a gate the next build closes. That is where the validation agent reads its
login from. It is already reading its milestone's issues with the repo
credentials it was given, so the ticket costs it no second authenticated call, no
payload sitting on disk for the run's lifetime, and no inference from a spec
file. The ticket lists every account, not just the ones this build created:
keyed to what changed, a rebuild's ticket would list nothing and its validation
could sign in as nobody.

Two rules carry the safety, and both reduce to the presence of a row in the
platform's own tables: **it enrols members only into roles it created**, and **it
modifies only accounts it owns**. Thunder rejects custom attributes (400
USR-1019), so the row is the only ownership marker available.

Nothing is ever auto-deleted. A dropped role, a rename, a deleted project — the
directory object stands. The Security panel offers an explicit delete for a TEST
USER, and deliberately none for a role: a role is shared, outlives every project
that names it, and may hold real members this platform never created, so removing
one is an operator action on the identity provider rather than a console button.

**Thunder application — not this gate.** Sign-in is still an explicit
`thunder-app` platform resource on `design.json` (ADR-0006). Create reads the
`thunder` object from `security.json` at the version tag and registers a PKCE
client with the placeholder callback. The real redirect URI is a **web-app
deploy** step: after that app is serving, aep-api patches `<origin>/callback`
onto the Thunder application and waits until the CR shows it. That wait is the
deploy verdict, not a Task, not a GitHub issue, and not the Roles gate. Rebuilds
patch and wait again; create still skips when the resource is already Ready.

## Alternatives considered

**Frontmatter on markdown.** Measured, not assumed: every `.md` in the spec
workspace is seeded into a Tiptap collab editor with no frontmatter node, and a
round-trip flattens YAML nesting, escapes `[1, 2]` to `\[1, 2\]`, and turns `->`
into `&gt;`. `design.md`'s `sourceSpec` survives only because it is a flat scalar.
This is why the matrix is JSON, not YAML-in-markdown.

**Keeping the role matrix in prose.** Two copies of a role name can disagree with
no tiebreak, and the gate cannot parse prose — the platform would create
`Compliance Admin` while the document promised `Compliance Officer`.

**A prose companion beside the JSON (`security.md`).** The file had no parser, no
save/build/ensure check, and the coding skill never opened it — role resolution
already lives in coding guidance. A second document that nothing machine-reads is
a place for drift, not a safeguard. Deleted. No policy object on `security.json`
in its place.

**Project-namespaced group names.** It defeats reuse, which is the entire point
of showing the design agent the existing catalog.

**Extending the `ThunderApplication` CR.** It is per-project with a delete
finalizer, so tearing down one project would delete directory objects others
share. New cluster-scoped CRs were rejected too: they add CRDs and make the
generated password async to retrieve.

**Folding the ensure into the existing `thunder-app` gate.** Traced, not assumed:
build preflight skips a dependency already `Ready`, so on every rebuild
`provisionResource` is never called and `settleReadyGates` re-authors nothing. A
role added in v2 would never be created. The roles gate is therefore driven by
the design at the tag, not by the drawer inputs. The same skip is why Thunder
**create** is not the place that waits for the real callback — that wait is web-app
deploy, every time.

**A URI Task or GitHub issue for the callback.** Planning and validation would
mint work that is not agent work. The platform already patches the binding
(ADR-0006); making “deployed” wait until the Thunder application CR carries the
real URI is the same moment, not a second tracker.

**Reset-on-demand with nothing stored.** It makes a read destructive: the moment
validation asks, a password a human is holding stops working, and a runner asking
twice invalidates its own first answer.

**Refusing the build when a role has no test user.** It trades a real blocked
build for a documentation nicety the platform can obviously handle itself. The
build supplies `test-<role-slug>` instead, flagged in the panel.

**Refcounted reclaim of shared objects.** It needs a cross-project reverse index
correct through untagged edits and deleted projects, and getting it wrong deletes
a live account mid-validation.

**A platform callback the runner asks for a password.** Built first, then
removed: `POST /internal/v1/validation/{cycleID}/test-credentials` served a
shared `admin`/`admin` stand-in, was briefly wired to the real accounts, and is
now gone along with its contract path. Wiring both it and the ticket makes two
published copies of one password, free to disagree the moment somebody rotates
one from the panel; wiring only the callback costs the agent an extra
authenticated hop plus a roster handed to it separately just to know which role
to ask for. The ticket answers both halves at once — which accounts exist, and
what they sign in with — from a source the agent already reads. Leaving the
endpoint in place with no caller was the third option, and it is the worst: a
reachable credential endpoint nobody owns, invisible to the dead-code gate
because the router reaches it.

**A REST endpoint writing `security.json` directly.** It introduces a second
writer to committed truth while a room is open — precisely the race room-mode
exists to prevent. The panel patches the room's `Y.Text`; the committer lands it.

## Consequences

**Cross-org role visibility.** With one IdP serving the cluster, `list_roles`
shows one org's design agent the role names another org created, and role names
can carry business meaning. Accepted; it resolves when the IdP becomes
namespace-scoped. The panel's "referencing projects" column is org-fenced, and
the cross-org figure is a bare count with no names.

**Standing credentials accumulate.** Additive-only means a test user outlives its
project and keeps a working password. The panel makes the set visible — role,
referencing projects, last rotation — but reclaim is a human action.

**A failed ensure fails the build.** The roles gate reports a `ProvisionFailure`,
which `build_adapters.go` collapses into one error, so the run settles at
planning rather than waiting on a hold that may never lift. That is stronger
than holding dispatch: nothing downstream starts. The gate issue is left open
carrying the error, and the next build's ensure — idempotent — closes it.

**A published password is readable by whoever can read the repository.** The
credentials sit in an issue comment in the project's own repo, in that issue's
history, and in whatever GitHub notification carried it; editing or deleting the
comment does not unpublish it. Accepted, and bounded by what these accounts are:
platform-created, holding only that project's application roles, undeliverable
`@test-users.invalid` addresses, and never a real person's account — the ensure
refuses a username it does not own. Anyone who can read the ticket can already
read the repository the same agent pushes to. The ticket and the panel both say
so in as many words.

That bound is enforced, not assumed. GitHub delivers every comment the platform
posts straight back as an `issue_comment` webhook, and the receiver persists each
verified delivery's raw body to `webhook_payloads` for audit — so publishing
would otherwise have copied every password into the platform's own database in
cleartext, cross-org, with no retention sweep and no reader. The receiver
redacts a body carrying the marker before storing it
(`sourcecontrol/webhook/redact.go`), keyed on the same constant the writer
anchors on. The database is the one place in this service where every other
credential is sealed, and a published test-user password does not reach it.

**A shared test user's password is published in every project that references
it.** Test-user names are derived from the role name (`test-<role-slug>`), and
roles are shared, so two projects declaring `Viewer` reference the SAME account —
and each one's gate ticket publishes its password into its own repository. With
one IdP serving the cluster that crosses org boundaries: a reader of org A's repo
holds a login org B's project also uses. The account holds only application role
membership, and both projects' apps already trust the same directory, but this is
the sharpest edge of combining shared accounts with published passwords. The fix,
if it is wanted, is to scope the ACCOUNT to the project (`test-<project>-<role>`)
while keeping the ROLE shared — a change to how names are supplied, not to this
delivery mechanism.

**Rotating a password leaves the published copy stale.** The panel rotates against
the directory and reseals; the ticket already posted keeps the old value until
the next build publishes a fresh one. A rotation mid-run breaks that run's
logins, which is the same hazard rotation always had.

**Membership edits recreate the group.** Thunder sets group members only at
creation, so adding one is a delete-and-recreate with the merged list, serialised
per group name across a read-modify-write. The group id changes; nothing keys on
it, because both the token claim and the authz bindings use the NAME. It is not
atomic: a failure between the delete and the create leaves the group absent, and
the gate stays open so the next build recreates it.

**Web-app deploy is slower when a Thunder application is involved.** “Deployed”
includes the callback landing on the CR, not only OpenChoreo Ready. A failed
wait is a deploy failure. APIs are unchanged. Higher-environment callback union
is out of this decision.
