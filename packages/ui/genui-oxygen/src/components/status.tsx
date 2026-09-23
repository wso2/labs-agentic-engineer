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

import {
  Alert,
  AlertTitle,
  Box,
  Chip,
  LinearProgress,
  Typography,
} from "@wso2/oxygen-ui";
import type { GenUiPropsOf, GenUiRenderProps, GenUiTone } from "@aep/ui-genui";

/** Oxygen's Chip colour for each catalog tone. */
export const CHIP_COLOR: Record<
  GenUiTone,
  "default" | "info" | "success" | "warning" | "error"
> = {
  neutral: "default",
  info: "info",
  success: "success",
  warning: "warning",
  error: "error",
};

export function GenUiStatusChip({
  props,
}: GenUiRenderProps<GenUiPropsOf<"StatusChip">>) {
  // The Box keeps the chip at its own width inside a column Stack, which
  // would otherwise stretch it across the row.
  return (
    <Box>
      <Chip label={props.label} color={CHIP_COLOR[props.tone ?? "neutral"]} />
    </Box>
  );
}

export function GenUiAlert({ props }: GenUiRenderProps<GenUiPropsOf<"Alert">>) {
  return (
    <Alert severity={props.tone ?? "info"}>
      {props.title ? <AlertTitle>{props.title}</AlertTitle> : null}
      {props.message}
    </Alert>
  );
}

export function GenUiProgress({
  props,
}: GenUiRenderProps<GenUiPropsOf<"Progress">>) {
  return (
    <Box>
      {props.label ? (
        <Typography variant="caption" color="text.secondary" component="p">
          {props.label}
        </Typography>
      ) : null}
      <LinearProgress
        variant="determinate"
        value={props.value}
        aria-label={props.label ?? "Progress"}
      />
    </Box>
  );
}
