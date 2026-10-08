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

import { useEffect, useRef, type ReactNode } from "react";
import { Box, IconButton, Tooltip, keyframes } from "@wso2/oxygen-ui";
import { X } from "@wso2/oxygen-ui-icons-react";
import { PHONE } from "../layout";

const slideIn = keyframes`
  from { opacity: 0; transform: translateX(12px); }
  to { opacity: 1; transform: none; }
`;

const fadeIn = keyframes`
  from { opacity: 0; }
  to { opacity: 1; }
`;

const reducedMotion = { "@media (prefers-reduced-motion: reduce)": { animation: "none" } };

/**
 * What every Card looks like over its Page: the scrim, the framed card with
 * its header (the card's own `heading`, its `actions`, a close button) and
 * its body. Closing (X, Escape, or a click on the scrim) calls `onClose`; the
 * caller knows which Page that is (a project's card, `CardOverlay`; the org's
 * Settings, the Dashboard).
 *
 * A `fill` body is laid out by its children (a card with its own menu and
 * scrolling pane); otherwise the body is one padded scroll. Focus moves into
 * the card as it opens, and again whenever `openKey` changes.
 */
export function CardFrame({
  name,
  heading,
  closeHint,
  onClose,
  actions,
  fill = false,
  openKey,
  children,
}: {
  /** The card's accessible name. */
  name: string;
  heading: ReactNode;
  /** The close button's tooltip: where closing goes. */
  closeHint: string;
  onClose: () => void;
  actions?: ReactNode;
  fill?: boolean;
  openKey?: string;
  children: ReactNode;
}) {
  const cardRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    // Menus and dialogs above the card stop their own Escape from reaching here.
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !e.defaultPrevented) onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  // Focus moves into the card as it opens, so keyboard and screen reader land
  // on what is now in front.
  useEffect(() => {
    cardRef.current?.focus();
  }, [openKey]);

  return (
    <>
      <Box
        aria-hidden
        onClick={onClose}
        sx={{
          position: "absolute",
          inset: 0,
          bgcolor: "var(--aep-shell-scrim)",
          animation: `${fadeIn} 0.2s ease`,
          ...reducedMotion,
        }}
      />
      <Box
        ref={cardRef}
        role="dialog"
        aria-label={name}
        tabIndex={-1}
        sx={{
          // Framed on all sides, the page showing round it under the scrim.
          position: "absolute",
          inset: (t) => t.spacing(1.75),
          bgcolor: "background.paper",
          border: 1,
          borderColor: "divider",
          borderRadius: (t) => t.spacing(1.75),
          boxShadow: "var(--aep-shell-card-shadow)",
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
          outline: "none",
          animation: `${slideIn} 0.22s ease`,
          ...reducedMotion,
          [PHONE]: { inset: (t) => t.spacing(1.25) },
        }}
      >
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            gap: 1.25,
            pl: 2.5,
            pr: 2,
            py: 1.5,
            borderBottom: 1,
            borderColor: "divider",
            [PHONE]: { flexWrap: "wrap", rowGap: 1 },
          }}
        >
          {heading}
          {actions && (
            <Box sx={{ display: "flex", alignItems: "center", gap: 1, [PHONE]: { order: 3, flexBasis: "100%" } }}>
              {actions}
            </Box>
          )}
          <Tooltip title={closeHint}>
            <IconButton size="small" aria-label="Close" onClick={onClose}>
              <X size={18} />
            </IconButton>
          </Tooltip>
        </Box>
        {fill ? (
          <Box sx={{ flex: 1, minHeight: 0 }}>{children}</Box>
        ) : (
          <Box sx={{ flex: 1, minHeight: 0, overflowY: "auto", px: 3.5, pt: 3, pb: 11, [PHONE]: { px: 2, pb: 17.5 } }}>
            {children}
          </Box>
        )}
      </Box>
    </>
  );
}
