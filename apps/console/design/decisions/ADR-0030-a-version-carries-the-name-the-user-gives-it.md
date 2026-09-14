# ADR-0030 — A version carries the name the user gives it

## Context

The spec has always been versioned as one incrementing `v<N>` git tag over the
whole `specs/` tree. The number was not a label: the platform found the newest
version by matching `^v(\d+)$` across the tags and taking the maximum, and it
used that same scan to decide whether HEAD's `specs/` tree still matched the
newest tag — which is what makes a repeated Build reuse a version instead of
cutting a new one.

So the number carried three jobs at once: identity, order, and the name the
user reads. The user could not say what a version was for. `v7` is a
serial, and a person shipping a payments change wants to call it that.

## Decision

**The version's name is the tag, and the user writes it.** The Start build
dialog opens with a name in the box; the user may replace it. What they type
becomes the git tag, the milestone title, and the `/builds/<name>` address.
The characters allowed are the ones a git tag allows; a name already in use is
an error before submit and a conflict on the server. Nothing is slugged
silently — a name the platform rewrites is not the user's name.

**The suggested name is `v<count of versions + 1>`**, incremented until it is
free. A project nobody renames still reads `v1`, `v2`, `v3`, and the ceremony
is unchanged for a user who does not care.

**Order comes from the tag's creation time.** The tag record gains the date
(`%(creatordate:iso-strict)`, one more field in the `for-each-ref` the mirror
already runs); newest is the most recently created tag. Creation time also
separates two tags that point at the same commit, which commit ancestry
cannot.

**A name labels a snapshot; it does not make one.** The `specs/` tree still
decides what a version is. When HEAD matches the newest version's tree, the
build reuses that version and reopens its milestone: the name box is locked to
the existing name and the action reads **Rebuild `<name>`**. Cutting a second
tag over an identical tree would spend a whole planning turn to change a word.

**Renaming a cut version is not part of this.** The box names a version once,
as it is cut.

## Consequences

- The PRD's **Spec versioning** section no longer describes an incrementing
  `v<N>` sequence as the model: `v<N>` is the suggestion, and the sequence is
  the tag order by creation time.
- `^v(\d+)$` survives only as the shape of the *suggestion*. Every read that
  used it to mean "the newest version" reads the dated tag list instead.
- Tag creation no longer heals a collision by recomputing the next number —
  that only ever worked because the number advanced on its own. A supplied
  name that is taken comes back as a conflict.
- A version name reaches OpenChoreo nowhere: the tag names the spec snapshot
  and the milestone, not a release. Deploy and provisioning keep addressing
  components and resources by their own names.
- The older per-artifact `v<N>-<M>` design tags remain legacy and untouched;
  nothing writes them on this path.
