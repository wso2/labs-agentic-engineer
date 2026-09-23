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

import { cn } from "cn";
import type { GenUiPropsOf, GenUiRenderProps } from "@aep/ui-genui";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "#shadcn/components/ui/card";
import { Separator } from "#shadcn/components/ui/separator";

// A catalog gap step is 8px, as in Oxygen, so both design systems space a
// spec the same. Written out in full so Tailwind finds every class.
const GAP = [
  "gap-0",
  "gap-2",
  "gap-4",
  "gap-6",
  "gap-8",
  "gap-10",
  "gap-12",
] as const;

// shadcn has no stack component; this is the plain flex layout its own
// examples use. Rows centre their items and do not wrap, as the Oxygen Stack
// does.
export function GenUiStack({
  props,
  children,
}: GenUiRenderProps<GenUiPropsOf<"Stack">>) {
  return (
    <div
      className={cn(
        "flex",
        props.direction === "row" ? "flex-row items-center" : "flex-col",
        props.justify === "between" && "justify-between",
        GAP[props.gap ?? 2],
      )}
    >
      {children}
    </div>
  );
}

export function GenUiCard({
  props,
  children,
}: GenUiRenderProps<GenUiPropsOf<"Card">>) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{props.title}</CardTitle>
        {props.subtitle ? <CardDescription>{props.subtitle}</CardDescription> : null}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">{children}</CardContent>
    </Card>
  );
}

export function GenUiDivider() {
  return <Separator />;
}
