# ADR-0036 — The coding credential is a Claude subscription, and the AI agents card saves as one unit

**Status:** Accepted · 2026-09-24
**Supersedes:** [ADR-0016](ADR-0016-coding-agent-key-is-an-override-not-a-peer.md)
(the coding-agent key is an override on the org's key). Its reasons for one
credential variable per run, for persisting `credential_kind`, for the kind-aware
probe and for failing closed still hold and are restated here where they apply.
**Amends:** [ADR-0028](ADR-0028-the-coding-agent-runtime-and-model-are-an-organization-setting.md)
(see its 2026-09-24 amendment).

## Context

ADR-0016 let the coding agent bill a credential of its own: either a second
Console API key or a Claude Code OAuth token (`claude setup-token`). In practice
the second API key bought nothing an org asked for (it is the same kind of
credential, billed the same way) and it made every rule around the coding agent
two-branched. The token is the one that matters: it bills a Claude plan instead
of API credits.

At the same time the settings console folded the org key, the model and the
coding agent into one "AI agents" card with one Save, which the API served as
three sections (`llm`, `codingLlm`, `codingAgent`) written by separate
transactions. A database error between them could leave a save half-applied.

## Decision

1. **The coding role holds a Claude subscription token and nothing else.** The
   `org_anthropic_credentials` CHECK is `(role = 'default' AND credential_kind =
   'api_key') OR (role = 'coding' AND credential_kind = 'oauth_token')`. A
   separate coding API key is not offered; the migration that installs the CHECK
   (`phase16_coding_role_subscription_only`) deletes every existing one with its
   `org_secrets` bytes, and those orgs' coding runs bill their default key.

2. **One `agents` section replaces `codingLlm` and `codingAgent`.** `llm` stays
   the org's API key. `agents` carries the one model every agent uses, the coding
   runtime, and `subscription`:
   - write `{model?, runtime?, subscription?: {kind: "claude", token} | null}` —
     every field optional, `subscription` three-state (absent keeps, value sets,
     `null` deletes), `agents: null` resets the section to the platform defaults;
   - read `{model, runtime, subscription: {kind, keyPrefix, keyLast4, status,
     connectedAt, …} | null, updatedAt, updatedBy}` — never null.
   The token is sent only when it changes, so a model change never needs it back.

3. **One rule: a subscription needs the `claude-code` runtime and a connected API
   key**, judged on the state the patch leaves (`judgeCard`,
   `organization/agents_rule.go`). What follows from it:
   - choosing `opencode`, disconnecting the key (`llm: null`) and resetting the
     card (`agents: null`) each delete the stored token in the same transaction;
   - a patch that sets a token the end state cannot use is refused on
     `body.agents`: `agents_subscription_requires_claude_code` (OpenCode in the
     same patch, or already) and `agents_subscription_requires_api_key` (no key,
     or the key cleared in the same patch);
   - an API key offered as the subscription is refused
     (`agents_subscription_token_required`), as is a token offered as the org key
     on `body.llm` (`anthropic_oauth_token_coding_only`).
   A 400 from `/config` carries the refusal's slug as the error `code` when it
   has one, `validation_failed` otherwise.

4. **One save of the card is one unit of work.** `llm` and `agents` are written in
   one transaction under the per-org advisory lock `org_anthropic:<org>`,
   covering the credential rows, the `org_agent_settings` row and the
   `org_secrets` bytes: the secret store joins the transaction
   (`secrets.TxCredentialStore.WithDB`). The patch is judged once before the live
   key probes (so a refusal costs no probe) and again inside the transaction
   against the rows it writes over. The SM-API copy of a written credential is
   mirrored after commit, best-effort. A save clears the row's secret-ref
   triplet, because the vault path is fixed per org and role: a failed mirror
   then fails dispatch closed instead of mounting the previous credential. A
   deleted credential's copy is deleted after commit by the secret-ref name read
   before the row went. A blank key or token is refused on its section.

5. **Dispatch mounts the subscription only on Claude Code.**
   `ResolveCodingSecretRef(ctx, org, runtime)` returns the subscription's triplet
   when the runtime is `claude-code` and one exists, the default key's otherwise.
   It fails closed on a subscription with no usable triplet, and on any other
   runtime never consults the subscription at all. Exactly one of `ANTHROPIC_API_KEY` /
   `CLAUDE_CODE_OAUTH_TOKEN` reaches a run, because Claude Code ranks the former
   above the latter and would silently ignore the token.

6. **The org records when its key was disconnected**
   (`organizations.llm_disconnected_at`, projected as
   `ConfigProjection.llmDisconnectedAt` while `llm` is null). The credential row
   is deleted on disconnect, so this is the one trace that the org had a key; the
   onboarding wizard reads it to say "your key was disconnected".

## Consequences

- Coding spend of orgs that had a separate coding API key moves onto their
  default key when the migration runs. Release notes must say so.
- **Orphaned SM-API copies.** The migration cannot delete the vault copies of the
  keys it removes (entity `anthropic-coding`): deleting one needs a signed-in
  user's context. They stay until the org next saves a subscription, which
  overwrites that path. Nothing reads them — dispatch resolves through the row,
  which is gone. A post-commit delete that fails leaves the same kind of orphan.
- `org_coding_agent_settings` becomes `org_agent_settings`
  (`phase17_org_agent_settings` copies the rows into the table AutoMigrate
  creates, then drops the old one): the row holds the model every agent uses.
- The advisory lock stays org-scoped: a disconnect moves both credential rows,
  and the card moves the setting row with them.
- `AnthropicCredentialService` no longer has `Connect`/`Disconnect`; the card's
  unit of work (`AgentSettingsService`) is the only writer of credential rows.
