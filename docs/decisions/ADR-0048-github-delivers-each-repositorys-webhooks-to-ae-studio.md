# ADR-0048 — GitHub delivers each repository's webhooks to the org's AE Studio

**Status:** Accepted · 2026-10-06
**Related:** [ADR-0045](ADR-0045-design-work-runs-in-the-organizations-ae-studio.md)
(the org's AE Studio) ·
[ADR-0046](ADR-0046-ae-studio-checks-platform-idp-tokens-itself.md) (the
credential the forward carries) ·
[ADR-0047](ADR-0047-an-org-secrets-value-lives-only-in-vault.md) (the webhook
secret)
**Detail:** [`components/dataplane/ae-system-project/ae-studio/ae-studio-tools/design/route-groups.md`](../../components/dataplane/ae-system-project/ae-studio/ae-studio-tools/design/route-groups.md)

## Context

GitHub delivered every repository's events to one install-wide route on
`aep-api`, which verified the signature with a secret it read from Postgres.
Once the org's GitHub token and webhook secret live only in the org's pod
(ADR-0045, ADR-0047), `aep-api` holds neither, and a control-plane route that
anyone on the internet can post to is one more surface on the control plane.
`aep-api` still owns what a delivery means: the delivery ledger, its replay
and the run state the events move.

## Decision

**Each repository's hook delivers to its org's `ae-studio-tools`, which
verifies it and forwards it synchronously to `aep-api`; `aep-api` keeps the
ledger, the replay and the sweeps.**

1. **One hook per repository, asked for by `aep-api`.** `aep-api` registers
   the hook through `ae-studio-tools`
   (`POST /internal/v1/repos/{owner}/{repo}/hooks`) when a project's
   repository is created or linked, stores its id on the repository row, and
   deletes it when the project goes. The pod owns the delivery URL and signs
   with the org's webhook secret. A create whose registration failed keeps
   going with a warning; the 60 s reconcile sweep registers the missing hook.

2. **`/webhooks/github` verifies and forwards.** The route caps the body at
   25 MiB, reads it within 10 s, admits 8 deliveries at a time, and checks
   `X-Hub-Signature-256` with the org's one current secret (the secret is not
   rotated). A verified delivery goes byte for byte to `aep-api`
   `POST /internal/v1/ae-studio/webhook-events` under the org's
   `ae-studio-<org>` client token; `aep-api` binds the org recorded for that
   client. There is no buffer in the pod.

3. **GitHub sees 200 or 503 for a verified delivery.** 200 when `aep-api`
   took it or refused it for good (2xx, or a 4xx other than 401, 403 and 429).
   503 when `aep-api` answered 5xx after two retries inside an 8 s budget,
   could not be reached, refused the pod's own credential (401, 403) or
   throttled it (429). A delivery the pod itself refuses answers its own
   status (busy 503, 413, 408, 400, a bad signature 401) and is not forwarded.

4. **`aep-api` persists, then runs.** It stores the delivery (deduped on
   GitHub's delivery id), takes a lease, answers 202 (200 for a duplicate) and
   runs the handlers detached. The `Replayer` re-runs an
   unprocessed delivery until it has had 5 runs in all (the first included),
   within 15 minutes of receipt
   (backoff from 30 s, doubling); past that the delivery is abandoned and the
   reconcile sweeps recover from ground truth.

5. **A failed delivery stays failed on GitHub.** GitHub does not redeliver on
   its own; a 503 shows on the hook as failed and is redelivered only by hand.
   A delivery lost to a pod roll is therefore recovered by `aep-api`'s sweeps,
   not by GitHub.

6. **Locally, one smee channel per org.** A laptop install has no public
   ingress, so the org's hooks deliver to a smee.io channel, derived and never
   stored: the first 22 characters of base64url(HMAC-SHA256(install seed,
   the org's OpenChoreo namespace)). The optional `webhook-relay` container
   (`gosmee`, pinned by digest) in the org's pod relays it to
   `/webhooks/github` over the pod's loopback, and the HMAC check is the same
   as for a direct delivery.

   Note (2026-10): a hosted dev install whose edge GitHub cannot reach turns
   the same relay on with `AE_STUDIO_WEBHOOK_RELAY_ENABLED=true`; its key is
   derived from the install's existing `CREDENTIAL_ENCRYPTION_KEY` (HKDF), so
   no relay secret is configured. Production stays off: smee.io is
   third-party.

## Consequences

- No webhook route remains on the control plane; GitHub reaches only the
  org's own pod.
- A pod roll drops the deliveries that arrive while it is down. They show as
  failed on the hook; the sweeps converge the state they described.
- The delivery semantics `aep-api` had (ledger, lease, replay window) are
  unchanged. Only the route and the verifier moved.
- Rotating the webhook secret needs a second reference and a sweep over every
  repository's hook; it is not built.

## Alternatives considered

- **Keep one install-wide route on `aep-api`.** Rejected: it keeps an
  internet-facing route on the control plane and needs the webhook secret
  there.
- **One smee channel per install.** Rejected: the relay would then need the
  repository → org routing that only `aep-api` has.
