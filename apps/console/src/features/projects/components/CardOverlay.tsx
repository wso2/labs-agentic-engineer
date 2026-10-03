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

import { useCallback, useEffect, useId, useRef, type ReactNode } from "react";
import { useNavigate, useParams } from "@tanstack/react-router";
import { Box, IconButton, Tooltip, Typography, keyframes } from "@wso2/oxygen-ui";
import { X } from "@wso2/oxygen-ui-icons-react";
import { PHONE } from "../../shell/layout";
import { cardTitle, type ProjectCard } from "../../shell/scope";
import { WorkspaceTabs } from "./WorkspaceTabs";

const STEP: Record<ProjectCard, number> = { spec: 1, design: 2, prototype: 2, builds: 3 };

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
 * A card drawn over the project overview: the overview stays rendered under
 * a scrim, and closing (X, Escape, or a click on the scrim) goes back to it.
 * Each card is a route of its own, so this is what the card routes render.
 *
 * Spec, Design and Prototype are one workspace, so their header is the
 * Spec · Design · Prototype tabs rather than a title. A card's own actions
 * (the design card's Address comments) sit in the header beside the close
 * button, and wrap under the tabs at phone width. A `fill` body is laid out by its children (the spec
 * card's rail and document scroll on their own); otherwise the body is one
 * padded scroll.
 */
export function CardOverlay({
  card,
  fill = false,
  actions,
  children,
}: {
  card: ProjectCard;
  fill?: boolean;
  actions?: ReactNode;
  children: ReactNode;
}) {
  const { projectName } = useParams({ from: "/projects/$projectName" });
  const navigate = useNavigate();
  const titleId = useId();
  const cardRef = useRef<HTMLDivElement>(null);

  const close = useCallback(
    () => void navigate({ to: "/projects/$projectName", params: { projectName } }),
    [navigate, projectName],
  );

  useEffect(() => {
    // Menus and dialogs above the card stop their own Escape from reaching here.
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !e.defaultPrevented) close();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [close]);

  // Focus moves into the card as it opens, so keyboard and screen reader land
  // on what is now in front.
  useEffect(() => {
    cardRef.current?.focus();
  }, [card]);

  return (
    <>
      <Box
        aria-hidden
        onClick={close}
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
        aria-labelledby={card === "builds" ? titleId : undefined}
        aria-label={card === "builds" ? undefined : cardTitle(card)}
        tabIndex={-1}
        sx={{
          // Framed on all sides, the overview showing round it under the scrim.
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
          {card === "builds" ? (
            <>
              <Typography variant="caption" color="text.secondary" sx={{ fontFamily: "monospace" }}>
                {STEP[card]} / 4
              </Typography>
              <Typography id={titleId} component="h2" sx={{ fontSize: "1rem", fontWeight: 600, flex: 1 }}>
                {cardTitle(card)}
              </Typography>
            </>
          ) : (
            <WorkspaceTabs active={card} />
          )}
          {actions && (
            <Box sx={{ display: "flex", alignItems: "center", gap: 1, [PHONE]: { order: 3, flexBasis: "100%" } }}>
              {actions}
            </Box>
          )}
          <Tooltip title="Back to project overview">
            <IconButton size="small" aria-label="Close" onClick={close}>
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
