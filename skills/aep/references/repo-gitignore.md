# The repo-root `.gitignore`

The project has one `.gitignore` for all its components. The lead owns it.

- Add a pattern in the commit that adds the files that it covers: build
  output, dependency directories, local env files.
- Anchor each pattern to its component: `/onboarding-api/target/`, not
  `target/`. An unanchored `generated/` for one component also hides the
  `src/generated/` folder that a web app must commit.
- Do not use `git add -f` to add an ignored file. Change the pattern.

## Crash artefacts

A compiler, a JVM or a browser that crashes writes a large binary file in its
working directory. One `git add -A` then commits that file. Put this block at
the top of the `.gitignore`:

```gitignore
# crash artefacts — never wanted, in any component
core
!core/
core.[0-9]*
hs_err_pid*.log
replay_pid*.log
```

**Copy these five lines exactly.** They are not anchored, because a crash can
occur in any folder. Do not make them wider: `core.*` also hides
`src/authz/core.ts`, and without `!core/` a `src/core/` folder disappears from
`git status`.
