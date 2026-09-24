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

import { useState, type ReactNode } from "react";
import { Box, Button, IconButton, InputAdornment, TextField, Typography } from "@wso2/oxygen-ui";
import { Eye, EyeOff } from "@wso2/oxygen-ui-icons-react";

/** The fields of a stored credential the card shows. */
interface Masked {
  keyPrefix: string;
  keyLast4: string;
}

/**
 * A stored credential, masked, with a Replace button while `onReplace` is set
 * and any further actions (`children`) beside it.
 */
export function MaskedCredential({
  stored,
  onReplace,
  disabled,
  children,
}: {
  stored: Masked;
  onReplace?: (() => void) | undefined;
  disabled: boolean;
  children?: ReactNode;
}) {
  return (
    <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1 }}>
      <Typography variant="body2" fontFamily="monospace">
        {stored.keyPrefix}•••••••••{stored.keyLast4}
      </Typography>
      {onReplace && (
        <Button size="small" onClick={onReplace} disabled={disabled}>
          Replace
        </Button>
      )}
      {children}
    </Box>
  );
}

/**
 * A write-only credential input: masked by default with a reveal toggle, and
 * never offered to (or saved by) the browser's password manager.
 */
export function SecretField({
  label,
  placeholder,
  value,
  onChange,
  disabled,
  error,
  helperText,
  noun,
}: {
  label: string;
  placeholder: string;
  value: string;
  onChange: (value: string) => void;
  disabled: boolean;
  error: string | undefined;
  helperText: ReactNode;
  /** What the reveal toggle names: "key", "token". */
  noun: string;
}) {
  const [shown, setShown] = useState(false);
  return (
    <TextField
      label={label}
      placeholder={placeholder}
      type={shown ? "text" : "password"}
      autoComplete="new-password"
      value={value}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled}
      error={error !== undefined}
      helperText={error ?? helperText}
      fullWidth
      slotProps={{
        input: {
          endAdornment: (
            <InputAdornment position="end">
              <IconButton
                aria-label={shown ? `hide ${noun}` : `show ${noun}`}
                onClick={() => setShown((v) => !v)}
                edge="end"
              >
                {shown ? <EyeOff size={18} /> : <Eye size={18} />}
              </IconButton>
            </InputAdornment>
          ),
        },
      }}
    />
  );
}
