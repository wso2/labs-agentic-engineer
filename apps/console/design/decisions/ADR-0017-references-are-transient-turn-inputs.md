# ADR-0017: Reference documents are transient turn inputs, not versioned spec artifacts

Status: Accepted. Reverses the core of feature #383 v1 (grilling comment
2026-08-04, decisions 2, 3 and 5), which committed uploaded references to
`specs/requirements/references/` through `files/apply` and rendered them in the
Spec view.

## Context

A user starting a project often has material already written — a PRD, notes, an
API spec, a screenshot of a legacy screen. #383 lets them attach it on the
create view so the `/start` kickoff reads it instead of interviewing about what
is already written down.

v1 treated that material as a **spec artifact**: committed with the spec,
versioned with it, listed in the Spec view, previewable. That framing was what
dragged binary handling through the whole platform — `WriteOp.encoding` and
`FileContent.encoding` on the Files API, a preview dialog decoding base64 into a
`blob:` URL, a `references` group in the Spec view's mapper — and it opened a
corruption channel into the collab room that had to be closed with a predicate
at both boundaries after a live PDF was flushed back as text over its own bytes.

The material has one job: seed the derived requirements. Once
`specs/requirements/requirements.md` exists, the artifact the user reads is the
requirements, not the attachment. v1 paid a permanent versioning cost for a
transient input.

## Decisions

1. **A reference is an INPUT to a turn, not an artifact of the project.** It is
   never committed and never enters GitHub. The user's repo holds what the
   platform derived, not the raw material it derived it from.

2. **Bytes live in a per-project non-git directory on the shared `/workspaces`
   volume**, written by aep-api — its sole writer, as for mirrors and snapshots.
   Retention is the project's lifetime; the bytes count against the per-org
   quota.

3. **They reach the agent by OVERLAY, not by `.gitignore`.** This is the part
   that is easy to get wrong: a turn workspace is `git archive --format=tar
   <sha>` out of a bare mirror (`gitfs/snapshots.go`) — there is no persistent
   working tree anywhere in the platform. `.gitignore` prevents commits; it does
   not carry bytes, and an ignored file is not in the tree, so it is not in the
   archive. aep-api copies the stored references into
   `specs/requirements/references/` **inside the extracted snapshot**, so agents
   read the same path whether the design is v1 or v2.

4. **Two independent commit guards, because neither subsumes the other.**
   `specs/requirements/references/` is scaffolded into the repo's `.gitignore`
   at project create — that is what covers the coding-agent runner, which does a
   real `git clone` and stages with git, a path no server-side predicate sees.
   `isReferenceDocPath` stays in the collab committer — the committer builds
   writes from the **room**, not from a working tree, so `.gitignore` does not
   apply to it at all.

5. **A transient input gets no console surface.** No Spec view section, no
   preview, no listing after create. Chips in the create composer are the only
   place the user ever sees them.

6. **Binary transport is multipart on a dedicated endpoint**, not base64 through
   the Files API. `POST /projects/{projectName}/references` sits outside the
   specs write scope, which is correct once references are not spec files, and
   raw bytes avoid both the ~33% inflation and the decoded-vs-encoded size-cap
   trap.

## Consequences

- The Files API stays text-only. `WriteOp.encoding` and `FileContent.encoding`
  come out of the #384 handshake; a future binary-in-git need must argue for
  them on its own merits, not inherit them from this feature.
- Reference bytes are **not durable**: they survive pod restarts and reloads but
  die with the volume, and are subject to the 85% watermark eviction. A failed
  overlay is currently silent — the agent simply interviews as if no documents
  existed. Any feature that makes references load-bearing must first make that
  failure visible.
- `listReferenceDocs` sources the store, not the git tree. Projects created
  under v1 keep their committed files harmlessly and stop feeding turns; there
  is no migration.
- The next feature that wants to attach a file (agent-chat composer attachments,
  explicitly out of #383's scope) inherits this shape: store, overlay, no
  surface, no commit. **Amended by ADR-0019**: chat attachments kept "no surface"
  and "never committed", but took neither the store nor the overlay. The
  dividing question turned out to be LIFETIME — a reference outlives its request
  because `/start` can be re-run; a chat attachment does not, because the
  conversation history holds it the moment its one turn runs.

## Rejected

- **Keeping the git commit and only hiding the Spec view section.** Smallest
  change and no new storage concept — but the documents still accumulate in the
  user's repo forever, which is the cost this ADR exists to remove.
- **Browser-held references re-sent with every turn.** Zero server storage and
  nothing to clean up, but a reload, a different device, or a teammate loses
  them, and a `/start` re-run silently has none.
- **Agents-pod local disk.** The literal reading of "keep them in the pod", and
  the one that fails first: it dies on every restart and breaks the moment
  `agents` runs more than one replica.
- **Postgres blobs.** Durable and backed up, but multi-MB PDFs in the
  transactional DB is precisely what the shared workspace volume exists to
  avoid.
- **A union lookup over the store and the git tree**, to keep v1 projects
  working. Two permanent code paths and a which-wins ambiguity, for a case that
  cannot recur once this ships.
