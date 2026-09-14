# Oxygen UI — Deriving a brand theme

Read this only when `SKILL.md`'s **Brand colors** resolved at least one hex
for this build. With none resolved, the provider gets `OxygenTheme` exactly as
Setup wires it and nothing below applies.

A brand theme is a **theme of your own derived from the stock one** — not
hand-written colors sprinkled over components. Painting components brand
colors through `sx` violates "colors are tokens" and leaves every unstyled
surface off-brand. `createOxygenTheme` deep-merges overrides onto the Oxygen
base and returns a ready theme; there is no compile step and nothing
generated to commit:

```ts
// src/theme.ts — the example below is what BOTH colors resolved produce.
// Write only the keys whose color resolved: an accent alone is the two
// `primary` lines and nothing else; a neutral alone is the `background`
// line and nothing else. A key you do not write is the stock value.
import { createOxygenTheme } from '@wso2/oxygen-ui';

export const brandTheme = createOxygenTheme({
  colorSchemes: {
    light: {
      palette: {
        primary: { main: '#f5c518' },                        // accent (only if resolved)
      },
    },
    dark: {
      palette: {
        primary: { main: '#f5c518' },                        // accent (only if resolved)
        background: { default: '#0a0a0a', paper: '#161616' }, // neutral (only if resolved), as a ramp
      },
    },
  },
});
```

```tsx
// src/main.tsx — the only change from Setup
import { brandTheme } from './theme';
// <OxygenUIThemeProvider theme={brandTheme}>
```

What each color becomes:

| Color | Set it on |
|---|---|
| Accent | `palette.primary.main` in **both** color schemes. MUI derives `light`, `dark`, and the text that sits on the accent (`contrastText`) from it, so set only `main`; check the result reads in both modes and darken the light-scheme `main` if a pale hue fails contrast on white. |
| Neutral | `palette.background.default` and `palette.background.paper` in the color scheme the hex belongs to — a dark hex goes on `dark`, a light one on `light` — as a ramp: `default` the brand value, `paper` a step lighter (dark) or the brand value with `default` a step darker (light). Leave the other scheme's background stock; never put a dark neutral on the light scheme. |

Keep the hue, move the lightness: contrast is not negotiable to match a brand.
Omit a key rather than guess it — an override you do not write is the stock
value, which is the correct answer for a color nobody set. One brand color
plus one stock color is a valid outcome; a guessed hex is not.

## Pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| Brand colors are set, deployed app is stock-themed | Colors read but never put in `src/theme.ts`, or the provider still gets `OxygenTheme` | Derive `brandTheme` with `createOxygenTheme` and pass it to the provider |
| The user gave brand colors in chat, the build ignored them | A coding run never sees a conversation — colors reach it only from the skill or the project's `specs/requirements/prd.md` | Set them in The organization's colors (Settings → Skills) for the whole org, or under `## Brand colors` in the project's `specs/requirements/prd.md` for one project; an answer that is not in a file did not happen |
| Brand accent is unreadable in one mode | One `primary.main` for a pale hue used in both color schemes | Darken the light scheme's `main`; keep the hue, move the lightness |
