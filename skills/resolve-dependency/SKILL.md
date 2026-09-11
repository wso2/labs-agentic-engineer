---
name: resolve-dependency
description: Use for taking one external dependency from open to resolved — `/resolve-dependency <name> [answer]` names it and may carry the provider the user chose; `/resolve-dependencies` walks every open one in turn. Ask which provider (their suggestions as options; never choose for them), get its interface on disk (found, uploaded from the card, or assumed under the authorization the card records), then derive the config keys.
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Resolve a dependency

One external dependency, taken from whatever state it is in to **resolved**:
a chosen provider, a committed contract in its own directory, and the config
keys every consumer codes against. The instruction names the dependency, and
may carry the user's answer after the name — `/resolve-dependency
currency-converter Open Exchange Rates`, or a document URL — which is the
provider they chose on the definition. `/resolve-dependencies` (plural, no name)
walks every dependency that is still open, one at a time, in the order the
Build drawer lists them, and ends with the list empty or with what is left
named plainly.

`architecture` owns the definition's shape and the research playbook; this
skill is the guided flow over it. `grilling` owns the question mechanics.
Everything you write goes to `specs/design/dependencies/<name>/` — never to a
component's `design.json`, which only references the dependency by name.

## Read the state first

Read `specs/design/dependencies/<name>/dependency.json` from the snapshot and
say, in one line, where it stands:

| On disk | State | This flow's job |
|---|---|---|
| no `provider` (`suggestions` may be open) | needs-input | ask which provider, settle it |
| `provider` + `style`, no `contract` (or no `sdk` for style `sdk`) | needs-contract | get the contract |
| contract on disk, `assumed` absent, contract marked assumed | needs-acceptance | ask the user to accept |
| contract on disk | resolved | nothing — say so and stop |
| `source: "org"` | registered | nothing — the org record owns it |

Do the steps below in order, skipping the ones the state has already passed.
Each step is at most one `ask_question`; a `/resolve-dependencies` walk asks
them per dependency, never as one batch across dependencies — the user
answers one system at a time.

## 1. Settle the provider — the user's choice, never yours

All research for an open dependency happens here, and the user chooses.

- **No provider yet: ask the "Which provider?" card first.** ONE
  `ask_question`, before any research: the `suggestions` on file as the
  options (label = the suggestion's name; description = its one-line
  distinction for THIS product; mark at most one recommended only when a
  real signal favours it), then ALWAYS these two, in this order —
  `{ "label": "Another provider", "freeText": true, "description": "Name a different provider, or paste a link to its API document." }`
  and `{ "label": "Find one for me", "description": "I research the options and come back with what fits." }`.
  Never write options back into the file — the question lives in this
  conversation.
  - A named provider settles `provider`: find out how it is consumed
    (`style`) and go on to step 2.
  - A document URL settles both: fetch it through `slice_openapi_spec` (the
    URL plus the operations the design calls), take `provider` from the
    document's `info.title`, and treat step 2's route 1 as taken.
  - **Find one for me**: research the capability (`web_search`) and come
    back with ONE more card — the providers that genuinely fit, each with its
    distinction, your recommendation marked, and **Another provider** as
    free text. One fit is still a question ("Use Stripe?").
- **The instruction already carries an answer** (a name or a URL after the
  dependency's name): treat it as the card's answer and skip the card.
- **A Registered External resource fits.** Say so and write only
  `{ "name", "source": "org" }` — the platform fills the rest at save, and no
  contract step follows.

Write the choice: `provider` and `style` set, `suggestions` removed. The
config keys come last (step 3), from the provider chosen — never before.

## 2. Get the contract

Four routes — a ladder, climbed in this order, and the user is told which
rung you took:

1. **Find it.** `web_search` for the provider's published OpenAPI or GraphQL
   document. Name the operations the design actually calls (the flows and
   the component's description say which) and call `slice_openapi_spec` with
   the URL and those operations. It fetches the whole document outside your
   context, cuts the slice, validates it, and returns the slice with its
   provenance. `addFile` the slice as `openapi.yaml` in the dependency's
   directory and record `contract` plus the `provenance` block the tool
   returned. If you cannot name the operations, you do not understand the
   dependency well enough to slice it — go back to the flows before asking
   the user for anything.
2. **Derive it from the provider's documentation.** When no public document
   exists but the provider's OWN developer reference does — pages that name
   the operations the design calls, with their parameters and responses —
   write the interface from those pages: the operations the design needs and
   nothing more, as `openapi.yaml` in the dependency's directory, with
   `x-aep-derived: true` at the document root and, on EVERY operation, an
   `x-aep-source: <page url>` naming the page it came from. Record `contract`
   and a `provenance` block with `sourceUrl` = the reference's root page. No
   permission is needed: the dependency reads resolved, flagged *derived*.
   The bar is the whole design, not part of it — if one operation the design
   needs has no page, or the only pages are marketing, a blog, a third-party
   tutorial or a partial reference, this rung does not apply: go to 3.
3. **Ask for it.** When neither a document nor documentation exists (most
   couriers, most private APIs), ask ONE question — "How should I get its
   interface?" — whose `options` are EXACTLY these three, `action` included
   (the console runs the action when the user answers; an option without it
   is a dead button):

   ```json
   [
     { "label": "Give a link", "freeText": true,
       "description": "Paste a URL to the provider's published OpenAPI document." },
     { "label": "Upload one",
       "action": { "kind": "upload-interface", "dependency": "<name>" },
       "description": "Upload the document here; I read it once it lands." },
     { "label": "Proceed on your assumption", "recommended": true,
       "action": { "kind": "accept-assumption", "dependency": "<name>" },
       "description": "I write the interface from the provider's documentation, covering only what this design calls. Choosing this authorizes it — nothing further is asked; validation cannot check it against a published document." }
   ]
   ```

   - **Give a link** → route 1 with that URL.
   - **Upload one** → the answer arrives once the document is on disk; read
     it from the snapshot and go on to step 3.
   - **Proceed on your assumption** → the user's authorization is already
     recorded on the definition when the answer reaches you; go on to route
     4. Never describe this option as needing a later acceptance.
4. **Assume it — authorized by that answer, and only then.** Write the
   contract yourself from the provider's documentation pages and what you
   know — the operations the design needs and nothing more — as
   `openapi.yaml` in the dependency's directory, with `x-aep-assumed: true`
   at the document root and, in the `info.description`, a short note of what
   you are unsure about (auth scheme, pagination, error shapes). Record
   `contract` and a `provenance` block with `sourceUrl` naming the
   documentation you read. Re-read `dependency.json` from the snapshot
   first: the user's `assumed` record is already on it — carry it over
   exactly; you never write or alter it. Nothing further is asked of the
   user: the dependency reads resolved (flagged assumed) the moment the
   file lands.

An `sdk` style needs its manifest too: write `sdk.json` with a `packages`
entry for every implementation language the design's components use (the
ecosystem-prefixed identifier the provider publishes), the docs URL, and the
calls the design relies on; set `"sdk": "sdk.json"`. Put the API slice beside
it when the provider has one — without it the dependency is SDK-only, and
you say so.

## 3. Settle the config keys

Derive `config` from the contract — a REST API's `securitySchemes`, an SDK's
constructor arguments — following the `architecture` skill's conventions
(SCREAMING_SNAKE_CASE, `secret` only for credentials, a `description` saying
where the user finds the value). Keys already on the file stay unless the
contract contradicts them.

## Close

One line per dependency you touched: its name, the provider chosen, the state
it is in now, and the one thing (if any) still needed from the user. Nothing
else: the files carry the detail, and the Build drawer re-reads them.
