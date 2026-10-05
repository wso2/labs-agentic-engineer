# platform/secrets — credential storage and delivery

> **Kernel package.** Part of the [aep-api architecture](../../README.md).

Owns the BFF-side secret fence: column-level encryption for the few sealed
columns left outside the vault, and OpenBao/Vault KV delivery into the
dataplane. Domains import only the ports here — never the vault SDK. No org
secret value is stored in Postgres: the org secrets (the GitHub PAT, the model
keys, the org clients) live only in the vault, written through
`organization.OrgSecretWriter` over `secretmanagersvc`.

## Core pieces

| Piece | Role |
|---|---|
| **ColumnCipher** | AES-256-GCM seal/open for credential columns (e.g. `publisher_client_secret`, `webhook_secrets`, `test_users.password_sealed`), keyed by `credential-encryption-key`. `OpenTolerant` accepts legacy plaintext only during the encrypt-in-place migration; all new writes seal. |
| **DeliveryKV** | Vault/OpenBao KV-v2 helper for pushing user-app delivery secrets. Confined here by the OpenBao import fence; callers use `secretmanagersvc.Provider` instead. |

## Ports (selected)

| Port | Consumers | Contract |
|---|---|---|
| `secretmanagersvc.Provider` | `edge` composition | KV writes + `SecretReference` authoring for ESO |

## Invariants

- Secret values never cross domain boundaries as plaintext on the wire — API
  responses and issue bodies carry refs/names only.
- Vault SDK imports stay inside this package (`DeliveryKV`, provider wiring).
- Platform-wide rules → [../../README.md](../../README.md).
