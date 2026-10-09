# platform/secrets — credential storage and delivery

> **Kernel package.** Part of the [aep-api architecture](../../README.md).

Owns the BFF-side secret fence: per-org credential persistence, column-level
encryption for org config fields, and OpenBao/Vault KV delivery into the
dataplane. Domains import only the ports here — never the vault SDK.

## Core pieces

| Piece | Role |
|---|---|
| **CredentialStore** | Per-org read/write store for credential material (GitHub PAT, Anthropic key, …). Wired implementation: Postgres AES-256-GCM (`dbStore` / `NewDBStore`). |
| **ColumnCipher** | AES-256-GCM seal/open for credential columns outside `org_secrets` (e.g. `publisher_client_secret`, `webhook_secrets`). Shares `credential-encryption-key` with `CredentialStore`. `OpenTolerant` accepts legacy plaintext only during the encrypt-in-place migration; all new writes seal. |
| **DeliveryKV** | Vault/OpenBao KV-v2 helper for pushing user-app delivery secrets. Confined here by the OpenBao import fence; callers use `secretmanagersvc.Provider` instead. |

## Ports (selected)

| Port | Consumers | Contract |
|---|---|---|
| `CredentialStore` | `organization` | `Get` / `Put` / `Delete` scoped by `ocOrgID` |
| `Resolver` · `AppTokenMinter` | `organization`, `dependencies` | resolve credential refs; mint app tokens |
| `secretmanagersvc.Provider` | `edge` composition | KV writes + `SecretReference` authoring for ESO |

## Invariants

- Secret values never cross domain boundaries as plaintext on the wire — API
  responses and issue bodies carry refs/names only.
- Vault SDK imports stay inside this package (`DeliveryKV`, provider wiring).
- **`CREDENTIAL_ENCRYPTION_KEY` has NO default and boot fails without it.** A
  default is a key every installation shares and anyone can read in this tree.
  `aectl platform install` generates one per installation into OpenBao at
  `aep/credential-encryption-key` and never replaces it — replacing it makes
  every credential sealed under it unreadable. If OpenBao has lost the key
  (it runs in-memory), a re-run of install restores it from the ESO-synced
  `aep-credential-encryption-key` Secret rather than generating a new one.
- Platform-wide rules → [../../README.md](../../README.md).
