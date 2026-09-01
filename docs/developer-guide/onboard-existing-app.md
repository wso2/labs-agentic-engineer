# Onboard an existing application (requirements import)

Existing apps enter AEP as a **requirements bundle**, not as vendored source.
See [ADR-0020](../decisions/ADR-0020-onboarding-imports-requirements-only.md).

## 1. Install `modernize-extract` on your machine

The skill ships in the org's `org-skills` repo (reconciled from the platform
library). Copy it where Claude Code / Cursor will see it:

```bash
# from a clone of your org's org-skills repo
cp -r skills/modernize-extract ~/.claude/skills/
# or into the legacy app itself
cp -r skills/modernize-extract /path/to/legacy-app/.claude/skills/
```

You can also copy the body from console **Settings → Skills → modernize-extract**.

## 2. Run it in the legacy repository

```bash
cd /path/to/legacy-app
# In Claude Code / Cursor:
/modernize-extract
```

The skill surveys the tree, tries to run the app, grills you on load-bearing
behaviour, and writes `.aep/requirements/` (`prd.md`, `domain-model.md`,
`business-rules.md`, `integrations.md`). It refuses to run if `specs/` already
looks like an AEP project.

Pack:

```bash
cd .aep && tar czf requirements-bundle.tar.gz -C requirements .
```

## 3. Import into a new AEP project

1. In the console, choose **Import an existing app** on the create page (or
   create with `requirementsImportPending: true` on the API). This skips the
   `/start` interview — the bundle is the brief.
2. Name the project and continue — you land on **Spec** with the import dialog open.
3. Upload `requirements-bundle.tar.gz` (upload icon also lives in the
   Requirements rail header when the project has no requirements yet).

The API gates the PRD shape (numbered user stories), flat paths, and size, then
commits and cuts `v1`.

## 4. Generate design and build

Use **Generate design** as for any greenfield project. `/design` reads the full
requirements corpus. Then **Build** as usual — milestones, coding, deploy are
unchanged.
