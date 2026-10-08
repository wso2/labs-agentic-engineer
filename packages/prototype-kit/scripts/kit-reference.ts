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
 * The kit reference a coding agent writes against: every export a
 * `prototype.tsx` may use, with its props and their docs, read from the
 * kit's own types — so it cannot describe a prop the kit lacks — plus the
 * finding codes. Written to `reference.md` by `gen`; the kit's test fails when
 * it is stale.
 */

import ts from "typescript";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { FINDING_CODES } from "../src/findings.js";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");

export const KIT_REFERENCE_PATH = join(root, "reference.md");

/** Exports that are the theme contract, not a prototype's vocabulary. */
const THEME_CONTRACT = new Set(["KitComponentName", "KitComponentProps", "PrototypeTheme", "ThemeRegistry", "SelectableRootProps"]);
const isThemeContract = (name: string) => THEME_CONTRACT.has(name) || name.startsWith("Theme");

const SECTIONS: [string, string[]][] = [
  ["Module", ["defineApp", "PrototypeAppDefinition"]],
  ["View hooks", ["useNav", "KitNav", "useParams", "useRole", "useDisplayState"]],
  ["Data hooks", ["useCollection", "Collection", "useValue", "useToday", "PROTOTYPE_TODAY"]],
  ["Layout", ["AppShell", "AppShellUser", "Screen", "Section", "Stack", "Grid", "Split", "Detail", "DetailField"]],
  ["Navigation", ["Navigation", "NavigationItem", "Breadcrumbs", "BreadcrumbItem", "Tabs", "Stepper", "Panel"]],
  ["Content", ["Heading", "Text", "Badge", "Stat", "StatIcon", "StatGroup", "Alert", "EmptyState", "Button", "Link", "Tone", "Pressable"]],
  ["Forms", ["Form", "Field", "FieldType", "Filters", "ValidationSummary"]],
  ["Data", ["Table", "TableColumn", "TableRow", "TableStatus", "TableAction", "Timeline", "TimelineEntry"]],
  ["Overlays", ["Dialog", "Drawer"]],
];

function doc(checker: ts.TypeChecker, symbol: ts.Symbol): string {
  return ts.displayPartsToString(symbol.getDocumentationComment(checker)).replace(/\s+/g, " ").trim();
}

function typeText(checker: ts.TypeChecker, type: ts.Type, at: ts.Node, flags = ts.TypeFormatFlags.UseAliasDefinedOutsideCurrentScope): string {
  return checker.typeToString(type, at, ts.TypeFormatFlags.NoTruncation | flags);
}

function members(checker: ts.TypeChecker, type: ts.Type, at: ts.Node): string[] {
  return checker.getPropertiesOfType(type).map((p) => {
    const optional = (p.flags & ts.SymbolFlags.Optional) !== 0;
    const d = doc(checker, p);
    // An optional prop's `| undefined` is what `?` already says.
    const type = typeText(checker, checker.getTypeOfSymbolAtLocation(p, at), at);
    return `- \`${p.name}${optional ? "?" : ""}: ${optional ? type.replace(/ \| undefined$/, "") : type}\`${d ? ` — ${d}` : ""}`;
  });
}

export function kitReference(): string {
  const entry = join(root, "src", "index.ts");
  const program = ts.createProgram([entry], {
    jsx: ts.JsxEmit.ReactJSX,
    strict: true,
    exactOptionalPropertyTypes: true,
    moduleResolution: ts.ModuleResolutionKind.Bundler,
    module: ts.ModuleKind.ESNext,
    target: ts.ScriptTarget.ES2022,
    skipLibCheck: true,
  });
  const checker = program.getTypeChecker();
  const exports = new Map(
    checker
      .getExportsOfModule(checker.getSymbolAtLocation(program.getSourceFile(entry)!)!)
      .map((s) => [s.name, s.flags & ts.SymbolFlags.Alias ? checker.getAliasedSymbol(s) : s]),
  );

  const listed = new Set(SECTIONS.flatMap(([, names]) => names));
  const unlisted = [...exports.keys()].filter((n) => !listed.has(n) && !isThemeContract(n) && !(n.endsWith("Props") && exports.has(n.slice(0, -5))));
  if (unlisted.length > 0) throw new Error(`kit-reference: place ${unlisted.join(", ")} in a section`);

  const out: string[] = [
    "# @wso2/prototype-kit reference",
    "",
    "Generated from the kit's types by `pnpm --filter @wso2/prototype-kit gen`; do not edit.",
    "",
    "A prototype is `prototype.json` (the manifest) and `prototype.tsx`, which imports only `react` and `@wso2/prototype-kit`. Every component with an `id` is an element a reviewer can point at: give each a stable id, unique on its screen.",
    "",
  ];
  for (const [title, names] of SECTIONS) {
    out.push(`## ${title}`, "");
    for (const name of names) {
      const symbol = exports.get(name);
      if (!symbol) throw new Error(`kit-reference: ${name} is not exported`);
      const decl = symbol.declarations?.[0];
      if (!decl) throw new Error(`kit-reference: ${name} has no declaration`);
      const d = doc(checker, symbol);
      if (ts.isFunctionDeclaration(decl)) {
        const signature = checker.getSignatureFromDeclaration(decl)!;
        const param = signature.getParameters()[0];
        const propsType = param && checker.getTypeOfSymbolAtLocation(param, decl);
        if (/^[A-Z]/.test(name) && propsType) {
          out.push(`### \`<${name}>\``, "", ...(d ? [d, ""] : []), ...members(checker, propsType, decl), "");
        } else {
          const text = checker.signatureToString(signature, decl, ts.TypeFormatFlags.NoTruncation);
          out.push(`### \`${name}${text}\``, "", ...(d ? [d, ""] : []));
        }
      } else if (ts.isVariableDeclaration(decl)) {
        out.push(`### \`${name}\` = \`${decl.initializer?.getText() ?? ""}\``, "", ...(d ? [d, ""] : []));
      } else {
        const type = checker.getDeclaredTypeOfSymbol(symbol);
        if (ts.isTypeAliasDeclaration(decl) && !(type.flags & ts.TypeFlags.Object)) {
          out.push(`### \`${name}\` = \`${typeText(checker, type, decl, ts.TypeFormatFlags.InTypeAlias)}\``, "", ...(d ? [d, ""] : []));
        } else {
          out.push(`### \`${name}\``, "", ...(d ? [d, ""] : []), ...members(checker, type, decl), "");
        }
      }
    }
  }
  out.push("## Finding codes", "", "`prototype check --json` prints `{ ok, findings: [{ code, file, location, message }] }` and exits 1 when there are findings.", "", "| Code | Meaning |", "|---|---|");
  for (const [code, meaning] of Object.entries(FINDING_CODES)) out.push(`| \`${code}\` | ${meaning.replace(/\|/g, "\\|")} |`);
  return `${out.join("\n").trimEnd()}\n`;
}
