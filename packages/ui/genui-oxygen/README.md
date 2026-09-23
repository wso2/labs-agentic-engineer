# @aep/ui-genui-oxygen

The `@aep/ui-genui` catalog rendered with WSO2 Oxygen UI.

```tsx
import { GenUiView } from "@aep/ui-genui-oxygen";

// Inside the host's OxygenUIThemeProvider.
<GenUiView spec={spec} handlers={handlers} />;
```

Every component is a stock Oxygen component with no styling override (no
`sx` colours, radii or sizes), so generated UIs take on whichever Oxygen theme
the host uses: 20px pill buttons under Acrylic Orange (the console), 4px under
Classic (the Oxygen Storybook). Tables use `ListingTable`, metrics `StatCard`,
code `CodeBlock`, page titles `PageTitle`, sections `Accordion`. `sx` appears
only for layout (row alignment, spacing) and in `AgentTimeline`, which Oxygen
has no component for and is composed from primitives and theme tokens.

Tests: `src/conformance.test.tsx` runs the shared suite from
`@aep/ui-genui/testing`. See the core README for how the design-system
packages fit together.
