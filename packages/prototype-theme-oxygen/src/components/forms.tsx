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

/** Forms: form card, filter bar, field and validation summary. */

import { Alert, AlertTitle, Box, FormControlLabel, FormHelperText, Switch, TextField } from "@wso2/oxygen-ui";
import type { ThemeFieldProps, ThemeFiltersProps, ThemeFormProps, ThemeValidationSummaryProps } from "@wso2/prototype-kit";
import { TitledCard } from "./card.js";

export function Field({ inputId, label, type, value, options, error, required, placeholder, readOnly, compact, onChange }: ThemeFieldProps) {
  const size = compact ? "small" : "medium";
  const sx = compact ? { minWidth: 180 } : {};
  if (type === "switch") {
    const errorId = `${inputId}-error`;
    return (
      <Box sx={sx}>
        <FormControlLabel
          label={label}
          control={
            <Switch
              id={inputId}
              size={size}
              checked={value === "on"}
              disabled={readOnly}
              required={required}
              onChange={(e) => onChange(e.target.checked ? "on" : "off")}
              slotProps={{ input: { role: "switch", "aria-invalid": error !== undefined, ...(error !== undefined ? { "aria-describedby": errorId } : {}) } }}
            />
          }
        />
        {error !== undefined && (
          <FormHelperText id={errorId} error>
            {error}
          </FormHelperText>
        )}
      </Box>
    );
  }
  const multiline = type === "textarea";
  return (
    <TextField
      id={inputId}
      label={label}
      type={type === "number" || type === "date" ? type : "text"}
      select={type === "select"}
      multiline={multiline}
      minRows={3}
      value={value}
      required={required}
      error={error !== undefined}
      helperText={error}
      {...(placeholder !== undefined ? { placeholder } : {})}
      size={size}
      sx={sx}
      fullWidth={!compact}
      onChange={(e) => onChange(e.target.value)}
      slotProps={{
        // A native select: no popup menu to portal out of the frame's scene, and it works the same in the render check.
        select: { native: true },
        // A date, and a select with nothing chosen, show their own placeholder text: keep the label above it.
        inputLabel: type === "date" || type === "select" ? { shrink: true } : {},
        htmlInput: type === "select" ? { disabled: readOnly } : { readOnly },
      }}
    >
      {type === "select" && (
        <>
          {value === "" && <option value="">{placeholder ?? "Choose…"}</option>}
          {options.map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </>
      )}
    </TextField>
  );
}

export function Form({ title, summary, children, actions, onSubmit }: ThemeFormProps) {
  return (
    <TitledCard title={title}>
      <Box
        component="form"
        noValidate
        onSubmit={(e) => e.preventDefault()}
        onKeyDown={(e) => {
          if (e.key === "Enter" && e.target instanceof HTMLInputElement && e.target.type !== "checkbox") {
            e.preventDefault();
            onSubmit();
          }
        }}
        sx={{ display: "flex", flexDirection: "column", gap: 2.5 }}
      >
        {summary}
        {children}
        {actions !== undefined && <Box sx={{ display: "flex", gap: 1, justifyContent: "flex-end" }}>{actions}</Box>}
      </Box>
    </TitledCard>
  );
}

export function Filters({ children }: ThemeFiltersProps) {
  return <Box sx={{ display: "flex", gap: 1.5, flexWrap: "wrap", alignItems: "flex-end" }}>{children}</Box>;
}

export function ValidationSummary({ title, issues }: ThemeValidationSummaryProps) {
  return (
    <Alert severity="error" role="alert">
      <AlertTitle>{title}</AlertTitle>
      <Box component="ul" sx={{ m: 0, pl: 2.5 }}>
        {issues.map((issue, i) => (
          <li key={`${issue}-${i}`}>{issue}</li>
        ))}
      </Box>
    </Alert>
  );
}
