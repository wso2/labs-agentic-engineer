# ADR-0047 — An org secret's value lives only in vault

**Status:** Accepted · 2026-10-06
**Related:** [CONTEXT.md](../../CONTEXT.md) **Org secret** ·
[ADR-0045](ADR-0045-design-work-runs-in-the-organizations-ae-studio.md) (the
pod that reads them) ·
[ADR-0036](ADR-0036-the-coding-credential-is-a-subscription.md) and
[ADR-0038](ADR-0038-an-organization-has-one-model-connection.md) (the card
that writes two of them; each carries a 2026-10-06 amendment)

## Context

The platform kept every org's secrets twice: the bytes in Postgres (sealed
with a column cipher, or in plain value columns), and a copy mirrored to the
vault after commit for the workloads that mount it. Previews, vault paths and
key prefixes sat beside them. Anyone who could read the database and the
cipher key could read back every org's GitHub token, model key and client
secrets. A vault reference was fixed per org and role and patched in place, so
a consumer could not tell an old value from a new one.

## Decision

**The vault is the only place an org secret's value is written. Postgres keeps
the name of the SecretReference that holds it, and a recorded name means
"set".**

1. **Six org secrets.** `github-pat`, `github-webhook-secret`, `default-key`,
   `coding-agent-key`, `ae-publisher-client` and `ae-studio-client`
   (CONTEXT.md **Org secret**). `org_secrets` is
   `(oc_org_id, secret, secret_ref_name, written_at)`, keyed by
   `(oc_org_id, secret)`. No column holds a value, a sealed copy, a preview or
   a vault path.

2. **Every write is a new SecretReference.** The value goes from the request
   to the vault as a new reference (on Cloud through the secret-management
   API; locally straight to OpenBao under the write-only `aep-api-writer`
   policy). Then the row moves to the new name, the consumers that point at
   the vault entry repoint, and after the commit the old reference is deleted
   by the name the row held. The AE Studio pod follows on its converge: its
   synced Secrets are kept (`deletionPolicy: Retain`), so it runs on the
   previous values until the roll. Writes of one
   secret are serialized by a per-(org, secret) lock. A vault failure saves
   nothing (502 `secret_store_write_failed`, one value-free
   `orgsecret.write_failed` line); a failure after it undoes the new
   reference and leaves the old one in place.

3. **Readers take keys from the reference, never values.** The AE Studio
   converge reads each reference's `spec.data` entries and pins them as the
   pod's parameters with a revision. The pod's Secret names stay fixed,
   because OpenChoreo prunes a Resource's objects by resource id. A container
   whose mounted revision differs from its pinned one exits, and restarts
   until `AE_SECRET_REV` equals `AE_EXPECTED_SECRET_REV`, so the pod never
   runs on a half-synced set.

4. **Every write has a user behind it.** The secrets are written by the
   setup and settings saves (GitHub connect, the AI agents card) and by the
   client ensure inside GitHub connect. Deploy-time readers only read: a
   build or coding dispatch that finds no publisher reference fails closed.
   The webhook secret is generated once at GitHub connect and not rotated.

5. **Agent Manager gets the key at save.** The card's save is the one moment
   the platform holds the model key, so that save publishes it to the Agent Manager provider of every org
   environment with an AI gateway binding. A deploy only looks the provider up and
   fails closed when it is missing.

6. **The console shows `Set` or `Not set`.** No part of a value is returned
   or displayed.

## Consequences

- The platform database holds no org secret. A database dump, a backup or the
  column cipher's key reveals none of them.
- The platform cannot read a value back. Agent Manager's copy and the client
  heal both rely on a user re-entering, or the platform regenerating, the
  value.
- A write of any org secret but `coding-agent-key` rolls the org's pod
  (ADR-0045), so it interrupts turns and Room sessions in flight.
- An AI-gateway binding created after the key save needs the key saved again
  before governed agents deploy there.
- `ColumnCipher` remains for one column, the published test-user passwords
  (`test_users.password_sealed`, ADR-0022). Moving those is a separate change.

## Alternatives considered

- **Sealed columns.** Rejected: the platform could read every value back, so
  the database stays a store of every org's secrets.
- **Patching one fixed SecretReference in place.** Rejected: a consumer cannot
  tell the old value from the new one, and a delete by label selector can take
  a reference another write still needs.
