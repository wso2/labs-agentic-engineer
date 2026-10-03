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

/** Layout: the bare screen, the page every screen's content sits on, stack, grid, split and detail record. */

import { Box, PageContent, Typography } from "@wso2/oxygen-ui";
import type { ThemeDetailProps, ThemeGridProps, ThemeScreenProps, ThemeSplitProps, ThemeStackProps } from "@wso2/prototype-kit";
import type { ReactNode } from "react";
import { TitledCard } from "./card.js";

/** A screen's content on Oxygen's `PageContent`: centred, padded, its parts a column apart. */
export function Page({ children }: { children?: ReactNode }) {
  return <PageContent sx={{ display: "flex", flexDirection: "column", gap: 2.5 }}>{children}</PageContent>;
}

export function Screen({ nav, children }: ThemeScreenProps) {
  return (
    <Box sx={{ display: "grid", gridTemplateColumns: "auto minmax(0, 1fr)", gridTemplateRows: "auto minmax(0, 1fr)", flex: 1, minHeight: 0, height: "100%" }}>
      {nav}
      <Box component="main" sx={{ gridColumn: 2, gridRow: 2, minWidth: 0, minHeight: 0, display: "flex" }}>
        <Page>{children}</Page>
      </Box>
    </Box>
  );
}

export function Stack({ direction, children }: ThemeStackProps) {
  return (
    <Box sx={direction === "row" ? { display: "flex", flexDirection: "row", flexWrap: "wrap", alignItems: "center", gap: 1.5 } : { display: "flex", flexDirection: "column", gap: 2 }}>
      {children}
    </Box>
  );
}

export function Grid({ columns, children }: ThemeGridProps) {
  return <Box sx={{ display: "grid", gap: 2, gridTemplateColumns: { xs: "minmax(0, 1fr)", md: `repeat(${columns}, minmax(0, 1fr))` } }}>{children}</Box>;
}

export function Split({ left, right, ratio }: ThemeSplitProps) {
  const column = { display: "flex", flexDirection: "column", gap: 2.5, minWidth: 0 } as const;
  return (
    <Box sx={{ display: "grid", gap: 2.5, gridTemplateColumns: { xs: "minmax(0, 1fr)", md: `minmax(0, ${ratio}fr) minmax(0, ${12 - ratio}fr)` } }}>
      <Box sx={column}>{left}</Box>
      <Box sx={column}>{right}</Box>
    </Box>
  );
}

export function Detail({ title, fields }: ThemeDetailProps) {
  return (
    <TitledCard title={title}>
      <Box component="dl" sx={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(200px, 1fr))", gap: "12px 24px", m: 0 }}>
        {fields.map((f, i) => (
          <div key={`${f.label}-${i}`}>
            <Typography component="dt" variant="caption" color="text.secondary" sx={{ textTransform: "uppercase", letterSpacing: "0.06em" }}>
              {f.label}
            </Typography>
            <Typography component="dd" variant="body2" sx={{ m: 0, mt: 0.25 }}>
              {f.value}
            </Typography>
          </div>
        ))}
      </Box>
    </TitledCard>
  );
}
