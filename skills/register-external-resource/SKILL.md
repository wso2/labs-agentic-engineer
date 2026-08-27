---
name: register-external-resource
description: Use when registering a Registered External resource from Marketplace chat (`/register-external-resource`).
metadata:
  aep:
    kind: platform
    audience: [design]
---

# Register External resource

This creates a **Registered External resource** — an org-level integration
projects can consume, with config keys whose values are filled per environment
on the form, never in chat.

The instruction's trailing text is the user's idea, in their words. It is a
brief, not a schema.

## When unsure, ask — never invent a schema

If the idea does not pin a name, what the resource is, how consumers should
call it, or which config keys exist, stop and ask with `ask_question` /
`ask_questions`. Do **not** invent a schema to look finished.

`grilling` owns the question mechanics. Ask only questions whose answers change
the draft.

## When sure (or after answers), draft

Call `draftExternalResource` with:

- **name** — the resource identity
- **description** — what the resource is
- **consumption instructions** — how a consuming project should use it;
  distinct from description, never a restatement of it
- **config keys** — each with `key`, `description`, and `secret`
- **resource-docs** — optional, URL-only

## Secrets stay off this channel

**Never** put env values or secret bytes in the draft, in questions, or in
narration. Environment values are form-only.

## Edit

On edit, **never** change `name` or config key identities. You may refine
description, consumption instructions, existing key descriptions, and URL
resource-docs. Do not add or rename keys.
