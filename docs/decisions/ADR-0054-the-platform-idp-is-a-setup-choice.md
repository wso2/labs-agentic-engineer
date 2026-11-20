# ADR-0054 — The platform IdP is described by configuration, not by a kind

**Status:** Accepted · 2026-11-11 · rewritten 2026-11-19 · the value
`idp.clients: asgardeo` added 2026-11-20. The first version
chose the IdP by a kind (`idp.kind`: `thunder` or `asgardeo`) that code
branched on
**Amends:** [ADR-0027](ADR-0027-one-cluster-one-platform-idp.md) ("installed
unconditionally"), [ADR-0028](ADR-0028-the-platform-idp-is-neutral-infrastructure.md)
(who administers the platform IdP), [ADR-0029](ADR-0029-environment-identity-is-bound-by-a-record.md)
(an environment IdP's trust of the platform IdP),
[ADR-0046](ADR-0046-ae-studio-checks-platform-idp-tokens-itself.md) (decision
2, the AE-only token's org claim) and
[ADR-0047](ADR-0047-an-org-secrets-value-lives-only-in-vault.md) (decision 3,
for two secrets).
**Detail:** `services/aep-api/internal/platform/auth/README.md` (the token
dialect and the one-org mapping), `services/aep-api/internal/organization/README.md`
(`OrgClients`), `tools/aectl/internal/idpclaims` (the presets),
`tools/aectl/AGENTS.md` and `deployments/README.md` ("Install with Asgardeo
as the platform IdP", "Use another IdP"),
`tools/aectl/internal/asgardeo` (the management API client),
`deployments/scripts/ae-asgardeo-apps.sh`.

## Context

AE's platform IdP was ThunderID, installed in the cluster and administered
by AE: `aectl` registered AE's OAuth clients in it, and aep-api created two
more per org (the publisher client a coding run signs in as, and the AE
Studio client the org's pod signs in as).

A deployment target runs its identity on Asgardeo. There the IdP is a
service outside the cluster, owned by someone else, and shared with other
applications. No running part of AE can be given the right to create or
reset clients on it, and nothing in the cluster can be installed "for" it.

ThunderID's claim names and admin API were written into four token readers
(aep-api, the pod's Go and TypeScript readers, the console), into aep-api's
client lifecycle, into `aectl` and into the chart. Adding Asgardeo as a
second kind put a branch at each of those places, and a third IdP would have
added a third arm to every one.

Two separate things differ between IdPs, and the kind tied them together:

- how a token names the caller's org and says whether it was issued to a
  machine or to a user. No standard defines either;
- who creates AE's clients on the IdP.

## Decision

1. **No code branches on which IdP the platform IdP is.** The two
   differences are two independent settings, `idp.claims` and `idp.clients`.
   Nothing else about the IdP is configured: its endpoints come from its OIDC
   discovery document.

2. **The token dialect is configuration: `idp.claims`.** It names the claim
   that carries the org's handle, the one that carries the IdP's id for the
   org, optionally one for its display name, the claim that says who a token
   was issued to, its value for a machine and optionally its value for a
   user. Each is a top-level text claim.
   - Every reader of the IdP's tokens is given the values and knows no IdP.
     The mandatory four have no default in any reader: one that is missing
     stops aep-api and the pod's containers at start. The console reads one
     value, the name of the handle claim. A built console fails at load
     without it, and its image does not start unless the config file that
     is served names it. The console's local dev server keeps a default for
     it, the dev ThunderID's, as it does for its other settings: the console
     has no dev config file.
   - With a user value set, any third value is refused. With none, any value
     other than the machine's is a user's, which is what ThunderID needs: it
     has several user grants.
   - A token the dialect cannot place as a user's or a machine's is refused
     by both kinds of route. Nothing stands in for the org's handle.
   - Claims a standard names are read by their standard names on every IdP
     (`sub`, `aud`, `iss`, `exp`, `client_id` as in RFC 9068, `name`,
     `email`). They are not in the block.
   - `aectl` ships the values for the IdPs AE has been run against as
     presets, `thunder` and `asgardeo`, and layers the block: the preset
     (`thunder` when unset), then the config's keys. It prints each value
     with its source, because a value changes which tokens count as a
     machine's. `preset: none` starts from nothing, so that a config for
     another IdP cannot inherit a name it left out. Only `aectl` knows a
     preset's name. A claim value is not taken from the environment: `aectl`
     stops when an `AEP_IDP_CLAIMS_*` variable is set.

3. **Who creates the clients is its own setting: `idp.clients`.** `thunder`
   (the default): `aectl` registers them on a ThunderID it administers and
   generates their secrets. `imported`: the IdP's admin created them and
   imports their secrets, and `aectl` administers nothing on the IdP.
   `asgardeo`: `aectl` creates them in an Asgardeo organization through its
   Application Management API and generates their secrets. This is the one
   place a way of creating clients is named, because creating a client goes
   through each IdP's own admin API.

   With `asgardeo`, the organization's admin gives `aectl` the credential of
   an admin application for one command (`platform install`,
   `platform sync-clients`), in two files. `aectl` holds it in memory for
   that command. It is not stored in the cluster or the config, so no
   running service has management rights on the organization. The admin
   application needs the scopes `internal_application_mgt_view` and
   `internal_application_mgt_create`, and
   `internal_application_mgt_client_secret_create` to replace a secret.

   Asgardeo's API sets how a secret moves. An application is created with
   the secret `aectl` generated, so the vault is the source, as on
   ThunderID. An application that exists does not show its secret and
   cannot be given one (an update with a secret is refused). So `aectl`
   checks a secret by asking the organization for a token with it. When the
   token is refused, Asgardeo can only regenerate the secret: it chooses the
   new value, the old one stops working at once, and `aectl` writes the new
   value over the vault path. `aectl` does that only with
   `--replace-client-secrets`, because the client ids are fixed: two
   installs that share one organization would otherwise take each other's
   secrets away without a warning. An application that exists is never
   rewritten: `aectl` reports each setting that is not what AE needs and
   stops.

   A regenerated secret reaches a pod only when the pod starts, and a later
   run cannot see from the vault that a pod still holds the old one. So
   `aectl` records on each Deployment a checksum of the client Secret its
   pods started with, and every run restarts a Deployment whose checksum is
   no longer the Secret's. A run that failed after the write to the vault
   is so completed by the next run.

4. **aep-api administers no IdP.** The org's two clients are created before
   aep-api runs, by `aectl` or by the IdP's admin, and aep-api is given
   their secrets as settings. It writes them into the org's references on
   GitHub connect (this amends ADR-0047 decision 3 for these two secrets) and
   never creates, changes or deletes a client. It holds no IdP admin
   credential. The two secrets so have copies in the control plane: in the
   vault (`aep/idp-clients/`), in the cluster Secret that aep-api reads
   (`aep-org-client-secrets`) and in aep-api's environment. That is accepted
   for now. aep-api reading them from the vault at GitHub connect would
   remove the last two copies (issue #975).

5. **An install serves one org.** The org is named at install by the handle
   the IdP's tokens carry (`idp.org_handle`) and by its OpenChoreo namespace.
   aep-api maps one to the other, and a token of any other org of the IdP
   reads as no org. The pod is given the IdP's handle, so the pod and the
   console need no mapping.

6. **The pod's gate for aep-api's install-wide client does not read that
   token's org claim.** The client is install-wide: the `X-Impersonate-Org`
   header says which org a call is for, and the gate pins the caller by
   signature, issuer, audience, `client_id` and caller type. What an IdP puts
   in the org claim of a client outside any org is the IdP's choice
   (ThunderID: nothing; Asgardeo: the organization), so reading it needed a
   rule per IdP. This amends ADR-0046 decision 2.

7. **The second tier stays ThunderID.** The environment IdPs are still
   ThunderIDs AE installs and administers. They are told to trust the
   platform IdP only where it is a ThunderID `aectl` administers, because
   that trust serves Agent Manager, which needs its clients registered
   there.

## Consequences

- An IdP other than ThunderID or Asgardeo is configured, not coded: its
  admin creates the five clients (`idp.clients: imported`), and the config
  names its claims. That is
  configurable, not supported: AE has been run against two IdPs, and what a
  third needs beyond its claim names is only known for those two.
- The IdP has to state the org and the caller type in top-level text claims.
  One that does not is configured to add them, or cannot be used.
- Adding an org is not automatic on any IdP: the install names the one org.
  On ThunderID this replaces onboarding at first sign-in, for one org.
- Rotating an org client's secret: change it at the IdP, store it again,
  restart aep-api, reconnect GitHub. aep-api cannot rotate it.
- aep-api no longer checks that an org id from a token exists at the IdP: it
  has no admin client to ask. The token is the IdP's statement, and under
  the one-org mapping it names the install's org. A token issued before the
  org was recreated at the IdP can put the old id back until it expires
  (issue #974).
- With `idp.clients: imported` or `asgardeo` an install lacks what needs
  clients `aectl` registers on a ThunderID: the in-cluster ThunderID itself,
  Agent Manager, and `aectl sre install`. With `asgardeo`, `aectl` creates
  AE's five clients only. OpenChoreo's own clients in the organization are
  the cluster owner's.
- With `idp.clients: asgardeo`, a new cluster on an organization that has
  the applications starts with an empty vault, so its install needs
  `--replace-client-secrets`. The secrets the earlier cluster held then
  stop working.
- Settings and stored names every IdP has carry the IdP's role and not a
  product's (`idp.issuer`, `idp.jwksURL`, the vault path family
  `aep/idp-clients/`, the Secret `aep-idp-client-secrets`). Names that
  describe a ThunderID AE administers keep ThunderID's name (`thunder.*`,
  `aep-thunder-admin-creds`).
- Three names still say "thunder" for a value every IdP has, because each
  is a wide rename left for its own change (issue #972): the organization
  row's `thunder_org_uuid`, which holds the IdP's id for the org; the
  console's `VITE_THUNDER_*` settings; and "Thunder OU" in some of aep-api's
  error text.
- aep-api keeps no IdP entity id of a client: the two columns that held one
  are dropped. It records the client ids only.
- An install from before this decision is reinstalled, not upgraded.

## Alternatives rejected

- **A kind per IdP, with a branch at each place it matters.** The first
  version of this decision. Every new IdP is a code change in four readers,
  the installer and the chart, and the kind hides that reading tokens and
  creating clients are separate questions.
- **A separate claims file.** `aectl` has no second file: its config already
  holds a block per concern and validates it. A snippet of that config is as
  easy to share.
- **An environment variable that overrides a claim value for one run.**
  Built, then removed. The value is not stored, so an install with the
  variable followed by an update without it sent the stored value to the
  chart, and every reader then read a claim that the tokens do not carry.
- **`idp.clients: managed` in place of `thunder`.** The chart has to know
  that the IdP is a ThunderID `aectl` administers (its CORS policy, its
  admin credentials), and creating clients is specific to an IdP's admin
  API. One explicit value says both, with no guess from which config block
  is filled.
- **One JSON value carried to the readers.** Two hops insert the value into
  text (the console's start script, the pod's template), and JSON there
  needs escaping that a plain value does not.
- **aep-api registering clients on Asgardeo through the management API.** It
  works (a caller-chosen client id and secret are honoured), but a running
  service would hold application-management rights on a tenant shared with
  other applications. `aectl` creates them instead, with a credential for
  one command (decision 3).
- **Replacing a mismatched Asgardeo secret without a flag.** It makes a new
  cluster install in one step, and lets a second install on the same
  organization break the first without a warning.
- **`aectl` deleting and recreating an Asgardeo application to give it a
  chosen secret.** It keeps the vault as the only source, but it needs the
  right to delete applications, and it removes whatever the admin set on
  the application by hand.
- **Keeping client creation on demand in aep-api for ThunderID.** It needs
  the admin credential in a running service and a second path beside the
  pre-created one.
- **Many orgs per install.** Possible on the same design (a mapping, a pair
  of clients and an "add an org" step per org), and not needed yet. On
  Asgardeo it needs sub-organizations, which are untested.
- **Mapping the handle in every reader.** The pod and the console would each
  need the handle and the namespace as settings. Giving the pod the IdP's
  handle needs none.
