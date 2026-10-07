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
import { Box } from "@wso2/oxygen-ui";
import { PHONE } from "../layout";

// The base layer of the main area: a page that scrolls on its own, under any
// card drawn over it. `covered` is set while a card is open: the page stays
// in view, dimmed by the card's scrim, but out of reach of pointer, keyboard
// and screen reader until the card closes.
export function BasePage({ children, covered = false }: { children: ReactNode; covered?: boolean }) {
  return (
    <Box
      inert={covered}
      aria-hidden={covered || undefined}
      sx={{
        position: "absolute",
        inset: 0,
        overflowY: "auto",
        px: 4.5,
        pt: 3.5,
        pb: 11,
        [PHONE]: { px: 2, pt: 2.5, pb: 17.5 },
      }}
    >
      {children}
    </Box>
  );
}
