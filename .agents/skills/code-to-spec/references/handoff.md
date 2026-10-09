# Getting the specs into an AEP project

The tree `code-to-spec` writes is the state an AEP project reaches after `/start` and every
`/interview`, plus the domain model. The platform has no import path, so it enters a project
through git, which is the project's durable truth: the spec room reseeds from git on every load,
and while a spec tab is open the room's live document wins over git for the files it holds.
Creating a project also fires `/start` on the server. The order below respects both.

1. **Create the project** in the console with the product's name and a one-line idea. Let the
   kickoff interview run; answer or dismiss its question form. The content does not matter, the
   push replaces it.
2. **Wait for the flush.** The room commits about a minute after the last edit; the spec view
   shows the kickoff's `prd.md` committed. The kickoff may flush more than once.
3. **Close every spec tab** for the project, in every browser, so the room unloads.
4. **Push the generated files** to the project repository's default branch, on top of whatever is
   there at that moment: replace `specs/requirements/` and add `specs/design/domain-model.md`.
   Leave `specs/.agentic-engineer.toml` and `.claude/` as the platform wrote them. Push only the
   requirements and the domain model; the rest of the design is `/design`'s to derive with the
   organization's catalog.

   ```bash
   git clone <project repo url> recreate && cd recreate
   rm -rf specs/requirements && cp -R <out>/specs/requirements specs/requirements
   mkdir -p specs/design && cp <out>/specs/design/domain-model.md specs/design/
   git add specs && git commit -m "Requirements and domain model from the existing codebase" && git push
   ```
5. **Reopen the spec view.** The room reseeds from git and shows the generated requirements. A
   tab left open at step 3 would have reverted the push on its next flush. Expect one more commit
   from the room a couple of minutes later: it writes the files back in its own markdown escaping,
   so `[tag]` becomes `\[tag\]` and the diff is one line out for one line in. Nothing is lost; the
   platform's reader unescapes it. A diff that deletes feature files is the revert to watch for.
6. **Settle the assumed lines and answer the open questions** in the console, or leave them; they
   hold nothing up. A feature with a `*blocking*` question stays undesigned until it is answered.
7. **Run `/design`.** It converges the domain model, derives the cell and components, and mints
   the acceptance criteria. External systems the requirements name as givens come up as "needs
   your input" unless the organization has registered them as external resources. From here the
   project is an ordinary AEP project.

With a checkout of the platform repository, the same `/design` can be tried locally first by
pointing the playground at the output directory and running its design phase.
