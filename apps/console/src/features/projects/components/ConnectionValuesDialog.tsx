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
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  Typography,
} from "@wso2/oxygen-ui";
import { X } from "@wso2/oxygen-ui-icons-react";
import { ConnectionValueFields } from "@aep/ui-connection-value-fields";
import { useSaveConnectionValues } from "../api/queries";
import type { components } from "../../../generated/aep-api";

type ConfigKey = components["schemas"]["ConfigKey"];

/**
 * What this dialog needs of a dependency: a name to save under and the keys to
 * collect. Deliberately NOT `ConnectionRow` — the Builds page projects its own
 * row type (external-only, keys unioned across consumers) and both satisfy this
 * shape structurally, so two callers share one dialog without either lib
 * bending to the other.
 */
export interface ValuesDialogResource {
  name: string;
  config: ConfigKey[];
}

/** Plain defaults the design already declares are legitimate initial input; a
 *  SECRET default is not — echoing one into a field would put a shared dummy
 *  credential a click away from being saved as the real value. */
function seedPlainDefaults(config: ConfigKey[]): Record<string, string> {
  return Object.fromEntries(
    config
      .filter((key) => !key.secret && key.defaultValue !== undefined)
      .map((key) => [key.key, key.defaultValue ?? ""]),
  );
}

// Collect an external resource's values for an environment. Two callers: the
// Deployments page (#395 — the build-time drawer collected placeholders and this
// hands the platform the real ones), and the Builds page's External resources
// section, where a run parked on the deploy gate is waiting for them.
//
// The fields come from the resource's own config schema, and stored values are
// WRITE-ONLY: secrets go to the secret manager and are never echoed back, so a
// key already set opens EMPTY rather than pretending to show what is stored.
// Saving re-authors the resource and the platform reconciles the new values in.

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
  connection: ValuesDialogResource;
  environment: string;
}) {
  const save = useSaveConnectionValues(projectName, connection.name);
  const [values, setValues] = useState<Record<string, string>>(() =>
    seedPlainDefaults(connection.config),
  );
  // Reseed on CLOSE, never while open — a reseed mid-entry would wipe the field
  // the user is typing into. Keyed on the resource too, so opening a second one
  // starts from its own defaults rather than the first one's.
  useEffect(() => {
    if (!open) {
      setValues(seedPlainDefaults(connection.config));
      save.reset();
    }
    // save.reset is stable (react-query); depending on `save` would loop.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, connection.name]);

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
          New {environment} values for this resource. Stored values never
          display here — saving replaces them all.
        </Typography>
        <ConnectionValueFields
          config={connection.config}
          values={values}
          onValueChange={(key, value) =>
            setValues((current) => ({ ...current, [key]: value }))
          }
        />
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
          {...(!complete && { title: "Enabled when every value is set" })}
        >
          <Button
            variant="contained"
            disabled={!complete || save.isPending}
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
