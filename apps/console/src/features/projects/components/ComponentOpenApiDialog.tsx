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
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Typography,
} from "@wso2/oxygen-ui";
import { OpenApiView } from "@aep/ui-openapi-view";
import { useApiViewSecurity } from "../../spec/hooks/useApiViewSecurity";
import { useComponentOpenApi } from "../api/queries";

// Renders a service component's OpenAPI contract in-app via the shared
// OpenApiView. Replaces the old "API contract" link, which navigated the
// browser straight to the JWT-guarded /openapi endpoint and 401'd (a raw
// navigation carries no Bearer token). Fetching through the authenticated
// client fixes that; the viewer also renders the spec instead of dumping the
// endpoint's `{ spec }` JSON envelope into a new tab.
export function ComponentOpenApiDialog({
  projectName,
  componentName,
  onClose,
}: {
  projectName: string;
  componentName: string | null;
  onClose: () => void;
}) {
  const open = componentName !== null;
  const { data, isLoading, isError, error } = useComponentOpenApi(
    projectName,
    componentName ?? "",
    open,
  );
  // Who grants each scope and which audience they are on. There is no collab
  // room here — this dialog opens outside the Spec workspace — so it reads the
  // committed design, which is the right copy anyway for a contract the
  // platform has already built.
  const security = useApiViewSecurity({ projectName, active: open });

  return (
    <Dialog open={open} onClose={onClose} maxWidth="lg" fullWidth>
      <DialogTitle>
        <Typography variant="h6" fontWeight={700}>
          {componentName} · API contract
        </Typography>
      </DialogTitle>

      {/* OpenApiView owns its own padding + scroll (height: 100%), so the
          content area is zero-padding and height-bounded. */}
      <DialogContent dividers sx={{ p: 0, height: "80vh" }}>
        {isLoading && (
          <Box sx={{ display: "flex", justifyContent: "center", py: 6 }}>
            <CircularProgress />
          </Box>
        )}

        {isError && (
          <Box sx={{ p: 3 }}>
            <Alert severity="error">
              {error?.message ?? "Failed to load the API contract"}
            </Alert>
          </Box>
        )}

        {data && (
          <OpenApiView
            spec={data.spec}
            roles={security.roles}
            resourceServer={security.resourceServer}
          />
        )}
      </DialogContent>

      <DialogActions>
        <Button variant="contained" onClick={onClose}>
          Close
        </Button>
      </DialogActions>
    </Dialog>
  );
}
