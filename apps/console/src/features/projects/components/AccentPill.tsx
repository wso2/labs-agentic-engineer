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

import type { ReactNode } from "react";
import { Button, alpha, type ButtonProps } from "@wso2/oxygen-ui";

/**
 * The console's tinted-pill recipe, in the app's accent: an ACTION among
 * readouts must out-rank its neighbours' quiet captions, and this is the one
 * shape for "a pill you can press" — Configure on a connection row, Try API
 * and Visit on a deployed component. Lifted out of the Deployments page so
 * the three read as one control instead of three near-copies.
 */
export function AccentPill({
  children,
  startIcon,
  ...rest
}: Omit<ButtonProps, "children" | "startIcon"> & {
  children: ReactNode;
  startIcon?: ReactNode;
  /** Anchor attributes for a pill that is a link (Visit on a deployed web
   *  app). MUI's Button accepts them through `href`, but only types them on
   *  the anchor overload. */
  target?: string;
  rel?: string;
}) {
  return (
    <Button
      size="small"
      color="inherit"
      disableElevation
      {...(startIcon ? { startIcon } : {})}
      {...rest}
      sx={(theme) => ({
        borderRadius: 999,
        minWidth: 0,
        px: 1.25,
        py: 0.25,
        flexShrink: 0,
        fontWeight: 600,
        fontSize: theme.typography.body2.fontSize,
        lineHeight: 1.6,
        color: "primary.main",
        border: `1px solid ${alpha(theme.palette.primary.main, 0.3)}`,
        bgcolor: alpha(theme.palette.primary.main, 0.14),
        transition: "background-color 120ms, border-color 120ms",
        "&:hover, &.Mui-focusVisible": {
          bgcolor: alpha(theme.palette.primary.main, 0.24),
          borderColor: alpha(theme.palette.primary.main, 0.5),
        },
        "&.Mui-focusVisible": {
          outline: `2px solid ${alpha(theme.palette.primary.main, 0.6)}`,
          outlineOffset: 2,
        },
      })}
    >
      {children}
    </Button>
  );
}
