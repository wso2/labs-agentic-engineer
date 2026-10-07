# Pages, Cards and their routes

The console's screens are **Pages** with **Cards** over them (`CONTEXT.md`,
"Console"). Each is a route; the route tree is what draws one over the other.

```
routes/_dashboard/
  route.tsx                  the Dashboard, the org's base Page (pathless)
    index.tsx                  /                       no card
    settings.tsx               /settings?section=ai    Settings card
routes/skills/
  route.tsx                  the Skills Page          /skills
    $name.tsx                  /skills/go              Skill card
    new.tsx                    /skills/new             Skill card, new
routes/resources/
  route.tsx                  the Resources Page       /resources
    $name.tsx                  /resources/maps?project=p  Resource card
    new.tsx                    /resources/new          Resource card, new
routes/projects/$projectName/
  route.tsx                  the project: build picker host, build notes, ?chat=open
  _overview/route.tsx        the overview Page (pathless)
    index.tsx                  /projects/$p            no card
    spec.tsx                   /projects/$p/spec       Spec card
    design.tsx                 /projects/$p/design     Design card
    questions.tsx              /projects/$p/questions  Questions card
  builds/route.tsx           build history           /projects/$p/builds
    $version.tsx               /projects/$p/builds/v2  Build card
  validations/route.tsx      the Validation ledger   /projects/$p/validations
    $version.tsx               /projects/$p/validations/v2  Validation card
  deploy/route.tsx           the Deploy Page         /projects/$p/deploy
    $env/configure.tsx         /projects/$p/deploy/staging/configure  Configure card
  issues/route.tsx           the Issues Page         /projects/$p/issues
    $number.tsx                /projects/$p/issues/14  Issue card
```

- **A Page is a layout route.** It renders `PageWithCards` around its content:
  the page as the base layer (`BasePage`) and an `<Outlet />` for its Cards.
  The page stays mounted under an open card, covered by its scrim and inert.
- **A Card is a child route of its Page**. Closing it (X, Escape, the scrim)
  navigates to the Page it is over.
- **The org's Pages** are the Dashboard (pathless, below), Projects, Skills
  and Resources; Skills and Resources are layout routes like a project's
  Pages, each its own address with no leaf.
- **The overview and the Dashboard are pathless** (`_overview`,
  `_dashboard`), so their cards keep short addresses (`/projects/$p/spec`,
  `/settings`); each Page's own address needs its `index.tsx` leaf. The
  Dashboard's leaf is also where an org with no projects is sent on to New
  project, so that only happens at `/`, never under Settings.
- **Every card is drawn by `CardFrame`** (`features/shell/components/`): the
  scrim, the frame, the header and close (X, Escape, the scrim), told where
  closing goes. A project's cards go through `CardOverlay`, which reads that
  from the tables below; an org card (Settings, a Skill, a Resource) closes
  to its Page itself.
- **`features/shell/scope.ts` holds the tables**: which route IDs are Pages
  (`PAGE_ROUTES`, and `ORG_PAGE_ROUTES` for the org's), which are Cards
  (`CARD_ROUTES` and `ORG_CARD_ROUTES`, both read by `cardOfRoute`), and the
  Page each Card is over (`pageOfCard`; `ORG_CARD_PAGE`). The rail's active
  item, the chat's breadcrumb, the Turn scope and `CardOverlay`'s close all
  read them, so a new Card is a route file plus its rows there; a new project
  Page also needs its path in `CardOverlay`'s `PAGE_PATH`.
- **A card with a draft guards it**: `LeaveGuard` (`features/shell/`) asks
  before any navigation off the card while the draft is dirty (close,
  Escape, the scrim, the rail, back) and before a reload. Leaving on purpose,
  after a Save or a Delete, navigates with `ignoreBlocker`.
- **A Panel is not a route**: a Dialog owned by its Page or Card (the build
  picker, Try it, Delete project, Settings' Rotate token and Disconnect,
  Skills' Import, a Skill's Delete, a Resource's Delete and Promote to
  organization). It
  has no address and leaves the chat as it was. Try it has two owners: the
  Deploy Page opens it for an environment, every component serving there; the
  overview's components open it for one component, on the first environment
  it serves in (where its newest version runs, `componentTry`), with a switch
  to the others. Delete project is opened from the overview's menu.

A Build card is a version's, over build history (the ledger of versions);
closing it goes back there. The overview's track opens the newest version's
Build card from its Build leg, and the build picker opens the version it
started. A Validation card is a version's too, over the Validation ledger:
its attempts, the chosen one by feature, Fix and Revalidate. The Build card
links to it, and a failing build's next step is "See what failed", there.
Neither sets a Turn scope: no agent works on one build or one validation yet,
so the chat stays on the whole product.

The Configure card sets no Turn scope: no agent can change an environment yet,
so `turnScopeFor` and `chatTopic` read it as the whole product. The Settings
card sets none either: it is the org's, and the org's chat is inert. It is
opened from the rail (the Settings icon above the user menu), not from the
Dashboard, and keeps its section in the address (`?section=github|ai|usage`).

The Issues Page lists the project's GitHub issues (incidents the SRE agent
filed, the platform's own, people's), those that need attention first; an
Issue card is over it and sets no Turn scope either. The Dashboard's Alerts
link straight to Issue cards: they are every project's issues that need
attention (`features/issues/useAlerts.ts` asks each project, as no read
answers for the org), and the rail's logo counts those that need a person.

The Skills Page lists the org's skills (`features/skills/`): search, a kind
filter, the Disabled and Update to review tags; Import (a Panel for an
AgentSkills tarball) and Take updates (N), which refreshes every skill the
platform moved and the org never edited (the `update` state of
`/skills/updates`). A Skill card edits one skill as one draft: the name
(typed once, for a new skill; fixed after), the one-line description and the
markdown body in a Tiptap editor with the Spec editor's look (`proseSx`) and
no room. Save writes the whole SKILL.md: the frontmatter is never put through
the editor, only its `description` line is rewritten, and a body nobody
edited goes back byte for byte (`model/skillDraft.ts`). New skill is the same
card empty at `/skills/new`; its first Save creates the skill (so a skill
cannot be named `new`). A skill in conflict (both the org and the platform
changed it) is tagged and says so; reviewing the platform's version needs the
API to return it, which it does not yet. The card sets no Turn scope: there
is no skill agent yet, so the org's chat stays inert beside it.

The Resources Page is one list of everything a project can depend on
(`features/resources/`), filtered by kind: Platform (the platform's resource
types), External (the organization's Registered External resources, and the
Project External resources each project holds, tagged "In <project>") and
From projects (other projects' endpoints, which have no Page of their own).
Names collide across kinds and projects, so a Resource card's address keeps
the kind and project in its search where the name alone is not enough
(`model/resources.ts`, `resourceAddress`): none for a registered resource,
`?project=` for a project's own, `?kind=platform`, `?kind=endpoint&project=`.
A registered resource's card is the one the organization edits, one draft and
one Save (register at `/resources/new`, update after; its resource docs go
back untouched, since an update without them drops them); a platform type and
an endpoint are read-only; a project's own resource is read-only with Promote
to organization, a Panel for the consumption instructions and any values not
carried over from the project, after which the card opens on the registered,
editable record. No Resource card sets a Turn scope (there is no resource
agent). The Design card's component view links each dependency that reuses a
registered resource to its card (`registeredResourcesOf`, from the
dependency's `resource.ref`).
