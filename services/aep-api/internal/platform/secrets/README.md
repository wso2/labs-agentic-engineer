# platform/secrets — credential storage and delivery

> **Kernel package.** Part of the [aep-api architecture](../../README.md).

Owns the BFF-side secret fence: the one sealed column left in Postgres and
OpenBao/Vault KV delivery into the dataplane. Domains import only the ports
here — never the vault SDK.

No secret value is stored in Postgres. The org secrets (the GitHub PAT, the
GitHub webhook secret, the model keys, the two org clients) live only in the
vault, written through `organization.OrgSecretWriter` over `secretmanagersvc`;
Postgres holds their reference names only, in `org_secrets`
`(oc_org_id, secret, secret_ref_name, written_at)`, where a row means "set".
No credential table keeps a value, a sealed copy, a preview or a vault path.

## Core pieces

| Piece | Role |
|---|---|
| **ColumnCipher** | AES-256-GCM seal/open keyed by `credential-encryption-key`. Its one column is `test_users.password_sealed` (identity): Thunder never returns a password, so the generated test-user passwords are kept sealed. A decrypt failure is an error. |
| **DeliveryKV** | Vault/OpenBao KV-v2 helper for pushing user-app delivery secrets. Confined here by the OpenBao import fence; callers use `secretmanagersvc.Provider` instead. |

## Ports (selected)

| Port | Consumers | Contract |
|---|---|---|
| `secretmanagersvc.Provider` | `edge` composition | KV writes + `SecretReference` authoring for ESO |

## Invariants

- Postgres holds secret reference names, never a secret value
  (`migrate` step `phase26_secrets_refs_only`; pinned by the migrate dbtest
  `TestSchema_NoValueColumnsRemain`). The only sealed column is
  `test_users.password_sealed`.
- Secret values never cross domain boundaries as plaintext on the wire — API
  responses and issue bodies carry refs/names only.
- Vault SDK imports stay inside this package (`DeliveryKV`, provider wiring).
- Platform-wide rules → [../../README.md](../../README.md).
