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

/** Layout: the bare screen, section, stack, grid, split and the read-only detail record (the app shell is in shell.tsx). */

import type { ReactNode } from "react";
import { SelectableBox, requireId } from "../runtime/selectable.js";
import { useThemed } from "../theme/context.js";

export interface ScreenProps {
  /** Navigation for an app drawn without `<AppShell>`: one `<Navigation>`, usually shared by every screen. */
  nav?: ReactNode;
  children?: ReactNode;
}

export interface ThemeScreenProps {
  nav?: ReactNode;
  children?: ReactNode;
}

/** The root of a screen outside the app shell (signed out, a landing page): its content, and navigation if any. */
export function Screen({ nav, children }: ScreenProps) {
  const Themed = useThemed("Screen");
  return <Themed nav={nav}>{children}</Themed>;
}

export interface SectionProps {
  id: string;
  title: string;
  /** One line under the title: what the section holds or why. */
  subtitle?: string | undefined;
  /** How many records the section lists, shown beside the title. */
  count?: number | undefined;
  /** The section's own Buttons ("New request" above the requests it adds to). */
  actions?: ReactNode;
  children?: ReactNode;
}

export interface ThemeSectionProps {
  title: string;
  subtitle?: string | undefined;
  count?: number | undefined;
  actions?: ReactNode;
  children?: ReactNode;
}

/** A titled part of a screen — a table, a form, a group of stats — with the actions that belong to it. */
export function Section({ id, title, subtitle, count, actions, children }: SectionProps) {
  const Themed = useThemed("Section");
  return (
    <SelectableBox id={requireId("Section", id)} label={title} container>
      <Themed title={title} subtitle={subtitle} count={count} actions={actions}>
        {children}
      </Themed>
    </SelectableBox>
  );
}

export interface StackProps {
  direction?: "row" | "column" | undefined;
  children?: ReactNode;
}

export interface ThemeStackProps {
  direction: "row" | "column";
  children?: ReactNode;
}

/** Children in a column (default) or a wrapping row. */
export function Stack({ direction = "column", children }: StackProps) {
  const Themed = useThemed("Stack");
  return <Themed direction={direction}>{children}</Themed>;
}

export interface GridProps {
  /** 1–6 equal columns (one column on a narrow window). */
  columns: number;
  children?: ReactNode;
}

export interface ThemeGridProps {
  /** Already clamped to 1–6. */
  columns: number;
  children?: ReactNode;
}

/** Children in 1–6 equal columns. */
export function Grid({ columns, children }: GridProps) {
  const Themed = useThemed("Grid");
  return <Themed columns={Math.min(6, Math.max(1, Math.trunc(columns) || 1))}>{children}</Themed>;
}

export interface SplitProps {
  left: ReactNode;
  right: ReactNode;
  /** The left pane's share of 12 columns (default 6). */
  ratio?: number | undefined;
}

export interface ThemeSplitProps {
  left: ReactNode;
  right: ReactNode;
  /** Already clamped to 1–11 (of 12). */
  ratio: number;
}

/** Two panes side by side (stacked on a narrow window). */
export function Split({ left, right, ratio = 6 }: SplitProps) {
  const Themed = useThemed("Split");
  return <Themed left={left} right={right} ratio={Math.min(11, Math.max(1, Math.trunc(ratio) || 6))} />;
}

export interface DetailField {
  label: string;
  value: string;
}

export interface DetailProps {
  id: string;
  title?: string | undefined;
  fields: DetailField[];
}

export interface ThemeDetailProps {
  title?: string | undefined;
  fields: DetailField[];
}

/** A read-only record: label/value pairs. */
export function Detail({ id, title, fields }: DetailProps) {
  const Themed = useThemed("Detail");
  return (
    <SelectableBox id={requireId("Detail", id)} label={title ?? fields[0]?.label ?? "Detail"}>
      <Themed title={title} fields={fields} />
    </SelectableBox>
  );
}
