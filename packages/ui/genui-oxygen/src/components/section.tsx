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
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Chip,
  Stack,
  Typography,
} from "@wso2/oxygen-ui";
import { ChevronDown } from "@wso2/oxygen-ui-icons-react";
import type { GenUiPropsOf, GenUiRenderProps } from "@aep/ui-genui";
import { CHIP_COLOR } from "./status.js";

export function GenUiSection({
  props,
  children,
}: GenUiRenderProps<GenUiPropsOf<"Section">>) {
  return (
    <Accordion defaultExpanded={!props.collapsed}>
      <AccordionSummary expandIcon={<ChevronDown />}>
        <Stack direction="row" spacing={1.5} sx={{ alignItems: "center" }}>
          <Typography variant="subtitle1" component="h3">
            {props.title}
          </Typography>
          {props.summary ? (
            <Typography variant="caption" color="text.secondary">
              {props.summary}
            </Typography>
          ) : null}
          {props.badge ? (
            <Chip
              label={props.badge.label}
              color={CHIP_COLOR[props.badge.tone ?? "neutral"]}
            />
          ) : null}
        </Stack>
      </AccordionSummary>
      <AccordionDetails>
        <Stack spacing={2}>{children}</Stack>
      </AccordionDetails>
    </Accordion>
  );
}
