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

import type { ComponentType } from "react";
import { Card, CardContent, CardHeader, Divider, Stack } from "@wso2/oxygen-ui";
import type { GenUiPropsOf, GenUiRenderProps } from "@aep/ui-genui";

export function GenUiStack({
  props,
  children,
}: GenUiRenderProps<GenUiPropsOf<"Stack">>) {
  const row = props.direction === "row";
  // Layout only: rows centre their items and can push them apart. Stack's
  // own look is untouched.
  return (
    <Stack
      direction={row ? "row" : "column"}
      spacing={props.gap ?? 2}
      sx={{
        ...(row ? { alignItems: "center" } : {}),
        ...(props.justify === "between" ? { justifyContent: "space-between" } : {}),
      }}
    >
      {children}
    </Stack>
  );
}

export function GenUiCard({
  props,
  children,
}: GenUiRenderProps<GenUiPropsOf<"Card">>) {
  return (
    <Card>
      <CardHeader title={props.title} subheader={props.subtitle} />
      <CardContent>
        <Stack spacing={2}>{children}</Stack>
      </CardContent>
    </Card>
  );
}

// Divider takes no props; the parameter is omitted rather than left unused.
export const GenUiDivider: ComponentType<GenUiRenderProps<GenUiPropsOf<"Divider">>> =
  () => <Divider />;
