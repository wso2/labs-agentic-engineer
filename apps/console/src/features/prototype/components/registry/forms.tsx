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

// Forms: form, field, validation-summary. Every input is read-only — a
// prototype shows what the form holds, it never submits it.

import {
  Alert,
  AlertTitle,
  Box,
  Card,
  CardContent,
  CardHeader,
  FormControlLabel,
  MenuItem,
  Stack,
  Switch,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import type { PrototypeField, PrototypeFormNode, PrototypeValidationSummaryNode } from "@aep/prototype-model";
import { fieldErrorIn } from "../../model/visibility";
import { Selectable, usePrototypeRender } from "../renderContext";
import { ActionButton } from "./content";

const SWITCH_ON = new Set(["on", "true", "yes"]);

/** One field of a form or filter bar, read-only, showing its error in the states that declare it. */
export function FieldView({ field, compact }: { field: PrototypeField; compact?: boolean }) {
  const { view } = usePrototypeRender();
  const error = fieldErrorIn(field, view.stateId);
  const value = field.value ?? "";
  let control;
  if (field.type === "switch") {
    control = (
      <FormControlLabel
        label={field.label}
        control={<Switch checked={SWITCH_ON.has(value.toLowerCase())} slotProps={{ input: { readOnly: true } }} />}
      />
    );
  } else {
    const options = field.options ?? [];
    const select = field.type === "select";
    control = (
      <TextField
        label={field.label}
        size={compact ? "small" : "medium"}
        fullWidth={!compact}
        select={select}
        multiline={field.type === "textarea"}
        {...(field.type === "textarea" ? { minRows: 3 } : {})}
        value={value}
        error={Boolean(error)}
        {...(error ? { helperText: error } : {})}
        slotProps={{ input: { readOnly: true } }}
        {...(compact ? { sx: { minWidth: 180 } } : {})}
      >
        {select &&
          (options.includes(value) || value === "" ? options : [value, ...options]).map((o) => (
            <MenuItem key={o} value={o}>
              {o}
            </MenuItem>
          ))}
      </TextField>
    );
  }
  return (
    <Selectable id={field.id} inline={Boolean(compact)}>
      {control}
    </Selectable>
  );
}

export function FormView({ node }: { node: PrototypeFormNode }) {
  return (
    <Card variant="outlined">
      {node.title && <CardHeader title={node.title} />}
      <CardContent>
        <Box component="form" noValidate onSubmit={(e) => e.preventDefault()}>
          <Stack spacing={2}>
            {node.fields.map((f) => (
              <FieldView key={f.id} field={f} />
            ))}
            {node.actions.length > 0 && (
              <Stack direction="row" spacing={1} justifyContent="flex-end">
                {node.actions.map((b) => (
                  <ActionButton key={b.id} button={b} />
                ))}
              </Stack>
            )}
          </Stack>
        </Box>
      </CardContent>
    </Card>
  );
}

export function ValidationSummaryView({ node }: { node: PrototypeValidationSummaryNode }) {
  const count = node.issues.length;
  return (
    <Alert severity="error">
      <AlertTitle>{count === 1 ? "Fix 1 problem" : `Fix ${count} problems`}</AlertTitle>
      <Stack component="ul" spacing={0.5} sx={{ m: 0, pl: 2 }}>
        {node.issues.map((issue) => (
          <Typography key={issue} component="li" variant="body2">
            {issue}
          </Typography>
        ))}
      </Stack>
    </Alert>
  );
}
