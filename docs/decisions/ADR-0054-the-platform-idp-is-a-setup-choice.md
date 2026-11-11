# ADR-0054 — The platform IdP is a setup choice: ThunderID or an Asgardeo organization

**Status:** Accepted · 2026-11-11
**Amends:** [ADR-0027](ADR-0027-one-cluster-one-platform-idp.md) ("installed
unconditionally"), [ADR-0028](ADR-0028-the-platform-idp-is-neutral-infrastructure.md)
(who administers the platform IdP), [ADR-0029](ADR-0029-environment-identity-is-bound-by-a-record.md)
(an environment IdP's trust of the platform IdP) and
[ADR-0047](ADR-0047-an-org-secrets-value-lives-only-in-vault.md) (decision 3,
for two secrets).
**Detail:** `services/aep-api/internal/platform/auth/README.md` (the token
dialect and the one-org mapping), `services/aep-api/internal/organization/README.md`
(`StaticOrgClients`), `tools/aectl/AGENTS.md` and `deployments/README.md`
("Install with Asgardeo as the platform IdP"),
`deployments/scripts/ae-asgardeo-apps.sh`.

## Context

AE's platform IdP was ThunderID, installed in the cluster and administered
by AE: `aectl` registers AE's OAuth clients in it, and aep-api creates two
more per org (the publisher client a coding run signs in as, and the AE
Studio client the org's pod signs in as).

A deployment target runs its identity on Asgardeo. There the IdP is a
service outside the cluster, owned by someone else, and shared with other
applications. AE cannot be given the right to create or reset clients on it,
and nothing in the cluster can be installed "for" it.

ThunderID's claim names and admin API were written into four token readers
(aep-api, the pod's Go and TypeScript readers, the console), into aep-api's
client lifecycle, into `aectl` and into the chart.

## Decision

1. **One install has one platform IdP kind, chosen at setup.** `idp.kind` is
   `thunder` (the default) or `asgardeo`. The kind is not changed on a
   running install: an org's secret paths are derived from the IdP's id for
   the org.

2. **Every token reader reads the IdP's claims through a token dialect.** A
   dialect is the claim names and rules one kind uses to say who a caller
   is. ThunderID names the org in `ouHandle` / `ouId` / `ouName` and the
   grant in `grant_type`. Asgardeo names the org in `org_handle` / `org_id`
   / `org_name` and who a token was issued to in `aut`. A token the dialect
   cannot place as a user's or a machine's is refused on user routes and on
   machine routes alike, and a token of one kind read by the other's dialect
   names no org.

3. **An Asgardeo install serves one org.** An Asgardeo token names its org
   by the organization's handle, which is not the org's OpenChoreo
   namespace. aep-api maps the configured handle to the configured
   namespace in its dialect; a token of any other org reads as no org. The
   org's pod is given the IdP's handle as its own, since it uses the handle
   only to check a token's org and to name its Rooms, which the console
   names from the same token.

4. **AE holds no management rights on an Asgardeo organization.** Its admin
   creates AE's five applications once, with a script run under the admin's
   own credentials, and imports the four client secrets into the vault.
   `aectl` asks for no admin secret, generates no client secret and
   registers no client; it requires the imported secrets before it installs.
   aep-api's `OrgClients` is static: it serves the org's two pre-created
   clients and never calls the IdP. A write on one (set its secret, delete
   it) is refused.

5. **On Asgardeo aep-api holds the org's two client secrets as settings.**
   This amends ADR-0047's decision 3 for the publisher and AE Studio client
   secrets only. They reach aep-api the way its own client secret does
   (vault, ExternalSecret, Secret, env), and aep-api writes them into the
   org's references on GitHub connect as it writes every org secret. The
   readers of those references, the coding run and the org's pod, still
   take keys from the reference and never a value. ThunderID installs are
   as ADR-0047 has them.

6. **The second tier stays ThunderID.** The per-environment IdPs of
   generated applications (ADR-0029) are installed on either kind. On
   Asgardeo they are not told to trust the platform IdP: that trust serves
   Agent Manager, which installs on a ThunderID platform only.

7. **What an Asgardeo install does not have.** No ThunderID is installed
   for the platform, so ADR-0027's "installed unconditionally" holds for
   kind `thunder`. Agent Manager and `aectl sre install` are ThunderID
   only. OpenChoreo's own clients, and its trust of the same organization,
   are the cluster owner's.

## Consequences

- A ThunderID install is unchanged: the same chart render, the same
  `make dev-env`, the same config keys.
- Who may sign in to an Asgardeo install rests on the Asgardeo
  application's own access settings. AE refuses a token of another
  organization, and does not yet refuse a user of the organization by
  group.
- A client secret is rotated in Asgardeo, imported again, and reaches the
  org when aep-api is restarted and GitHub is reconnected. aep-api cannot
  rotate it.
- aep-api holds two narrow client secrets on Asgardeo in place of the
  ThunderID admin client's secret, which can create or reset any client.
- The local Asgardeo dev-env leaves out what signs in with a client that
  exists only in ThunderID: OpenChoreo's portal, the observability plane,
  Agent Manager, the SRE agent, and the last step of a build (OpenChoreo's
  workload publisher client).
- The settings that carry the IdP's endpoints keep their ThunderID names
  (`thunder.jwksURL`, `console.thunderPublicURL`, the vault path family
  `aep/thunder-clients/`).

## Alternatives rejected

- **AE registers its clients on Asgardeo through the management API.** It
  works (a caller-chosen client id and secret are honoured), and fits the
  same `OrgClients` port. It needs application-management rights on a
  tenant AE shares with other applications, which this decision avoids.
- **Many orgs on one Asgardeo organization.** Asgardeo's sub-organizations
  could carry them. No install needs it, and the one-org mapping keeps the
  org model unchanged.
- **Mapping the handle in every reader.** The pod and the console would each
  need the organization's handle and the namespace as new settings. Giving
  the pod the IdP's handle needs none.
- **Adopting the imported secret in place**, so aep-api never sees it. It
  needs a second write path through the org secret writer and `aectl`
  creating the org's secret references. Decision 5 uses the path every org
  secret already takes.
