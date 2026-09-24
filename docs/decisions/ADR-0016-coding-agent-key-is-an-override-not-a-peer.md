# ADR-0016 — The coding agent's Anthropic key is an override on the org's key, not a peer

**Status:** Superseded by [ADR-0036](ADR-0036-the-coding-credential-is-a-subscription.md)
(2026-09-24): the coding credential is a Claude subscription token only, there
is no separate coding API key, and `codingLlm` is folded into `agents`.

An organization brings one Anthropic API key, and every agent in the platform
runs on it. Orgs asked to bill the **coding agent** separately — it is the
expensive, long-running, unattended one, and its spend is the number they want
to see on its own invoice line.

Read literally that is "two keys". But the org's one key is read by four
different consumers, and they are not equivalent:

| Consumer | How it reads the key |
|---|---|
| Design agent | `EffectiveKey` → `X-Anthropic-Key` header → `createModel` per turn |
| Coding agent | SM-API triplet → per-run ExternalSecret → runner `ANTHROPIC_API_KEY` |
| Coding agent (workflow plane) | `ApplyWPSecret` → `anthropic-credentials` Secret → ClusterWorkflow |
| RCA agent | `pushExternalSecret` → `RCA_LLM_API_KEY` ExternalSecret |

The third turned out to be **dead**: `TriggerCodingAgent` and `ApplyWPSecret`
are wired as interface methods but never called from production
(`CodingExecutor.anthropic` is assigned and never read), so the remote-worker
path is the only live coding dispatch. Worth recording because the dead-code
gate cannot see it — RTA counts interface satisfaction and a constructor-assigned
struct field as reachability, so an orphaned port still looks live.

That leaves the real question: is the new key a second credential standing
beside the first, or a narrowing of it?

## Decision

**The coding-agent key is an OVERRIDE on the organization's default key, and
"reuse" is the absence of that override.**

Concretely:

- `org_anthropic_credentials` is re-keyed to `(oc_org_id, role)`, `role ∈
  {default, coding}`. Every existing column, helper, and code path is reused
  verbatim; a pre-migration row backfills to `default`, which is what it always
  was.
- Bytes live at `org_secrets[anthropic/key]` (unchanged) and
  `[anthropic/coding-key]`; the SM-API `EntityName` differs per role
  (`anthropic` vs `anthropic-coding`) so the two vault paths cannot collide.
- The wire gets a **peer section**, `codingLlm`, three-state like the others:
  absent = keep, `null` = remove the override, value = set/rotate it.
- A coding row may exist **only** while an active default row does. Setting one
  without a default is rejected; disconnecting the default cascades the coding
  row away with it.
- Only the live coding dispatch reads it. The design agent and the RCA agent
  stay on the default key, permanently.
- The coding credential may be **either** a Console API key or a Claude Code
  OAuth token from `claude setup-token`. The default key is always an API key.

### Why the coding credential can be an OAuth token

The coding agent is a Claude Code session, and Claude Code authenticates with
either an API key or a long-lived OAuth token — the same choice the
[dev container docs](https://code.claude.com/docs/en/devcontainer) offer for
Codespaces. The token bills a Claude subscription rather than API credits, which
for most orgs is the actual point of separating the coding agent's spend.

The design agent cannot use one: it is an AI SDK model call, which speaks API
keys only. So the asymmetry is not a policy choice but a capability one, and it
is enforced in the schema (`credential_kind = 'api_key' OR role = 'coding'`)
rather than left to a reader to discover by failing.

`credential_kind` is **persisted on the row**, not re-derived. Dispatch reads
the metadata row and never the secret bytes, so at mount time it has nothing to
sniff.

### Why exactly one credential variable reaches the run

Claude Code's [authentication precedence](https://code.claude.com/docs/en/authentication#authentication-precedence)
ranks `ANTHROPIC_API_KEY` **above** `CLAUDE_CODE_OAUTH_TOKEN`. A run carrying
both would authenticate with the API key and ignore the token in silence —
billing the exact credential the org moved away from, with no error anywhere.

So the ExternalSecret materialises the credential under **one** name, chosen by
kind, and the other is never mounted.

Connect-time validation branches on kind for the same reason. Measured against
the live API with a real `claude setup-token` token:

| probe | result |
|---|---|
| `Authorization: Bearer` | **200** |
| `Authorization: Bearer` + `anthropic-beta: oauth-2025-04-20` | **200** |
| `x-api-key` | **401** `invalid x-api-key` |

So the pre-existing probe would have rejected every valid token, and bearer
alone is sufficient — the beta header Claude Code also sends is deliberately NOT
pinned, since validation would then start failing the day that flag is retired.

The runner's progress scrubber is primed with **both** variable names for the
same reason the mount picks one: priming only `ANTHROPIC_API_KEY` would leave a
token unredacted in the feed on exactly the runs that use one.

### Why "reuse" is row-absence and not a stored mode

A `codingKeyMode: reuse | separate` column would be derivable from row presence
and therefore able to **disagree** with it. `mode=separate` with no key is
representable, and it is precisely the state where an org believes it has
isolation and does not. Absence cannot lie: a configuration that claims
isolation without a key behind it does not typecheck into the schema.

### Why a peer section and not a field on `llm`

`PATCH /config` replaces a section **wholesale** — it is not a deep merge,
because a section with write-only fields cannot be deep-merged into. Had the
coding key been `llm.codingApiKey`, then `{"llm":{kind,apiKey}}` — the exact
body the console already sends to rotate the default key — would silently
**delete** the coding key. Worse, rotating only the coding key would require
resending the default key, which is write-only and which no client can read
back.

As a peer section each key rotates independently, and the destructive act has
to be spelled out: `{"codingLlm": null}`.

The cost is an asymmetry worth stating plainly: `llm: null` means *not
connected*, `codingLlm: null` means *reuse*. There is no "disconnected coding
agent" state, because an org without a coding key is not degraded — it is in
the normal case.

### Why dispatch fails closed

A coding row that exists but whose secret reference is missing or stale **aborts
the run**. It does not fall back to the default key.

Falling back is the tempting choice — the run proceeds, availability is
preserved, a WARN is logged. But the org configured that key precisely so this
workload would not touch the default one, and a silent fallback delivers the
opposite of what was asked with no signal anywhere the org can see. A failed
dispatch is loud, diagnosable, and correct; a quiet mis-bill is discovered on
an invoice, a month later.

### What the runner knows

Nothing. It reads whatever credential variable is present, exactly as Claude
Code always has; the control plane decides which one that is and mounts it via
`envFrom`. The runner image is untouched, and the same holds locally —
`AEP_CODING_ANTHROPIC_KEY` in the playground resolves to `ANTHROPIC_API_KEY` or
`CLAUDE_CODE_OAUTH_TOKEN` by prefix, and clears the other.

## Consequences

- One migration is a **one-way door**: the primary-key swap. It runs
  expand → verify → contract in guarded `DO` blocks, and drops-and-adds the PK
  in a single statement so an interrupted boot cannot leave the table without
  one.
- The `role` is now part of every accessor's identity
  (`GetByOrg(ctx, org, role)`). A dropped role filter is a compile error rather
  than a cross-role write — which is what stops a coding-key rotation from
  overwriting an org's default key.
- The reuse fallback is stated **once**, in
  `AnthropicCredentialService.ResolveCodingSecretRef`. Every other reader is
  default-only by construction, so the rule cannot leak into one by omission.
- The org-scoped advisory lock is retained (not narrowed to the role) because
  the default's disconnect moves both rows.
- Locally, `AEP_CODING_ANTHROPIC_KEY` changes WHICH credential a run uses, not
  WHETHER it uses one. Docker mode takes it by default (a container reaches no
  keychain to fall back to); host mode still requires `--api-key`, because
  defining a variable is not the same act as asking this run to authenticate
  with it, and a `bypassPermissions` process on a developer's own filesystem
  should not acquire a shared credential because a file elsewhere defined one.
- Adding a third credential kind later (a cloud-provider credential, say) means
  a new `credential_kind` value plus its env var and probe — the two switch
  points are `AnthropicCredentialKind.RunnerEnvVar` and `validateAnthropicKey`,
  and nothing else branches on kind.
- **Known gap, pre-existing and now wider**: `Disconnect` deletes the row and the
  `org_secrets` bytes but never the SM-API/vault copy —
  `SecretRefWriter.DeleteAnthropic` has no production caller. The cascade now
  orphans up to two vault entries per org instead of one. The console's "this key
  cannot be recovered" remains true for the user (they must mint a new one), so
  this is hygiene rather than correctness, but it should be wired up.
- The coding row's "an active default must exist" precondition is read through
  the **transaction** handle (`OrgAnthropicTx.GetByOrg`), so the check and the
  write that depends on it share one snapshot. Reading it off the pool would
  have been a different connection, leaving the invariant resting on the
  advisory lock alone — true today, but silently false the moment a write is
  added ahead of it. No DB-level constraint backs the rule either way; a partial
  unique index cannot express "row A requires row B".
- `{"llm": null, "codingLlm": {…}}` is rejected in the patch's **reject** phase.
  It is the one section pair the probe phase cannot judge, because each half
  probes clean alone: only the persist phase discovers that clearing the default
  cascaded away the row the override needs. Caught there it would already have
  disconnected the org's key before failing — the half-applied patch this
  endpoint exists to prevent.
- **Known gap, pre-existing and now reachable by a second path**: the SM-API
  mirror is best-effort, so `Connect` can return an `active` projection while
  the row carries no usable triplet — and `ResolveCodingSecretRef` then fails
  every coding run for that org, with the console still showing a connected key.
  This is not new to the coding role (a default-key connect whose mirror failed
  has always broken coding dispatch the same way), but the override adds a
  second row that can land in it. Surfacing it properly means a degraded status
  on the projection for **both** roles, which is its own change.
