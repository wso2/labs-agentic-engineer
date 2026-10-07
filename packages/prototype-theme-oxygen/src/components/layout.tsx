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

/** Layout: the bare screen, the page every screen's content sits on, section, stack, grid, split and detail record. */

import { Box, Chip, PageContent, Typography } from "@wso2/oxygen-ui";
import type { ThemeDetailProps, ThemeGridProps, ThemeScreenProps, ThemeSectionProps, ThemeSplitProps, ThemeStackProps } from "@wso2/prototype-kit";
import type { ReactNode } from "react";
import { TitledCard } from "./card.js";

/**
 * A screen's content on Oxygen's `PageContent`: centred, padded, its parts a
 * column apart, and no wider than a table's columns read well (Oxygen's
 * default, 1400px, leaves tables stretched thin on a wide window).
 */
export function Page({ children }: { children?: ReactNode }) {
  return (
    <PageContent maxWidth={1200} sx={{ display: "flex", flexDirection: "column", gap: 2.5 }}>
      {children}
    </PageContent>
  );
}

/** A part of a page's title row: a heading a step below the page title, an optional count, a subtitle and the part's own actions. */
export function SectionHeader({ title, count, subtitle, actions }: Omit<ThemeSectionProps, "children">) {
  return (
    <Box sx={{ display: "flex", alignItems: "flex-start", justifyContent: "space-between", gap: 2, flexWrap: "wrap" }}>
      <div>
        <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
          <Typography variant="h4" component="h2" sx={{ fontWeight: 500 }}>
            {title}
          </Typography>
          {count !== undefined && <Chip size="small" label={count} />}
        </Box>
        {subtitle && (
          <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
            {subtitle}
          </Typography>
        )}
      </div>
      {actions !== undefined && <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>{actions}</Box>}
    </Box>
  );
}

/** Its header close to its content, and a little more room above it than between a page's other parts. */
export function Section({ title, subtitle, count, actions, children }: ThemeSectionProps) {
  return (
    <Box component="section" sx={{ display: "flex", flexDirection: "column", gap: 1.5, mt: 1 }}>
      <SectionHeader title={title} subtitle={subtitle} count={count} actions={actions} />
      {children}
    </Box>
  );
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
