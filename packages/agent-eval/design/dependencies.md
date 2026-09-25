# `@aep/agent-eval` — dependency notes

## The js-yaml override for promptfoo (root `package.json`)

`promptfoo@0.122.2` pins its own dependency on `js-yaml@5.3.0`, and its
transitive `@apidevtools/json-schema-ref-parser@16.0.1` needs js-yaml's v5
export surface (`binaryTag`, `setTag`, `defineMappingTag`, ...) — without it,
promptfoo fails to even import.

The repo's root `pnpm.overrides` pins `js-yaml` to `^4.3.1` workspace-wide
(commit `8137948d`, `fix(deps): bump js-yaml to ^4.3.1 for
GHSA-5p4m-2wfm-xmqj`) — a security FLOOR, not a ceiling: the advisory's
`!!omap` DoS covers `>=4.0.0 <4.3.1`, and that floor was raised specifically
to clear a HIGH Trivy finding on the overlay build. **The advisory does not
cover the 5.x line at all** — its fix landed in 5.2.1, well below the
`js-yaml@5.3.0`/`^5.2.3` versions used here. Bypassing the 4.x floor for
promptfoo's tree is not a regression against the CVE the floor exists for.

Two scoped overrides carve out just that tree:

```json
"promptfoo>js-yaml": "5.3.0",
"@apidevtools/json-schema-ref-parser>js-yaml": "^5.2.3"
```

The second entry is **not** anchored as `promptfoo>@apidevtools/json-schema-ref-parser>js-yaml`
because pnpm's override selector (pnpm 10.12.1) does not parse three-level
chains — `ERR_PNPM_INVALID_SELECTOR` on anything past `parent>child`, scoped
package names included. Verified at the time this was written: only
promptfoo's own tree resolves `@apidevtools/json-schema-ref-parser` in the
lockfile, so the wider match carries no blast radius today. If a future
workspace package also pulls in `json-schema-ref-parser` and genuinely needs
the `js-yaml@4.x` floor, this override will need to move to a real per-package
`packageExtensions` scoping (or the future package will need its own carve-out)
rather than assuming it stays promptfoo-only.

If a future Trivy scan flags `js-yaml@5.x`, check the advisory's affected
range against 5.3.0/5.2.3 before touching this — the 5.x line was clean as of
this writing.

## `yaml@^2.8.4` — reading the design contracts

The harness reads two committed YAML documents at run time: the agent's
`agent.afm.md` front matter, which names the provider contracts to stub and the
env var that addresses each, and each provider's `openapi.yaml`, which the stub
server serves from its own declared examples. Both are inputs the evaluation
cannot invent.

`yaml` (not `js-yaml`) is what the rest of this monorepo already uses for
design documents — `@aep/design-projection`, `@aep/agent-stream`, `playground`
— so a reader moving between packages meets one parser. It also reads JSON
unchanged, YAML being a superset, so a contract committed in either form needs
no special case. The `js-yaml` overrides above stay promptfoo's business and
are untouched by this.
