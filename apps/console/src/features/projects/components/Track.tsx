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
import { createLink } from "@tanstack/react-router";
import {
  Box,
  Button,
  ButtonBase,
  Card,
  Skeleton,
  Typography,
  keyframes,
} from "@wso2/oxygen-ui";
import { Check } from "@wso2/oxygen-ui-icons-react";
import { useBuildAction } from "../../builds/buildPicker";
import { PHONE } from "../../shell/layout";
import type { LegState } from "../model/track";
import { trackLegs, type TrackLegView } from "../trackLegs";
import { useProjectTrack } from "../useProjectTrack";

const LegLink = createLink(ButtonBase);

/** The palette colour a leg's number and lamp take; null is the muted default. */
const TONE: Record<LegState, "success" | "primary" | "warning" | null> = {
  done: "success",
  live: "primary",
  waiting: "warning",
  notyet: null,
};

const pulse = keyframes`
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
`;

function Lamp({ state }: { state: LegState }) {
  const tone = TONE[state];
  return (
    <Box
      aria-hidden
      sx={{
        width: 7,
        height: 7,
        borderRadius: "50%",
        flexShrink: 0,
        bgcolor: tone ? `${tone}.main` : "text.disabled",
        ...(state === "live" && {
          animation: `${pulse} 1.4s ease-in-out infinite`,
          "@media (prefers-reduced-motion: reduce)": { animation: "none" },
        }),
      }}
    />
  );
}

function StepNumber({ leg }: { leg: TrackLegView }) {
  const tone = TONE[leg.state];
  const done = leg.state === "done";
  return (
    <Box
      aria-hidden
      sx={{
        width: 18,
        height: 18,
        mt: 0.25,
        flexShrink: 0,
        borderRadius: "50%",
        border: 1,
        display: "grid",
        placeItems: "center",
        fontFamily: "monospace",
        fontSize: "0.75rem",
        borderColor: tone ? `${tone}.main` : "divider",
        color: done ? "success.contrastText" : tone ? `${tone}.main` : "text.secondary",
        bgcolor: done ? "success.main" : "transparent",
      }}
    >
      {done ? <Check size={12} strokeWidth={3} /> : leg.step}
    </Box>
  );
}

const legSx = {
  position: "relative",
  display: "flex",
  alignItems: "flex-start",
  justifyContent: "flex-start",
  textAlign: "start",
  gap: 1.5,
  pl: 2.5,
  pr: 2.25,
  py: 2,
} as const;

function LegBody({ leg }: { leg: TrackLegView }) {
  const tone = TONE[leg.state];
  const muted = leg.state === "notyet";
  return (
    <>
      <StepNumber leg={leg} />
      <Box sx={{ minWidth: 0 }}>
        <Typography
          sx={{
            fontSize: "0.9375rem",
            fontWeight: muted ? 500 : 600,
            color: muted ? "text.secondary" : "text.primary",
          }}
        >
          {leg.title}
        </Typography>
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            gap: 0.75,
            mt: 0.25,
            fontSize: "0.8125rem",
            color: tone && tone !== "success" ? `${tone}.main` : "text.secondary",
          }}
        >
          <Lamp state={leg.state} />
          {leg.summary}
        </Box>
      </Box>
    </>
  );
}

const hoverSx = { ...legSx, "&:hover": { bgcolor: "action.hover" } } as const;

function Leg({ leg, projectName, onOpen }: { leg: TrackLegView; projectName: string; onOpen: (() => void) | null }) {
  // The lamp's colour is never the only signal: the state is in the name.
  const label = leg.accessibleName;
  if (onOpen) {
    return (
      <ButtonBase aria-label={label} onClick={onOpen} sx={hoverSx}>
        <LegBody leg={leg} />
      </ButtonBase>
    );
  }
  const target = leg.opens;
  if (target.kind === "build") {
    return (
      <LegLink
        to="/projects/$projectName/builds/$version"
        params={{ projectName, version: target.version }}
        aria-label={label}
        sx={hoverSx}
      >
        <LegBody leg={leg} />
      </LegLink>
    );
  }
  const to = {
    spec: "/projects/$projectName/spec",
    design: "/projects/$projectName/design",
    prototype: "/projects/$projectName/prototype",
    builds: "/projects/$projectName/builds",
    deploy: "/projects/$projectName/deploy",
  } as const;
  return (
    <LegLink to={to[target.kind]} params={{ projectName }} aria-label={label} sx={hoverSx}>
      <LegBody leg={leg} />
    </LegLink>
  );
}

function Frame({ children }: { children: ReactNode }) {
  return (
    <Card
      variant="outlined"
      component="nav"
      aria-label="Project track"
      sx={{
        display: "grid",
        gridTemplateColumns: "repeat(4, 1fr)",
        borderRadius: 2.5,
        overflow: "hidden",
        mb: 3.5,
        [PHONE]: { gridTemplateColumns: "1fr" },
        // Legs after the first: a rule on the leading edge with a chevron through
        // it, pointing on to the next step. Stacked at phone width, a plain rule.
        // `&&` doubles the specificity to outrank ButtonBase's `border: 0`.
        "&& > * + *": {
          borderLeft: 1,
          borderColor: "divider",
          "&::after": {
            content: '""',
            position: "absolute",
            left: -5,
            top: "50%",
            width: 7,
            height: 7,
            transform: "translateY(-50%) rotate(45deg)",
            borderTop: 1.5,
            borderRight: 1.5,
            borderColor: "divider",
            bgcolor: "background.paper",
          },
          [PHONE]: {
            borderLeft: 0,
            borderTop: 1,
            borderColor: "divider",
            "&::after": { display: "none" },
          },
        },
      }}
    >
      {children}
    </Card>
  );
}

/**
 * Spec · Design · Build · Deploy: where the project is, each leg opening its
 * card (Build, the latest build's; Deploy, its Page). While a build is offered
 * (something designed, none running) the Build leg opens the build picker instead.
 */
export function Track({ projectName }: { projectName: string }) {
  const { track, latestBuild, error, retry } = useProjectTrack(projectName);
  const build = useBuildAction(projectName);
  if (error) {
    return (
      <Card
        variant="outlined"
        role="alert"
        sx={{ borderRadius: 2.5, mb: 3.5, p: 2, display: "flex", alignItems: "center", gap: 2 }}
      >
        <Typography variant="body2" color="text.secondary" sx={{ flex: 1 }}>
          {error}
        </Typography>
        <Button size="small" variant="outlined" onClick={retry}>
          Try again
        </Button>
      </Card>
    );
  }
  if (!track) {
    return (
      <Frame>
        {[1, 2, 3, 4].map((n) => (
          <Box key={n} sx={legSx}>
            <Skeleton variant="circular" width={18} height={18} />
            <Box sx={{ flex: 1 }}>
              <Skeleton width="50%" />
              <Skeleton width="80%" />
            </Box>
          </Box>
        ))}
      </Frame>
    );
  }
  return (
    <Frame>
      {trackLegs(track, latestBuild).map((leg) => (
        <Leg key={leg.key} leg={leg} projectName={projectName} onOpen={leg.key === "build" && build.label ? build.open : null} />
      ))}
    </Frame>
  );
}
