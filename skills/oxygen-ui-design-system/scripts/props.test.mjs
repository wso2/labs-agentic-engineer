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

// props.mjs is the API lookup a screen runs before its JSX is written. Pinned
// here against a stand-in install carrying the declaration shapes Oxygen
// 0.13.1 ships: a plain `interface XProps extends …`, a `React.FC<P> & { … }`
// compound, a compound typed through a named interface, and a `type` alias.
// Each shape must print its props and sub-components, and a name that is not
// an Oxygen composite must say so rather than print nothing.

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import path from "node:path";

const SCRIPT = path.join(import.meta.dirname, "props.mjs");

const STAT_CARD = `import * as React from 'react';
import { CardProps } from '@mui/material/Card';
export interface StatCardProps extends CardProps {
    /**
     * The main value to display
     */
    value: string | number;
    /**
     * The label/title for the stat
     */
    label: string;
    /** Optional icon element to display */
    icon?: React.ReactNode;
    /**
     * Optional color for the icon
     */
    iconColor?: 'primary' | 'secondary' | 'success' | 'error' | 'info' | 'warning';
}
/**
 * StatCard component - A card for displaying statistics
 * @example
 * <StatCard value="42" label="Total Users" />
 */
declare const StatCard: React.FC<StatCardProps>;
export default StatCard;
`;

const PAGE_TITLE = `import React from 'react';
import { PageTitleHeader } from './PageTitleHeader';
import { PageTitleActions } from './PageTitleActions';
export interface PageTitleProps extends React.HTMLAttributes<HTMLDivElement> {
    /**
     * The content of the page title (can include Header and Actions)
     */
    children: React.ReactNode;
    /** Click handler; fires on the title only */
    onSelect?: (id: string) => void;
    'aria-label'?: string;
}
declare const PageTitle: React.FC<PageTitleProps> & {
    Header: typeof PageTitleHeader;
    Actions: typeof PageTitleActions;
};
export default PageTitle;
`;
const PAGE_TITLE_HEADER = `import { TypographyProps } from '@mui/material/Typography';
export interface PageTitleHeaderProps extends TypographyProps {
    /** The content of the header */
    children: React.ReactNode;
}
export declare const PageTitleHeader: React.FC<PageTitleHeaderProps>;
`;
const PAGE_TITLE_ACTIONS = `export interface PageTitleActionsProps extends React.HTMLAttributes<HTMLDivElement> {
    /** The action elements (buttons, switches, etc.) */
    children: React.ReactNode;
}
export declare const PageTitleActions: React.FC<PageTitleActionsProps>;
`;

const LISTING_TABLE = `import { TableProps as MuiTableProps } from '@mui/material/Table';
import ListingTableRow from './ListingTableRow';
export interface ListingTableProps extends MuiTableProps {
    /** Density of the rows */
    density?: 'compact' | 'standard';
}
interface ListingTableComponent {
    (props: ListingTableProps): JSX.Element;
    Row: typeof ListingTableRow;
}
declare const ListingTableCompound: ListingTableComponent;
export default ListingTableCompound;
`;
const LISTING_TABLE_ROW = `import { TableRowProps } from '@mui/material/TableRow';
export type ListingTableRowProps = TableRowProps & {
    /** Row shows a pointer and hover state */
    clickable?: boolean;
};
declare const ListingTableRow: React.FC<ListingTableRowProps>;
export default ListingTableRow;
`;

function fixtureApp() {
  const dir = mkdtempSync(path.join(tmpdir(), "oxygen-props-"));
  const write = (rel, text) => {
    mkdirSync(path.dirname(path.join(dir, rel)), { recursive: true });
    writeFileSync(path.join(dir, rel), text);
  };
  const base = "node_modules/@wso2/oxygen-ui";
  write(`${base}/package.json`, JSON.stringify({ name: "@wso2/oxygen-ui", version: "0.13.1" }));
  write(`${base}/dist/components/StatCard/StatCard.d.ts`, STAT_CARD);
  write(`${base}/dist/components/StatCard/index.d.ts`, `export { default } from './StatCard';\n`);
  write(`${base}/dist/components/PageTitle/PageTitle.d.ts`, PAGE_TITLE);
  write(`${base}/dist/components/PageTitle/PageTitleHeader.d.ts`, PAGE_TITLE_HEADER);
  write(`${base}/dist/components/PageTitle/PageTitleActions.d.ts`, PAGE_TITLE_ACTIONS);
  write(`${base}/dist/components/ListingTable/ListingTable/ListingTable.d.ts`, LISTING_TABLE);
  write(`${base}/dist/components/ListingTable/ListingTable/ListingTableRow.d.ts`, LISTING_TABLE_ROW);
  write(`${base}/dist/components/ListingTable/index.d.ts`, `export { default } from './ListingTable/ListingTable';\n`);
  return dir;
}

function run(args, { cwd } = {}) {
  const r = spawnSync(process.execPath, [SCRIPT, ...args], { cwd: cwd ?? tmpdir(), encoding: "utf8" });
  return { status: r.status, out: r.stdout + r.stderr };
}

test("a plain composite prints every prop with its type, requiredness and doc line", () => {
  const { status, out } = run(["--app", fixtureApp(), "StatCard"]);
  assert.equal(status, 0, out);
  assert.match(out, /@wso2\/oxygen-ui@0\.13\.1 — props from the installed \.d\.ts/);
  assert.match(out, /StatCard  \(dist\/components\/StatCard\/StatCard\.d\.ts\) — plus CardProps/);
  assert.match(out, /^  value\s+required\s+string \| number\s+— The main value to display$/m);
  assert.match(out, /^  label\s+required\s+string\s+— The label\/title for the stat$/m);
  assert.match(out, /^  icon\?\s+React\.ReactNode\s+— Optional icon element to display$/m);
  assert.match(out, /^  iconColor\?\s+'primary' \| 'secondary'.*— Optional color for the icon$/m);
  assert.match(out, /sub-components: none/);
  // What the package's prose docs claim and the types do not have must not appear as a prop.
  assert.doesNotMatch(out, /^  (title|change|trend)\b/m);
});

// The doc line printed for a prop must be ITS doc, not the accumulated docs of
// every prop above it — the first cut of this script printed the latter.
test("each prop carries only its own doc line", () => {
  const { out } = run(["--app", fixtureApp(), "StatCard"]);
  const iconLine = out.split("\n").find((l) => /^  icon\?/.test(l));
  assert.ok(iconLine, out);
  assert.doesNotMatch(iconLine, /main value/);
});

test("a React.FC & { … } compound lists its sub-components and their props", () => {
  const { status, out } = run(["--app", fixtureApp(), "PageTitle"]);
  assert.equal(status, 0, out);
  assert.match(out, /sub-components: PageTitle\.Header, PageTitle\.Actions/);
  assert.match(out, /^  PageTitle\.Header — plus TypographyProps$/m);
  assert.match(out, /^  PageTitle\.Actions — plus React\.HTMLAttributes<HTMLDivElement>$/m);
  assert.match(out, /^    children\s+required\s+React\.ReactNode\s+— The action elements/m);
});

// An arrow type contains `=>`; its `>` must not be read as closing a generic,
// or the props after it collapse into one row.
test("a function-typed prop does not swallow the props that follow it", () => {
  const { out } = run(["--app", fixtureApp(), "PageTitle"]);
  assert.match(out, /^  onSelect\?\s+\(id: string\) => void\s+— Click handler; fires on the title only$/m);
  assert.match(out, /^  aria-label\?\s+string$/m);
});

test("a compound typed through a named interface, and a `type` alias sub-component, both resolve", () => {
  const { status, out } = run(["--app", fixtureApp(), "ListingTable", "ListingTable.Row"]);
  assert.equal(status, 0, out);
  assert.match(out, /ListingTable  \(dist\/components\/ListingTable\/ListingTable\/ListingTable\.d\.ts\) — plus MuiTableProps/);
  assert.match(out, /sub-components: ListingTable\.Row/);
  assert.match(out, /ListingTable\.Row  \(dist\/components\/ListingTable\/ListingTable\/ListingTableRow\.d\.ts\) — plus TableRowProps/);
  assert.match(out, /^  clickable\?\s+boolean\s+— Row shows a pointer and hover state$/m);
});

test("a sub-component that does not exist names the ones that do", () => {
  const { status, out } = run(["--app", fixtureApp(), "PageTitle.Caption"]);
  assert.equal(status, 1);
  assert.match(out, /PageTitle has no sub-component "Caption"\. It has: PageTitle\.Header, PageTitle\.Actions/);
});

test("a plain MUI component is named as such, with its API page, and does not fail the other lookups", () => {
  const { status, out } = run(["--app", fixtureApp(), "TextField", "StatCard"]);
  assert.equal(status, 1);
  assert.match(out, /TextField: not an Oxygen composite.*https:\/\/mui\.com\/material-ui\/api\/text-field\//);
  assert.match(out, /^  value\s+required/m);
});

test("a MUI X namespace is explained rather than looked up", () => {
  const { status, out } = run(["--app", fixtureApp(), "DatePickers.DatePicker"]);
  assert.equal(status, 0, out);
  assert.match(out, /DatePickers\.DatePicker: a MUI X namespace.*@mui\/x-date-pickers/);
});

test("the App Path defaults to the cwd", () => {
  const app = fixtureApp();
  const { status, out } = run(["StatCard"], { cwd: app });
  assert.equal(status, 0, out);
  assert.match(out, /^  value\s+required/m);
});

test("before npm install it fails with the fix, not a stack trace", () => {
  const empty = mkdtempSync(path.join(tmpdir(), "oxygen-props-empty-"));
  const { status, out } = run(["--app", empty, "StatCard"]);
  assert.equal(status, 1);
  assert.match(out, /FAIL  @wso2\/oxygen-ui is not installed under/);
  assert.match(out, /fix: run from the App Path after npm install/);
  assert.doesNotMatch(out, /at \w+ \(/);
});

test("no component named prints the usage", () => {
  const { status, out } = run([]);
  assert.equal(status, 2);
  assert.match(out, /usage: node props\.mjs/);
});
