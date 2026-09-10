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

import { useEffect, useState } from "react";
import {
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { X } from "@wso2/oxygen-ui-icons-react";
import { useHasPermission } from "../../../auth/permissions";
import { useSaveConnectionValues } from "../api/queries";
import type { ConnectionRow } from "../lib/promotion";

// Update an external connection's values for an environment (#395: the
// build-time drawer collected them once — often as placeholders — and this
// is the way to hand the platform the real ones afterwards). The fields come
// from the connection's own config schema; values are WRITE-ONLY: secrets go
// to the secret manager and are never echoed back, so the form always opens
// empty rather than pretending to show what is stored. Saving re-authors the
// connection's resource and the platform reconciles the new values in.

export function ConnectionValuesDialog({
  open,
  onClose,
  onSaved,
  projectName,
  connection,
  /** The environment the values are for — dev on this page. */
  environment,
}: {
  open: boolean;
  onClose: () => void;
  /** Called after a successful save (the page owns the confirmation). */
  onSaved: () => void;
  projectName: string;
  connection: ConnectionRow;
  environment: string;
}) {
  const hasBuild = useHasPermission("ae:build");
  const save = useSaveConnectionValues(projectName);
  const [values, setValues] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!open) {
      setValues({});
      save.reset();
    }
    // save.reset is stable (react-query); depending on `save` would loop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const complete = connection.config.every(
    (k) => (values[k.key] ?? "").trim() !== "",
  );

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle sx={{ pr: 6 }}>
        Configure — {connection.name}
        <IconButton
          aria-label="Close"
          onClick={onClose}
          size="small"
          sx={{ position: "absolute", right: 12, top: 12 }}
        >
          <X size={18} />
        </IconButton>
      </DialogTitle>
      <DialogContent>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          New {environment} values for this connection. Stored values never
          display here — saving replaces them all.
        </Typography>
        <Box
          sx={{
            display: "grid",
            gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr" },
            gap: 1.25,
          }}
        >
          {connection.config.map((key) => (
            // The KEY is the label (short, stable); the schema's description
            // goes below as helper text, which WRAPS — a floating label
            // truncates it to one clipped line (#401 feedback).
            <TextField
              key={key.key}
              label={key.key}
              {...(key.description && { helperText: key.description })}
              size="small"
              fullWidth
              {...(key.secret && { type: "password" })}
              value={values[key.key] ?? ""}
              onChange={(e) =>
                setValues((v) => ({ ...v, [key.key]: e.target.value }))
              }
              disabled={!hasBuild}
              sx={{ "& input": { fontFamily: "monospace" } }}
            />
          ))}
        </Box>
        {save.isError && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {save.error instanceof Error && save.error.message
              ? save.error.message
              : "Failed to save the connection's values"}
          </Alert>
        )}
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2.5 }}>
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ flexGrow: 1, minWidth: 0 }}
        >
          Secrets are stored in the platform's secret manager.
        </Typography>
        <Button onClick={onClose} variant="outlined" color="inherit">
          Cancel
        </Button>
        <span
          {...(!hasBuild
            ? { title: "You don't have permission to configure this connection." }
            : !complete && { title: "Enabled when every value is set" })}
        >
          <Button
            variant="contained"
            disabled={!complete || save.isPending || !hasBuild}
            onClick={() =>
              save.mutate(
                {
                  name: connection.name,
                  environment,
                  values,
                },
                { onSuccess: onSaved },
              )
            }
          >
            {save.isPending ? "Saving…" : "Save values"}
          </Button>
        </span>
      </DialogActions>
    </Dialog>
  );
}
