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

import type { PropsWithChildren } from "react";
import { Box, Button, CircularProgress, Typography } from "@wso2/oxygen-ui";
import { Link } from "@tanstack/react-router";

// The two pages AeStudioGate puts in place of the console. Full-viewport and
// centred like the onboarding gate's own loading and error states: the shell
// is not rendered behind either, because nothing in it works yet.

// The session's first answer was `provisioning` (an upgrade on visit): the
// whole console waits here until AE Studio is ready.
export function AeStudioHold() {
  return (
    <FullScreen>
      <CircularProgress size={32} />
      <Typography variant="h6">Upgrading AE Studio</Typography>
      <Typography variant="body2" color="text.secondary">
        This takes a minute or two.
      </Typography>
    </FullScreen>
  );
}

// AE Studio failed to start. Try again re-reads the state (a GET is also what
// starts a new converge); Settings stays reachable, since a setting is the
// usual fix.
export function AeStudioFailed({
  onRetry,
  retrying,
}: {
  onRetry: () => void;
  retrying: boolean;
}) {
  return (
    <FullScreen>
      <Typography variant="h6">AE Studio couldn't start</Typography>
      <Box sx={{ display: "flex", gap: 1.5 }}>
        <Button variant="contained" onClick={onRetry} disabled={retrying}>
          Try again
        </Button>
        <Button variant="text" component={Link} to="/settings">
          Open Settings
        </Button>
      </Box>
    </FullScreen>
  );
}

function FullScreen({ children }: PropsWithChildren) {
  return (
    <Box
      sx={{
        minHeight: "100vh",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        gap: 2,
        textAlign: "center",
        bgcolor: "background.default",
      }}
    >
      {children}
    </Box>
  );
}
