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

import type { GenUiPropsOf, GenUiRenderProps } from "@aep/ui-genui";
import {
  Card,
  CardDescription,
  CardHeader,
  CardTitle,
} from "#shadcn/components/ui/card";
import {
  Item,
  ItemContent,
  ItemDescription,
  ItemGroup,
  ItemTitle,
} from "#shadcn/components/ui/item";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "#shadcn/components/ui/table";

export function GenUiKeyValueList({
  props,
}: GenUiRenderProps<GenUiPropsOf<"KeyValueList">>) {
  // shadcn has no summary-strip component; this is a plain description list
  // in its muted-label style.
  if (props.layout === "columns") {
    return (
      <dl className="grid grid-cols-[repeat(auto-fit,minmax(8rem,1fr))] gap-4">
        {props.items.map((item, index) => (
          <div key={`${index}-${item.label}`} className="flex flex-col gap-1">
            <dt className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
              {item.label}
            </dt>
            <dd className="text-sm">{item.value}</dd>
          </div>
        ))}
      </dl>
    );
  }
  return (
    <ItemGroup>
      {props.items.map((item, index) => (
        // Labels may repeat in model output, so the index keeps keys unique.
        <Item key={`${index}-${item.label}`} size="sm">
          <ItemContent>
            <ItemTitle>{item.value}</ItemTitle>
            <ItemDescription>{item.label}</ItemDescription>
          </ItemContent>
        </Item>
      ))}
    </ItemGroup>
  );
}

export function GenUiDataTable({
  props,
}: GenUiRenderProps<GenUiPropsOf<"DataTable">>) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          {props.columns.map((column) => (
            <TableHead key={column.key}>{column.label}</TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {props.rows.map((row, rowIndex) => (
          <TableRow key={rowIndex}>
            {props.columns.map((column) => (
              <TableCell key={column.key}>{row[column.key] ?? "—"}</TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

// The metric card from shadcn's dashboard block (section-cards). That block
// lays the cards out in a grid; here they sit in a Stack row instead, and
// CardHeader is a container-query container, so its content does not size
// the card. The floor width and the even share of the row stand in for the
// grid's columns.
export function GenUiMetric({ props }: GenUiRenderProps<GenUiPropsOf<"Metric">>) {
  return (
    <Card className="min-w-40 flex-1">
      <CardHeader>
        <CardDescription>{props.label}</CardDescription>
        <CardTitle className="text-2xl font-semibold tabular-nums">
          {props.value}
        </CardTitle>
      </CardHeader>
    </Card>
  );
}

// shadcn has no code block component; this is the muted pre its docs use.
// Unlike Oxygen's CodeBlock it does not syntax-highlight.
export function GenUiCodeSnippet({
  props,
}: GenUiRenderProps<GenUiPropsOf<"CodeSnippet">>) {
  return (
    <pre className="overflow-x-auto rounded-lg bg-muted p-4 text-sm">
      <code data-language={props.language}>{props.code}</code>
    </pre>
  );
}
