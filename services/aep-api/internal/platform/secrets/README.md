# platform/secrets — credential storage

> **Kernel package.** Part of the [aep-api architecture](../../../README.md).

Owns the one sealed column left in Postgres and the credential validator.
aep-api stores and reads secret values through `secretmanagersvc` (OpenChoreo's
Secret API on OSS, an injected provider on Cloud), never a vault directly.

No secret value is stored in Postgres. The org secrets (the GitHub PAT, the
GitHub webhook secret, the model keys, the two org clients) live only in the
secret store, written through `organization.OrgSecretWriter` over
`secretmanagersvc`; Postgres holds their reference names only, in `org_secrets`
`(oc_org_id, secret, secret_ref_name, written_at)`, where a row means "set".
No credential table keeps a value, a sealed copy, a preview or a store key.

## Core pieces

| Piece | Role |
|---|---|
| **ColumnCipher** | AES-256-GCM seal/open keyed by `credential-encryption-key`. Its one column is `test_users.password_sealed` (identity): Thunder never returns a password, so the generated test-user passwords are kept sealed. A decrypt failure is an error. |

## Invariants

- Postgres holds secret reference names, never a secret value
  (`migrate` step `phase29_secrets_refs_only`; pinned by the migrate dbtest
  `TestSchema_NoValueColumnsRemain`). The only sealed column is
  `test_users.password_sealed`.
- Secret values never cross domain boundaries as plaintext on the wire — API
  responses and issue bodies carry refs/names only.
- No aep-api package imports the OpenBao or Vault SDK (`TestImportFences`).
- Platform-wide rules → [../../../README.md](../../../README.md).
