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
  Box,
  Button,
  Checkbox,
  FormControl,
  FormControlLabel,
  FormHelperText,
  MenuItem,
  TextField,
} from "@wso2/oxygen-ui";
import type { GenUiPropsOf, GenUiRenderProps } from "@aep/ui-genui";

type Variant = NonNullable<GenUiPropsOf<"Button">["variant"]>;

const BUTTON_PROPS: Record<
  Variant,
  { variant: "contained" | "outlined"; color: "primary" | "error" }
> = {
  primary: { variant: "contained", color: "primary" },
  secondary: { variant: "outlined", color: "primary" },
  danger: { variant: "outlined", color: "error" },
};

export function GenUiButton({
  props,
  emit,
}: GenUiRenderProps<GenUiPropsOf<"Button">>) {
  // The Box keeps the button at its own width inside a column Stack, which
  // would otherwise stretch it across the row.
  return (
    <Box>
      <Button
        {...BUTTON_PROPS[props.variant ?? "primary"]}
        disabled={props.disabled ?? false}
        onClick={() => emit("press")}
      >
        {props.label}
      </Button>
    </Box>
  );
}

/**
 * Each kind of Field is the stock Oxygen input for it. TypeScript narrows
 * props by kind, so options exist only for select and a checkbox's value is
 * a boolean. Every kind writes back through setProp("value", …).
 */
export function GenUiField({ props, setProp }: GenUiRenderProps<GenUiPropsOf<"Field">>) {
  const required = props.required ?? false;
  const help = props.error ?? props.helperText;
  switch (props.type) {
    case "checkbox":
      return (
        <FormControl required={required} error={Boolean(props.error)}>
          <FormControlLabel
            label={props.label}
            control={
              <Checkbox
                checked={props.value ?? false}
                onChange={(event) => setProp("value", event.target.checked)}
              />
            }
          />
          {help ? <FormHelperText>{help}</FormHelperText> : null}
        </FormControl>
      );
    case "select":
      return (
        <TextField
          select
          label={props.label}
          required={required}
          value={props.value ?? ""}
          onChange={(event) => setProp("value", event.target.value)}
          error={Boolean(props.error)}
          helperText={help}
        >
          {props.options.map((option) => (
            <MenuItem key={option.value} value={option.value}>
              {option.label}
            </MenuItem>
          ))}
        </TextField>
      );
    default:
      return (
        <TextField
          label={props.label}
          type={props.type === "textarea" ? "text" : props.type}
          {...(props.type === "textarea" ? { multiline: true, minRows: 3 } : {})}
          required={required}
          value={props.value ?? ""}
          onChange={(event) => setProp("value", event.target.value)}
          error={Boolean(props.error)}
          helperText={help}
        />
      );
  }
}
