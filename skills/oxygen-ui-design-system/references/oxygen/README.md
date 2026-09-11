# Oxygen UI's own reference docs

Verbatim copies of `node_modules/@wso2/oxygen-ui/.claude/*.md` from
**0.13.1** — the component index (`components.md`), whole-screen compositions
(`patterns.md`), theme customisation (`theming.md`) and the MUI/lucide
migration guide (`migration.md`).

They are here so the index of every composite Oxygen ships is in the project
mirror before `npm install` has run. **Once the package is installed, read the
package's copy instead** (`node_modules/@wso2/oxygen-ui/.claude/`): it is
matched to the installed version, and this one is not. Either copy is prose:
a prop is settled by `scripts/props.mjs`, which reads the installed types, and
`SKILL.md`'s "Known errors" lists where the prose is wrong.

Refresh together with `sample/`, from the same release.
