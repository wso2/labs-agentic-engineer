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

import { Box, Button, CircularProgress, Typography } from "@wso2/oxygen-ui";

// Full-viewport auth screen: spinner while redirecting to or returning from
// Thunder, message + retry when the handshake fails, and the signed-out state.
export function AuthScreen({
  label,
  error,
  onRetry,
  onSignIn,
}: {
  label: string;
  error?: string;
  onRetry?: () => void;
  /** Signed out on purpose: say so and offer to sign in again. */
  onSignIn?: () => void;
}) {
  return (
    <Box
      sx={{
        minHeight: "100vh",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        gap: 2,
        bgcolor: "background.default",
      }}
    >
      {onSignIn ? (
        <>
          <Typography variant="h6">You're signed out</Typography>
          <Button variant="contained" onClick={onSignIn}>
            Sign in
          </Button>
        </>
      ) : error ? (
        <>
          <Typography variant="h6">Sign-in failed</Typography>
          <Typography variant="body2" color="text.secondary">
            {error}
          </Typography>
          {onRetry && (
            <Button variant="contained" onClick={onRetry}>
              Try again
            </Button>
          )}
        </>
      ) : (
        <>
          <CircularProgress size={32} />
          <Typography variant="body2" color="text.secondary">
            {label}
          </Typography>
        </>
      )}
    </Box>
  );
}
