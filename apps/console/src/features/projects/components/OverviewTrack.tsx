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

import { Box, ButtonBase, Card, Chip, Stack, Tooltip, Typography, alpha } from "@wso2/oxygen-ui";
import { ChevronRight, Lock } from "@wso2/oxygen-ui-icons-react";
import { createLink } from "@tanstack/react-router";
import type { Theme } from "@wso2/oxygen-ui";
import type { components } from "../../../generated/aep-api";
import { useHasPermission } from "../../../auth/permissions";
import { useSession } from "../../../auth/SessionContext";
import { useAgentEngaged } from "../../agent-chat/useAgentEngaged";
import { useConversationLog } from "../../agent-chat/useConversationLog";
import { WorkingPulse } from "../../agent-chat/components/WorkingIndicator";
import { trackView, type LegState, type TrackLeg } from "../lib/track";

type ProjectStatus = components["schemas"]["ProjectStatus"];

// MUI's polymorphic `component={Link}` does not typecheck against the router's
// typed `to`/`params`; createLink is the console's established adapter.
const LegLink = createLink(ButtonBase);

/**
 * The track's corner radius, in PIXELS, read by the Card and by every leg.
 *
 * That is the whole trick behind the lit border: a leg is a square box inside a
 * rounded card, so an outline on it cut straight across the card's curve and
 * read as a rendering bug. Given the card's own radius on its outer corners,
 * the ring curves parallel to the border instead.
 *
 * Pixels, not spacing units, because the two scales are not the same one and
 * mixing them is how this drifted the first time: `borderRadius: 1.5` in `sx`
 * multiplies by `shape.borderRadius` and lands on 18px, while
 * `theme.spacing(1.5)` is 12px. Same literal, two different corners.
 */
const TRACK_RADIUS_PX = 12;

/**
 * The breakpoint at and above which the three legs sit in a ROW; below it they
 * stack into a column.
 *
 * A leg carries a stage name, a version, a sentence and sometimes a call to
 * action. Three of those across a phone — or across the half-window the
 * overview gets whenever the agent chat panel is open — leaves each one about
 * 110px, at which the sentence wraps to four lines and the card becomes a wall
 * of broken text. Stacked, every leg gets the full width and the flow simply
 * runs downward instead of across; the seam and the corner radii follow it (see
 * `SeamBreak` and `Leg`).
 */
const TRACK_ROW_UP = "md";

/**
 * The palette family a leg reads from. `waiting` has none — it is the theme's
 * own muted text, so every accent below falls back to disabled text rather than
 * inventing a palette entry.
 */
const FAMILY: Record<LegState, "primary" | "warning" | "success" | "error" | null> = {
  waiting: null,
  live: "primary",
  hold: "warning",
  done: "success",
  failed: "error",
};

/**
 * The seam's own weight, derived from the text colour rather than taken from
 * `divider`.
 *
 * `divider` is tuned for hairlines and resolves to black at 7% alpha, which on
 * this console's dark ground is invisible — the same measurement that made the
 * builds rail derive its own colour (`StageRow.tsx`). An arrowhead nobody can
 * see is the whole connection between two legs, gone.
 *
 * Read from the CSS VARIABLE, not from `theme.palette`. Oxygen is a
 * CSS-variable theme, so a colour computed inside an `sx` callback bakes the
 * DEFAULT (light) palette at render time and never follows the active scheme:
 * `theme.palette.text.primary` hands back `#40404B` while the page is actually
 * painting `#efefef`. The seam was therefore drawn in dark grey on a near-black
 * ground — present in the DOM, invisible on screen. The `Channel` variables
 * hold the same colour as space-separated RGB so an alpha can be applied to it.
 */
const textChannel = "var(--oxygen-palette-text-primaryChannel, 128 128 128)";
const seamInk = (a: number) => `rgba(${textChannel} / ${a})`;

/** The lit rail down a leg's leading edge, and the wash behind it. */
function litStyles(theme: Theme, state: LegState) {
  const family = FAMILY[state];
  // Only the two UNSETTLED states are lit. A settled leg is quiet: if every
  // visit glows, the glow stops meaning anything.
  if (family === null || state === "done") return {};
  const main = theme.palette[family].main;
  // A wash plus a 1px ring in the leg's own tone.
  //
  // No rail on the leading edge: that read as a SELECTION marker, the
  // convention every list in this console uses for "the row you picked", when
  // what it means is "this stage is unsettled".
  //
  // The ring works only because the leg carries the card's corner radii (see
  // TRACK_RADIUS_PX). An inset shadow follows its own element's radius, so on a
  // square leg it cut straight across the card's curve and read as a bug.
  return {
    background: `radial-gradient(135% 130% at 0% 0%, ${alpha(main, 0.14)} 0%, transparent 74%)`,
    boxShadow: `inset 0 0 0 1px ${alpha(main, 0.45)}`,
  };
}

/**
 * The seam: the rule breaks, and a chevron stands in the break.
 *
 * The line runs down, stops, the chevron carries it across, the line resumes.
 * The KNOCKOUT is what does the work — without it the glyph is painted on top
 * of an unbroken rule and reads as an ornament stuck to it, which is what the
 * arrowhead this replaces did. That arrowhead was a small triangle sitting
 * mid-way along a full-height divider: the divider read as the dominant thing
 * and the triangle as decoration on it, so nothing looked connected.
 *
 * Order and direction both come from here, now that the step numeral is gone;
 * no thread is run through the whole bar, which an earlier pass tried and which
 * read as a progress meter the legs were already carrying.
 */
function SeamBreak() {
  return (
    <Box
      aria-hidden
      sx={(theme) => ({
        position: "absolute",
        // Straddles whichever edge the next leg is on: the end edge while the
        // legs sit in a row, the bottom edge once they stack.
        //
        // Stacked, it is centred by AUTO MARGINS rather than by an inset plus a
        // half-width translate. The translate would have been the shorter
        // spelling and it is wrong in RTL: `insetInlineEnd` flips with the
        // writing mode and `translateX` does not, so the two disagree and the
        // chevron lands a full width off the seam. Auto margins have no
        // direction to disagree about — and they leave ONE transform for both
        // layouts instead of two.
        insetInlineStart: { xs: 0, [TRACK_ROW_UP]: "auto" },
        insetInlineEnd: { xs: 0, [TRACK_ROW_UP]: theme.spacing(-1.125) },
        marginInline: { xs: "auto", [TRACK_ROW_UP]: 0 },
        insetBlockStart: { xs: "100%", [TRACK_ROW_UP]: "50%" },
        transform: "translateY(-50%)",
        zIndex: 2,
        width: theme.spacing(2.25),
        height: theme.spacing(2.25),
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        borderRadius: "50%",
        bgcolor: "background.paper",
        color: seamInk(0.55),
      })}
    >
      {/* The glyph turns with the flow. A chevron still pointing right under a
          leg that continues DOWNWARD points at the page margin. */}
      <Box
        sx={{
          display: "flex",
          transform: { xs: "rotate(90deg)", [TRACK_ROW_UP]: "none" },
        }}
      >
        <ChevronRight size={14} strokeWidth={2.25} />
      </Box>
    </Box>
  );
}

function Leg({
  leg,
  projectName,
  first,
  last,
  locked,
  lockReason,
}: {
  leg: TrackLeg;
  projectName: string;
  first: boolean;
  last: boolean;
  /** True when the caller holds neither permission this leg's page needs. */
  locked: boolean;
  /** Tooltip copy while `locked` — unused otherwise. */
  lockReason: string;
}) {
  const family = FAMILY[leg.state];
  const muted = leg.state === "waiting" || locked;
  return (
    <Box sx={{ flex: 1, minWidth: 0, position: "relative", display: "flex" }}>
      {/* Tooltip needs a ref-forwarding child, and `disabled` on the link
          itself would otherwise swallow the hover that triggers it — the
          same span-wrapper convention EnvironmentCards/ConnectionsCard use
          for every other disabled-with-a-reason control in this console.
          `display: flex` on the span, not the default inline, so it still
          participates as the flex item the parent Box expects. */}
      <Tooltip title={locked ? lockReason : ""}>
        <span style={{ display: "flex", flex: 1, minWidth: 0 }}>
          <LegLink
            to={leg.to}
            params={{ projectName }}
            disabled={locked}
            // Colour is never the only signal, and it is not the accessible one at
            // all: the state is in the line, and the line is in the name. The call
            // to action joins it when there is one — a link announced as "Spec: the
            // agent has questions for you" gives no clue that following it is how
            // you answer them.
            aria-label={
              locked
                ? `${leg.name}: ${lockReason}`
                : leg.cta
                  ? `${leg.name}: ${leg.line}. ${leg.cta}`
                  : `${leg.name}: ${leg.line}`
            }
            sx={(theme) => ({
              flex: 1,
              minWidth: 0,
              position: "relative",
              display: "flex",
              flexDirection: "column",
              alignItems: "stretch",
              justifyContent: "flex-start",
              textAlign: "start",
              gap: 0.75,
              px: 2.25,
              py: 1.75,
              // The card's corners on the outer edges, square where legs meet —
              // and "outer" moves when the legs stack. The two corners on the
              // flow's own axis (top-start, bottom-end) belong to the first and
              // last leg either way; the other two swap owners.
              borderStartStartRadius: first ? TRACK_RADIUS_PX : 0,
              borderEndEndRadius: last ? TRACK_RADIUS_PX : 0,
              borderStartEndRadius: {
                xs: first ? TRACK_RADIUS_PX : 0,
                [TRACK_ROW_UP]: last ? TRACK_RADIUS_PX : 0,
              },
              borderEndStartRadius: {
                xs: last ? TRACK_RADIUS_PX : 0,
                [TRACK_ROW_UP]: first ? TRACK_RADIUS_PX : 0,
              },
              // Same reason as the seam: a separator at `divider` weight does not
              // survive the dark theme, and the legs read as one undivided block.
              // It runs along whichever edge the next leg is on.
              ...(!last && {
                borderStyle: "solid",
                borderColor: seamInk(0.12),
                borderWidth: 0,
                borderInlineEndWidth: { xs: 0, [TRACK_ROW_UP]: 1 },
                borderBlockEndWidth: { xs: 1, [TRACK_ROW_UP]: 0 },
              }),
              transition: theme.transitions.create("background-color", {
                duration: theme.transitions.duration.shortest,
              }),
              ...(locked
                ? { opacity: 0.55, cursor: "not-allowed" }
                : { "&:hover": { backgroundColor: "action.hover" }, ...litStyles(theme, leg.state) }),
            })}
          >
            <Stack direction="row" spacing={1} sx={{ alignItems: "center", width: "100%" }}>
              {/* The heaviest title on the page, deliberately.
                  `subtitle2` renders at weight 400 in this theme, which put the
                  stage name BELOW both a component row's name (14.5px/500) and the
                  section heading above it (14px/500) — the band that answers "what
                  now" was reading as the quietest thing on screen. */}
              <Typography
                sx={{
                  fontSize: "0.9375rem",
                  fontWeight: 600,
                  letterSpacing: "-0.01em",
                  color: muted ? "text.secondary" : "text.primary",
                }}
              >
                {leg.name}
              </Typography>
              {/* Shown for `live` only, and it is `WorkingPulse` — the same dot the
                  chat footer and the spec rail use, NOT a ring of this file's own.
                  That component was extracted (#575) so "an agent is working" looks
                  identical everywhere it appears, and this leg had grown exactly the
                  second animation it exists to prevent: a hollow ring breathing an
                  expanding shadow at 2.2s next to a solid dot pulsing at 1.2s two
                  panels away. One fact, one animation.

                  A leg waiting on the USER (`hold`) still does not pulse — a page
                  that animates while it waits for you to type is lying about who is
                  busy. Locked never pulses either, for the same reason: nobody is
                  waiting on a leg the reader cannot open. */}
              {leg.state === "live" && !locked && <WorkingPulse />}
              <Box sx={{ flexGrow: 1 }} />
              {/* Lock takes the trailing slot the version chip otherwise holds —
                  a caller who cannot open the leg has no version worth reading
                  either, so the two never compete for the spot. */}
              {locked ? (
                <Lock size={14} aria-hidden style={{ flexShrink: 0, opacity: 0.7 }} />
              ) : (
                leg.version && (
                  /* Version only when one exists — no em-dash placeholder. Blank says
                     "not yet" better than a dash pretending to be a value.

                     A LABEL, NOT A BADGE. This was a filled chip in the leg's own
                     state colour, and with three legs carrying one it put three solid
                     lozenges across the widest band on the page — the version, which
                     is the same "v1" three times over in the common case, ended up
                     shouting louder than the stage names it sits beside. The state is
                     already told by the lit ring, the pulse and the line; the version
                     only has to be READABLE. So: still a Chip — the console's tag
                     everywhere, and what the component rows beside it use — but
                     outlined, unstyled by state, monospace and in secondary ink. */
                  <Chip
                    size="small"
                    variant="outlined"
                    label={leg.version}
                    sx={{
                      height: 20,
                      flexShrink: 0,
                      fontFamily: "monospace",
                      fontSize: "0.6875rem",
                      color: "text.secondary",
                      borderColor: seamInk(0.18),
                    }}
                  />
                )
              )}
            </Stack>
            <Typography
              variant="body2"
              sx={{
                color: !locked && leg.state === "failed" ? "error.main" : "text.secondary",
              }}
            >
              {locked ? lockReason : leg.line}
            </Typography>
            {/* Its own line, under the description rather than beside it.
                Alongside, it only looked right while the description fitted on one
                line: a leg is a third of the track, so at real widths the line
                wraps to three and the action was left hanging in the whitespace
                beside the first of them.

                Not a button. The leg is already the link, and a button inside a
                link is a broken target — this only tells the reader that the thing
                they are reading is the thing to click. Same wording as the chat
                panel's questions pointer, which navigates here too. */}
            {!locked && leg.cta && (
              <Typography
                variant="caption"
                sx={{
                  fontWeight: 600,
                  color: family ? `${family}.main` : "text.primary",
                }}
              >
                {leg.cta} →
              </Typography>
            )}
          </LegLink>
        </span>
      </Tooltip>
      {!last && <SeamBreak />}
    </Box>
  );
}

/**
 * The overview's centrepiece: one version's journey through spec, build and
 * deploy, as three links.
 *
 * The track NAVIGATES and SUMMARISES; it never sends. Every way of starting
 * work lives on the page that owns it (#562), and a button inside a link is a
 * broken target anyway — so the whole leg is the affordance.
 */
/**
 * Which permission unlocks each leg, and what the lock says while it's held
 * short. Build and Deploy share one check (exact-match ae:build-view) —
 * there is no separate deployment permission in this console, deployments
 * are a build's own consequence — so clicking through to either page from a
 * locked leg would only land on that page's own PermissionRestrictedPage
 * anyway; the leg lock exists so that dead end is never reached in the
 * first place.
 */
const SPEC_LOCK_REASON = "You don't have permission to view the spec.";
const BUILD_LOCK_REASON = "You don't have permission to view builds.";
const DEPLOY_LOCK_REASON = "You don't have permission to view deployments.";

function legLock(
  name: string,
  hasSpecAccess: boolean,
  hasBuildAccess: boolean,
): { locked: boolean; reason: string } {
  if (name === "Spec") return { locked: !hasSpecAccess, reason: SPEC_LOCK_REASON };
  if (name === "Build") return { locked: !hasBuildAccess, reason: BUILD_LOCK_REASON };
  if (name === "Deploy") return { locked: !hasBuildAccess, reason: DEPLOY_LOCK_REASON };
  return { locked: false, reason: "" };
}

export function OverviewTrack({
  projectName,
  status,
}: {
  projectName: string;
  status: ProjectStatus;
}) {
  const org = useSession().orgHandle ?? "default";
  // Exact-match ae:design-view: this read-only rehydrate (below) is a passive
  // status check, not the interactive chat panel (which needs the stronger
  // ae:design — see AppLayout's own hasDesignAccess) — same read/write split
  // as every other permission pair in this console.
  const hasDesignView = useHasPermission("ae:design-view");
  // "The agent is waiting on you" has no server-side source: `spec.agent` folds
  // a completed turn to "", and a turn that ends ON a question is exactly that.
  // The local chat log is the only thing that knows — and this must make sure
  // that log EXISTS (#606), or a teammate opening the overview in a fresh
  // browser reads "nothing has started" over someone else's open question.
  // Passing `undefined` in place of projectName when the caller lacks
  // ae:design-view is what actually withholds the read: both hooks treat an
  // undefined project as "nothing to fetch/subscribe to" (their own
  // `enabled`/chatKey-null guards), the same idiom useProjectsList's own
  // `enabled` param uses elsewhere in this console.
  const conversationProject = hasDesignView ? projectName : undefined;
  useConversationLog(org, conversationProject);
  const engaged = useAgentEngaged(org, conversationProject);
  const { legs, summary } = trackView(status, engaged);
  // The Spec leg's lock reuses hasDesignView above rather than a second call:
  // NOT OR'd with ae:design, because SpecView's own page gate is exact-match
  // ae:design-view alone, so a caller holding only ae:design would see this
  // leg unlocked and land on SpecViewRestricted anyway — the leg lock exists
  // so that dead end is never reached (OverviewArchitecture.tsx carries the
  // same reasoning for the same underlying backend gate, ListFiles/ReadFile).
  const hasSpecAccess = hasDesignView;
  const hasBuildAccess = useHasPermission("ae:build-view");

  return (
    <Box>
      <Card
        variant="outlined"
        sx={{
          display: "flex",
          flexDirection: { xs: "column", [TRACK_ROW_UP]: "row" },
          overflow: "hidden",
          borderRadius: `${TRACK_RADIUS_PX}px`,
        }}
      >
        {legs.map((leg, i) => {
          const { locked, reason } = legLock(leg.name, hasSpecAccess, hasBuildAccess);
          return (
            <Leg
              key={leg.name}
              leg={leg}
              projectName={projectName}
              first={i === 0}
              last={i === legs.length - 1}
              locked={locked}
              lockReason={reason}
            />
          );
        })}
      </Card>
      {/* Rendered only when there is one. Reserving its height cost 28px of
          empty page on every quiet visit — which is most visits — to smooth a
          shift that happens exactly when a build starts or finishes, moments
          the reader is watching for anyway. */}
      {summary && (
        <Box sx={{ mt: 1.25 }}>
          <Stack direction="row" spacing={1.25} sx={{ alignItems: "flex-start" }}>
            <Box
              aria-hidden
              sx={{
                width: 7,
                height: 7,
                borderRadius: "50%",
                flexShrink: 0,
                mt: 0.75,
                bgcolor: `${summary.tone}.main`,
              }}
            />
            <Typography variant="body2" color="text.secondary">
              {summary.text}
            </Typography>
          </Stack>
        </Box>
      )}
    </Box>
  );
}
