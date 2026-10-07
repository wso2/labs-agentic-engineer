# `@wso2/prototype-theme-default` — design notes

Plain React and CSS custom properties (`--pt-*` tokens on `.pt-root`), no
component library, so the runtimes stay small. Each component file exports its
CSS; `provider.tsx` injects all of it inline, because the frame loads nothing.
Overlays draw in place (fixed position), not in a portal. Buttons are always
`type="button"`: submitting is the kit's. The host's `colorScheme` becomes
`data-theme` on `.pt-root` (dark tokens); without one, `prefers-color-scheme`
decides. The app shell is a plain header
(product, user button), a side list and the content; its user menu stays in
the markup while closed (`hidden`), as the kit's contract asks, so the render
check sees its targets. Stats draw no icon; a row's actions are all inline
buttons (no overflow menu).

`scripts/build-runtimes.ts` calls the kit's `buildThemeRuntimes` after `tsc`;
the package ships `dist/frame-runtime.js` and `dist/check-runtime.js`. A new
theme copies this shape: a default-exported `PrototypeTheme` plus the same two
runtime exports, selected with `prototype --theme <package>`.
