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

import { Box } from "@wso2/oxygen-ui";

/** A key to press, as the keyboard labels it ("C", "Esc"), in the surrounding text's size. */
export function Key({ children }: { children: string }) {
  return (
    <Box
      component="kbd"
      sx={{
        font: "inherit",
        fontSize: "0.6875rem",
        fontWeight: 600,
        lineHeight: "16px",
        minWidth: 18,
        px: 0.625,
        border: 1,
        borderColor: "divider",
        borderRadius: 0.5,
        color: "text.secondary",
        textAlign: "center",
      }}
    >
      {children}
    </Box>
  );
}
