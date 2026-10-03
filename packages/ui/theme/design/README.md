# `@aep/ui-theme` — design notes

AEP's one Oxygen UI theme: `aepTheme`, the guided-shell design's colours
(light and dark) over Oxygen's base, sentence-case buttons, and the shell's
CSS variables (`--aep-shell-*`). Two consumers apply it through
`OxygenUIThemeProvider`, so they cannot drift:

- the console (`apps/console/src/main.tsx`);
- `@wso2/prototype-theme-oxygen`, which bundles it into the sandboxed frame
  and render-check runtimes. So the theme stays plain data: no storage, no
  network, no assets beyond Oxygen's inlined fonts.

`tones.ts` spells out every palette field (variants and CSS-variable
channels): Oxygen's base keeps its own orange in any field an override leaves
out. `test/aepTheme.test.ts` pins that.

Built to `dist/` (`tsc`); a Docker image that builds either consumer builds
this package first.
