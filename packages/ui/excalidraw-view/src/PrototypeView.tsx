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

import { useMemo, useReducer, useRef, useState, useEffect, Suspense, type ReactNode } from "react";
import {
  Box,
  CircularProgress,
  FormControl,
  IconButton,
  InputLabel,
  MenuItem,
  Select,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import { ArrowLeft } from "@wso2/oxygen-ui-icons-react";
import type { PrototypeModel } from "@aep/excalidraw-dsl";
import { ExcalidrawComponent } from "./lazyExcalidraw.js";
import { parseScene, fitContentToViewport } from "./scene.js";
import { prototypeNavReducer } from "./prototypeState.js";
import { resolveFlow, flowEntryScreen, pickerScreens } from "./flowState.js";
import { hotspotToViewport, type ViewportRect } from "./hotspotOverlay.js";

const FLASH_MS = 900;

// A Select menu defaults to opening OVER its control, aligned on the selected
// option — which in a toolbar drops the list on top of the row it belongs to
// and over the panel beside it. Anchor it under the control instead, and cap
// the width so a long "role · journey" line wraps rather than stretching the
// menu across the page.
const MENU_PROPS = {
  anchorOrigin: { vertical: "bottom", horizontal: "left" },
  transformOrigin: { vertical: "top", horizontal: "left" },
  slotProps: { paper: { sx: { maxWidth: 460, mt: 0.5 } } },
} as const;

// Hotspot highlight: the wireframes' flow-accent blue, NOT the brand orange —
// primary CTAs are drawn filled orange, so an orange ring on top of one is
// invisible. The white halo keeps the ring legible over blue-ish elements
// (info badges, links) as well.
const HIGHLIGHT_BORDER = "#1971c2";
const HIGHLIGHT_FILL = "rgba(25, 113, 194, 0.12)";
const HIGHLIGHT_HALO = "0 0 0 2px rgba(255, 255, 255, 0.9)";

/** Excalidraw's own scroll/zoom snapshot — the shape hotspotToViewport needs. */
type CameraState = { scrollX: number; scrollY: number; zoom: { value: number } };

export interface PrototypeViewProps {
  model: PrototypeModel;
  /** Start screen (deep link). Unknown/absent → first screen. */
  initialScreen?: string;
  /** Start flow (deep link). Unknown/absent → first declared flow. */
  initialFlow?: string;
  /** Fires on every screen change — the full-screen route syncs the URL. */
  onScreenChange?: (screen: string) => void;
  /** Fires on every flow change — the full-screen route syncs the URL. */
  onFlowChange?: (flow: string) => void;
  /** Fill the parent's height (else fixed 600px), like ExcalidrawView. */
  fillHeight?: boolean;
  /** Right-aligned toolbar slot (e.g. the console's Canvas | Prototype switch,
   *  so the switch and this view's own chrome read as one toolbar row). */
  trailingSlot?: ReactNode;
}

/* eslint-disable @typescript-eslint/no-explicit-any */
// Expects to be remounted (e.g. via a `key`) when `model` changes: `initialData`
// is captured once at mount, and the screen-swap effect only reacts to
// navigation, not to a new `model` identity.
export function PrototypeView({
  model,
  initialScreen,
  initialFlow,
  onScreenChange,
  onFlowChange,
  fillHeight,
  trailingSlot,
}: PrototypeViewProps) {
  const byName = useMemo(() => new Map(model.screens.map((s) => [s.name, s])), [model]);
  const first = model.screens[0]!.name;
  // Flow selection is view state, not navigation state: it survives clicking
  // across flows (the flow is a starting lens, not a cage), so it lives beside
  // the nav reducer rather than inside it.
  const [flow, setFlow] = useState<string | null>(() => resolveFlow(model, initialFlow));
  // A deep-linked screen wins over the flow's entry screen — a shared link
  // points at a screen, and honouring the flow instead would silently move the
  // reader somewhere else.
  const start = initialScreen && byName.has(initialScreen)
    ? initialScreen
    : (flowEntryScreen(model, flow) ?? first);
  const [nav, dispatch] = useReducer(prototypeNavReducer, { current: start, stack: [] });
  const apiRef = useRef<any>(null);
  const [flash, setFlash] = useState(false);
  const flashTimerRef = useRef<ReturnType<typeof window.setTimeout> | null>(null);
  // Current screen's hotspots, transformed to container-viewport coordinates.
  // Recomputed on scroll/zoom, after a screen swap's fit-to-content settles,
  // and on initial mount — see the effects below.
  const [overlayRects, setOverlayRects] = useState<ViewportRect[]>([]);

  useEffect(() => {
    return () => {
      if (flashTimerRef.current !== null) window.clearTimeout(flashTimerRef.current);
    };
  }, []);

  const screen = byName.get(nav.current) ?? model.screens[0]!;
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const initialData = useMemo(() => parseScene(model.screens.find((s) => s.name === start)!.sceneJson), []);

  const recomputeOverlay = (camera: CameraState) => {
    setOverlayRects(screen.hotspots.map((h) => hotspotToViewport(h, camera)));
  };

  // fitContentToViewport settles inside a requestAnimationFrame; chain one
  // more frame after it so the overlay is measured against the SETTLED
  // camera, not the pre-fit one.
  const recomputeOverlaySoon = () => {
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        const api = apiRef.current;
        if (api) recomputeOverlay(api.getAppState());
      });
    });
  };

  // Screen swap: one mounted canvas, updateScene + refit — ExcalidrawView's
  // streaming pattern. Also notify the consumer (URL sync).
  const mounted = useRef(start);
  useEffect(() => {
    onScreenChange?.(nav.current);
    if (nav.current === mounted.current) return;
    mounted.current = nav.current;
    const api = apiRef.current;
    const next = parseScene(screen.sceneJson);
    setOverlayRects([]); // stale positions belong to the outgoing screen
    if (!api || !next?.elements) return;
    try {
      api.updateScene({ elements: next.elements });
      fitContentToViewport(api, next.elements);
      recomputeOverlaySoon();
    } catch {
      /* api torn down */
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [nav.current]);

  // Dead-area click (canvas background, not a hotspot overlay — those stop
  // propagation) → flash every hotspot on the current screen (Figma-style
  // discoverability).
  const onDeadAreaClick = () => {
    if (screen.hotspots.length === 0) return;
    setFlash(true);
    if (flashTimerRef.current !== null) window.clearTimeout(flashTimerRef.current);
    flashTimerRef.current = window.setTimeout(() => {
      flashTimerRef.current = null;
      setFlash(false);
    }, FLASH_MS);
  };

  const hasFlows = model.flows.length > 0;

  const onFlowSelected = (next: string) => {
    setFlow(next);
    onFlowChange?.(next);
    const entry = flowEntryScreen(model, next);
    if (entry) dispatch({ type: "reset", to: entry });
  };

  return (
    <Box
      sx={{
        flex: fillHeight ? 1 : undefined,
        height: fillHeight ? undefined : "600px",
        minHeight: 0,
        display: "flex",
        flexDirection: "column",
        border: "1px solid",
        borderColor: "divider",
        borderRadius: 1,
        "& .help-icon": { display: "none !important" },
        "& .dropdown-menu-button": { display: "none !important" },
        "& .App-menu_top__left": { display: "none !important" },
      }}
    >
      {/* Toolbar, ONE row: back · User flow · Screen · screen description.
          The flow's own description is deliberately absent here — each option
          in the open flow menu carries it, and repeating it in the row cost
          the width that forced a second tier. No flows → the flow control
          simply isn't rendered and the same row serves as before. */}
      <Box
        sx={{
          px: 1.5,
          // A notched label rides ON a control's top edge, so the row needs a
          // little more headroom above than below.
          pt: 1.25,
          pb: 0.75,
          display: "flex",
          alignItems: "center",
          gap: 1.5,
          borderBottom: 1,
          borderColor: "divider",
        }}
      >
        <IconButton size="small" aria-label="Back" disabled={nav.stack.length === 0} onClick={() => dispatch({ type: "back" })}>
          <ArrowLeft size={16} />
        </IconButton>
        {hasFlows && (
          <FormControl size="small" sx={{ minWidth: 200 }}>
            <InputLabel id="aep-flow-label">User flow</InputLabel>
            <Select
              labelId="aep-flow-label"
              label="User flow"
              value={flow ?? ""}
              onChange={(e) => onFlowSelected(String(e.target.value))}
              // Menu items carry two lines (name + role · journey); the CLOSED
              // control shows only the name, so the control stays one row tall.
              renderValue={(v) => String(v)}
              MenuProps={MENU_PROPS}
            >
              {model.flows.map((f) => {
                const detail = [f.role, f.description].filter(Boolean).join(" · ");
                return (
                  <MenuItem key={f.name} value={f.name}>
                    <Box sx={{ py: 0.25 }}>
                      <Typography variant="body2">{f.name}</Typography>
                      {detail && (
                        <Typography variant="caption" color="text.secondary" display="block" sx={{ whiteSpace: "normal" }}>
                          {detail}
                        </Typography>
                      )}
                    </Box>
                  </MenuItem>
                );
              })}
            </Select>
          </FormControl>
        )}
        <FormControl size="small" sx={{ minWidth: 180 }}>
          <InputLabel id="aep-screen-label">Screen</InputLabel>
          <Select
            labelId="aep-screen-label"
            label="Screen"
            value={nav.current}
            onChange={(e) => dispatch({ type: "navigate", to: String(e.target.value) })}
            MenuProps={MENU_PROPS}
          >
            {pickerScreens(model, flow, nav.current).map((name) => (
              <MenuItem key={name} value={name}>
                {name}
              </MenuItem>
            ))}
          </Select>
        </FormControl>
        {screen.description && (
          // Two pickers now sit left of it, so a long description truncates;
          // the tooltip carries the full text. Harmlessly redundant when the
          // description fits.
          <Tooltip title={screen.description}>
            <Typography variant="body2" color="text.secondary" noWrap sx={{ minWidth: 0 }}>
              {screen.description}
            </Typography>
          </Tooltip>
        )}
        {trailingSlot && <Box sx={{ ml: "auto", flexShrink: 0 }}>{trailingSlot}</Box>}
      </Box>
      <Box sx={{ position: "relative", flex: 1, minHeight: 0 }} onClick={onDeadAreaClick}>
        <Box sx={{ position: "absolute", inset: 0 }}>
          <Suspense
            fallback={
              <Box sx={{ height: "100%", display: "flex", alignItems: "center", justifyContent: "center" }}>
                <CircularProgress size={28} />
              </Box>
            }
          >
            <ExcalidrawComponent
              initialData={initialData as any}
              viewModeEnabled
              onScrollChange={(scrollX: number, scrollY: number, zoom: { value: number }) =>
                recomputeOverlay({ scrollX, scrollY, zoom })
              }
              excalidrawAPI={(api: any) => {
                apiRef.current = api;
                const els = initialData?.elements;
                if (els?.length) fitContentToViewport(api, els);
                recomputeOverlaySoon();
              }}
            />
          </Suspense>
        </Box>
        {screen.hotspots.map((h, i) => {
          const r = overlayRects[i];
          if (!r) return null;
          return (
            <Box
              key={`${h.target}:${i}`}
              role="button"
              aria-label={`Go to ${h.target}`}
              tabIndex={0}
              onClick={(e) => {
                e.stopPropagation();
                dispatch({ type: "navigate", to: h.target });
              }}
              onKeyDown={(e) => {
                if (e.key !== "Enter" && e.key !== " ") return;
                e.preventDefault();
                e.stopPropagation();
                dispatch({ type: "navigate", to: h.target });
              }}
              sx={{
                position: "absolute",
                // Excalidraw's own interactive canvas layer sets z-index: 2
                // (its CSS, not ours); since neither ancestor Box here
                // establishes its own stacking context, that z-index escapes
                // to this box's context and would otherwise paint OVER a
                // later-in-DOM but z-index:auto sibling. Beat it explicitly.
                zIndex: 3,
                left: r.left,
                top: r.top,
                width: r.width,
                height: r.height,
                cursor: "pointer",
                borderRadius: 1,
                border: "2px solid transparent",
                ...(flash
                  ? {
                      borderColor: HIGHLIGHT_BORDER,
                      bgcolor: HIGHLIGHT_FILL,
                      boxShadow: HIGHLIGHT_HALO,
                    }
                  : {
                      "&:hover": {
                        borderColor: HIGHLIGHT_BORDER,
                        bgcolor: HIGHLIGHT_FILL,
                        boxShadow: HIGHLIGHT_HALO,
                      },
                    }),
              }}
            />
          );
        })}
      </Box>
    </Box>
  );
}
