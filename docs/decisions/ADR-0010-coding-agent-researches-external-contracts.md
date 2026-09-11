> **Superseded in part by ADR-0027.** The contract a design commits to is a
> file in the dependency's own directory now, not a URL: the coding agent
> starts from that committed slice and researches beyond it. The rest of this
> record — research from the web for what the contract does not cover — stands.

# ADR-0010 — The coding agent researches external contracts from the web, not a stored, restricted spec

When a component depends on an external REST API or SDK, the coding agent needs
the provider's real contract to implement the client. The original design
handled this by **collecting the OpenAPI spec at design time, storing it in the
repo, and restricting the coding agent to exactly those operations** ("implement
against these EXACT operations; do not invent endpoints"), with the agent given
only server-side `WebSearch` and no `WebFetch`.

That model is brittle: many providers publish a docs page, not a fetchable
OpenAPI document; a spec stored once goes stale; and hard-restricting the agent
to a possibly-incomplete stored spec fights reality more than it helps.

## Decision

The coding agent **researches the provider's contract freely from the web**.
`WebSearch` (server-side, unchanged) plus `WebFetch` (newly enabled on the
runner pod) let it read the API/SDK's own docs and specs at build time. For a
`rest-api` external, `specPath` may be **a URL** (recorded as-is — not
fetched-and-stored) **or** a user-provided committed spec file; either way it is
a *source-of-truth hint*, not a cage — the `aep` skill and the resolved-
dependencies comment tell the agent to use it where it applies and research
the provider's docs for anything it does not cover. The design-time
store-and-restrict machinery is retired (the schema
fields `specUrl`, `sources`, and `candidates[].docsUrl` are removed; the
derived-state resolution of ADR-0003 stands, with `needs-spec` still gating a
`rest-api` that has neither a URL nor a file).

This relaxation is **external-only**. `org-service` dependencies keep the strict
contract ("implement against the EXACT operations; do not invent endpoints") —
their contract is a first-party, discoverable artifact, not something to
research.

## Security consequence

Enabling `WebFetch` on a pod that runs the agent under `bypassPermissions`
re-exposes an SSRF + secret-exfiltration surface driven by the model (which a
prompt-injection adversary may influence). Two things bound it:

1. **No new network reach.** The pod already had unrestricted egress via `Bash`
   (`curl`), so `WebFetch` adds a second egress path, not new reach.
2. **A fail-closed `PreToolUse` guard** on `WebFetch`: https-only; denies
   literal private/internal/link-local IPs (incl. cloud-metadata), internal and
   single-label hostnames, and secrets embedded in the URL; cross-host redirects
   re-enter the guard rather than being auto-followed; anything unparseable is
   denied.

**Residual (accepted, tracked):** the guard is string/URL-level and does not
resolve DNS, so a public hostname whose record points at an internal IP is not
caught. The airtight boundary is an **egress NetworkPolicy on the runner pod**
(deny RFC1918 / link-local / metadata / cluster CIDRs), which does not yet exist
in the repo. A platform/security review of the WebFetch enablement + guard is a
merge prerequisite, and the NetworkPolicy is tracked as a follow-up.
