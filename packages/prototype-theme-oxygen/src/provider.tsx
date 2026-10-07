/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

/**
 * The theme's root: AEP's own Oxygen theme (`@aep/ui-theme`, the console's),
 * through Oxygen's `OxygenUIThemeProvider` as the console applies it, so a
 * prototype looks like the console it is reviewed in. Sandbox-safe: one fixed
 * theme (no theme switching, so nothing is fetched or stored). The scheme is
 * the host's (`colorScheme`, the console's resolved light or dark), set
 * through MUI's `useColorScheme`; without one it follows the system, as the
 * console's default does. MUI's scheme storage is guarded and the frame has
 * none, so setting it stores nothing. Emotion injects every
 * style inline and Oxygen ships its Inter font as data URIs, so the frame
 * loads nothing.
 */

import { aepTheme } from "@aep/ui-theme";
import { Box, OxygenUIThemeProvider, useColorScheme } from "@wso2/oxygen-ui";
import { useEffect, type ReactNode } from "react";

/** Applies the host's scheme (or the system's) inside the provider. */
function Scheme({ colorScheme }: { colorScheme: "light" | "dark" | undefined }) {
  const { setMode } = useColorScheme();
  useEffect(() => {
    setMode(colorScheme ?? "system");
  }, [colorScheme, setMode]);
  return null;
}

export function OxygenProvider({ children, colorScheme }: { children: ReactNode; colorScheme?: "light" | "dark" | undefined }) {
  return (
    <OxygenUIThemeProvider theme={aepTheme}>
      <Scheme colorScheme={colorScheme} />
      <Box
        sx={{
          // The kit's Annotate outline and label, in the theme's primary.
          "--proto-select": aepTheme.vars.palette.primary.main,
          display: "flex",
          flexDirection: "column",
          flex: 1,
          minHeight: 0,
          height: "100%",
          bgcolor: "background.default",
          color: "text.primary",
        }}
      >
        {children}
      </Box>
    </OxygenUIThemeProvider>
  );
}
