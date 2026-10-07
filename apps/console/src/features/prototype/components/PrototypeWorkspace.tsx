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

import { useState } from "react";
import { Box, Button, Chip, CircularProgress, Skeleton, Tooltip, Typography } from "@wso2/oxygen-ui";
import { AppWindow } from "@wso2/oxygen-ui-icons-react";
import { PHONE } from "../../shell/layout";
import type { ReviewQueue } from "../model/feedback";
import { markReviewed } from "../model/reviewed";
import type { AppPrototype, PrototypeStatus } from "../model/prototypes";
import { usePrototypes } from "../usePrototypes";
import { usePrototypeTurns } from "../usePrototypeTurns";
import { MakePrototypeButton } from "./MakePrototypeButton";
import { PrototypeReview } from "./PrototypeReview";

const STATUS: Record<PrototypeStatus, { label: string; color: "default" | "success" | "error" | "info" }> = {
  none: { label: "No prototype", color: "default" },
  ready: { label: "Ready", color: "success" },
  invalid: { label: "Invalid", color: "error" },
  revising: { label: "Revising…", color: "info" },
};

/** The app icon's tile: a spinner while a turn works on the prototype. */
function AppTile({ busy, size }: { busy: boolean; size: number }) {
  return (
    <Box
      sx={{
        width: size,
        height: size,
        flexShrink: 0,
        borderRadius: 3,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        bgcolor: "action.hover",
        color: "primary.main",
      }}
    >
      {busy ? (
        <CircularProgress size={size * 0.46} aria-label="Working on the prototype" />
      ) : (
        <AppWindow size={size * 0.46} aria-hidden />
      )}
    </Box>
  );
}

/** Why the action waits, under it, while another turn runs. */
function Waiting({ reason }: { reason: string | null }) {
  if (!reason) return null;
  return (
    <Typography variant="caption" color="text.secondary">
      {reason}
    </Typography>
  );
}

interface RowProps {
  prototype: AppPrototype;
  ready: boolean;
  waiting: string | null;
  onReview: () => void;
  onMake: () => void;
}

/**
 * One web application's prototype: its name, status and action. Alone it is
 * the tab's centred hero; beside others, a card in a grid (`compact`).
 */
function Row({ prototype, ready, waiting, onReview, onMake, compact }: RowProps & { compact: boolean }) {
  const busy = prototype.status === "revising";
  // The first make: nothing to name, review or update yet.
  const making = busy && !prototype.exists;
  const status = making ? { ...STATUS.revising, label: "Making prototype…" } : STATUS[prototype.status];
  const name = prototype.files?.manifest.name;
  const align = compact ? "flex-start" : "center";
  return (
    <Box
      component="li"
      sx={{
        display: "flex",
        flexDirection: "column",
        alignItems: align,
        textAlign: compact ? "left" : "center",
        gap: 1,
        ...(compact && { border: 1, borderColor: "divider", borderRadius: 3, p: 2.5, bgcolor: "background.paper" }),
      }}
    >
      <Box sx={{ display: "flex", flexDirection: compact ? "row" : "column", alignItems: "center", gap: compact ? 1.5 : 1.75, minWidth: 0, maxWidth: "100%" }}>
        <AppTile busy={busy} size={compact ? 40 : 56} />
        <Box sx={{ minWidth: 0 }}>
          <Typography component="h3" noWrap={compact} sx={{ fontWeight: 600, fontSize: compact ? "1rem" : "1.375rem" }}>
            {name ?? prototype.component}
          </Typography>
          {name && (
            <Typography variant="body2" color="text.secondary" noWrap={compact} sx={{ fontFamily: "monospace" }}>
              {prototype.component}
            </Typography>
          )}
        </Box>
      </Box>
      <Chip size="small" label={status.label} color={status.color} variant={prototype.status === "none" ? "outlined" : "filled"} />
      {/* Mid-turn, a half-written prototype is not a broken one. */}
      {prototype.problem && !busy && (
        <Tooltip title={prototype.problem}>
          <Typography variant="body2" color="error.main">
            The last prototype couldn't be rendered.
          </Typography>
        </Tooltip>
      )}
      {/* While a turn works on it, the last good version can still be reviewed. */}
      {busy && !prototype.files ? (
        <Typography variant="body2" color="text.secondary">
          {making ? "This usually takes a few minutes." : "The agent is working on it."}
        </Typography>
      ) : (
        <Box sx={{ mt: compact ? "auto" : 2, pt: compact ? 1 : 0, display: "flex", flexDirection: "column", alignItems: align, gap: 0.75 }}>
          {prototype.files ? (
            <Button variant="contained" size={compact ? "small" : "medium"} onClick={onReview}>
              Review
            </Button>
          ) : (
            <>
              <Button variant="contained" size={compact ? "small" : "medium"} disabled={!ready} onClick={onMake}>
                {prototype.status === "invalid" ? "Try again" : "Make prototype"}
              </Button>
              <Waiting reason={waiting} />
            </>
          )}
        </Box>
      )}
    </Box>
  );
}

/**
 * The Prototype tab: each web application the design has, with its
 * prototype's status, Review (the full-screen overlay) and Make or Update.
 * Before any prototype exists it says what one takes, with Make prototype
 * when the design has a web application. The open review is the route's
 * (`?review=<component>`); the requests queued in a review are kept here, so
 * closing and opening it again keeps them.
 */
export function PrototypeWorkspace({
  projectName,
  review,
  onReview,
}: {
  projectName: string;
  review: string | undefined;
  onReview: (component: string | null) => void;
}) {
  const prototypes = usePrototypes(projectName);
  const turns = usePrototypeTurns(projectName);
  const [queues, setQueues] = useState<Record<string, ReviewQueue | null>>({});

  if (!prototypes) {
    return (
      <Box aria-busy sx={{ px: 3.5, pt: 3 }}>
        <Skeleton width={180} />
        <Skeleton variant="rounded" height={72} sx={{ mt: 2 }} />
      </Box>
    );
  }

  const open = review ? prototypes.find((p) => p.component === review) : undefined;
  const none = prototypes.every((p) => p.status === "none");

  return (
    <Box
      sx={{
        height: "100%",
        overflowY: "auto",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
        px: 3.5,
        py: 4,
        [PHONE]: { px: 2 },
      }}
    >
      {prototypes.length === 0 || none ? (
        <Box sx={{ display: "flex", flexDirection: "column", alignItems: "center", textAlign: "center", gap: 1, maxWidth: "52ch" }}>
          <Box
            sx={{
              width: 56,
              height: 56,
              mb: 0.75,
              borderRadius: 3,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              bgcolor: "action.hover",
              color: "text.secondary",
            }}
          >
            <AppWindow size={26} aria-hidden />
          </Box>
          <Typography component="h3" sx={{ fontWeight: 600, fontSize: "1.375rem" }}>
            No prototype yet
          </Typography>
          <Typography variant="body2" color="text.secondary">
            {prototypes.length === 0
              ? "Finish the spec and design a web application first. Its clickable prototype is made here."
              : prototypes.length > 1
                ? "Make clickable prototypes of the designed web applications to try them before anything is built."
                : "Make a clickable prototype of the designed web application to try it before anything is built."}
          </Typography>
          {prototypes.length > 0 && (
            <Box sx={{ mt: 2, display: "flex", flexDirection: "column", alignItems: "center", gap: 0.75 }}>
              <MakePrototypeButton projectName={projectName} variant="contained" />
              <Waiting reason={turns.waiting} />
            </Box>
          )}
        </Box>
      ) : (
        <Box
          component="ul"
          aria-label="Prototypes"
          sx={{
            m: "auto",
            p: 0,
            listStyle: "none",
            ...(prototypes.length > 1 && {
              width: "100%",
              maxWidth: 880,
              display: "grid",
              gridTemplateColumns: "repeat(auto-fit, minmax(260px, 1fr))",
              gap: 2,
            }),
          }}
        >
          {prototypes.map((p) => (
            <Row
              key={p.component}
              prototype={p}
              compact={prototypes.length > 1}
              ready={turns.ready}
              waiting={turns.waiting}
              onReview={() => onReview(p.component)}
              onMake={() => turns.make(p.component)}
            />
          ))}
        </Box>
      )}
      {open && (
        <PrototypeReview
          prototype={open}
          queue={queues[open.component] ?? null}
          onQueue={(queue) => setQueues((q) => ({ ...q, [open.component]: queue }))}
          ready={turns.ready}
          onSend={turns.sendFeedback}
          onSeen={(hash) => markReviewed(projectName, open.component, hash)}
          onClose={() => onReview(null)}
        />
      )}
    </Box>
  );
}
