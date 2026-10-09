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

import { useId, useState, type ReactNode } from "react";
import { Alert, Box, Button, ButtonBase, CircularProgress, IconButton, Paper, Tooltip, Typography } from "@wso2/oxygen-ui";
import { ChevronDown, ChevronUp, MessageSquarePlus, Send, Trash2 } from "@wso2/oxygen-ui-icons-react";
import type { PrototypeManifest } from "@wso2/prototype-kit/host";
import { MAX_FEEDBACK_REQUESTS, targetLabel, type FeedbackRequest } from "@wso2/prototype-kit/feedback";
import { visuallyHidden } from "../../../components/visuallyHidden";
import { commentCount, queueFull } from "../model/feedback";
import { Key } from "./Key";
import { DockGroup } from "./ReviewDock";

export interface CommentQueueProps {
  manifest: PrototypeManifest;
  requests: readonly FeedbackRequest[];
  /** Each visited screen's element labels, by key, which the list names a comment's elements by. */
  labels: ScreenLabels;
  /** The comments (0-based) written on an earlier revision than the one showing. */
  earlier: readonly number[];
  /** The comments out with the agent (0 when none are): they keep their places, so fewer can be queued meanwhile. */
  held: number;
  /** Why the last Send did not go; the queue is kept. */
  refused: string | null;
  sending: boolean;
  /** The agent is revising the prototype, working on this many sent comments; null when it is not. Send waits meanwhile. */
  revising: number | null;
  /** Why the last revision did not land (its comments are back in the queue); Retry sends the queue again. */
  failed: string | null;
  /** The comments (0-based) whose elements the revision showing no longer draws on this screen. */
  orphans: readonly number[];
  onSend: () => void;
  /** Keep the `index`th comment as a comment on its whole screen. */
  onKeepOnScreen: (index: number) => void;
  /** Comment on the whole screen showing, the bubble at the dock: the keyboard's way to what a click on empty space does. */
  onCommentOnScreen: () => void;
  /** Go to where the `index`th comment was made and open it. */
  onOpen: (index: number) => void;
  onRemove: (index: number) => void;
}

/** Element labels by key, per screen id. */
type ScreenLabels = Readonly<Record<string, Readonly<Record<string, string>>>>;

function nameOf(list: readonly { id: string; name: string }[], id: string): string {
  return list.find((x) => x.id === id)?.name ?? id;
}

/** Where a comment was made, as the reviewer reads it: screen · role · state · its elements' labels (or the whole screen). */
function placeOf(manifest: PrototypeManifest, labels: ScreenLabels, r: FeedbackRequest): string {
  const on = targetLabel(r, labels[r.screenId] ?? {});
  return [nameOf(manifest.screens, r.screenId), nameOf(manifest.roles, r.roleId), nameOf(manifest.states, r.stateId), on].join(" · ");
}

/** A callout above the dock. */
function Callout({ severity, role, action, children }: { severity: "warning" | "error" | "info"; role: "note" | "alert"; action?: ReactNode; children: ReactNode }) {
  return (
    <Alert severity={severity} role={role} variant="outlined" action={action} sx={{ bgcolor: "background.paper", boxShadow: "var(--aep-shell-card-shadow)" }}>
      {children}
    </Alert>
  );
}

/**
 * Every queued comment, across screens, roles and states; an entry goes where
 * the comment was made and opens it. Below them, "Comment on this screen".
 */
function QueueList({
  id,
  manifest,
  requests,
  labels,
  earlier,
  orphans,
  full,
  onOpen,
  onKeepOnScreen,
  onRemove,
  onCommentOnScreen,
}: Pick<CommentQueueProps, "manifest" | "requests" | "labels" | "earlier" | "orphans" | "onKeepOnScreen" | "onRemove" | "onOpen" | "onCommentOnScreen"> & {
  id: string;
  full: boolean;
}) {
  return (
    <Paper id={id} sx={{ borderRadius: 3, border: 1, borderColor: "divider", boxShadow: "var(--aep-shell-card-shadow)", overflow: "hidden" }}>
      <Box
        component="ol"
        aria-label="Queued comments"
        sx={{ m: 0, p: 1, listStyle: "none", maxHeight: "40vh", overflowY: "auto", display: "flex", flexDirection: "column", gap: 0.5, whiteSpace: "normal" }}
      >
        {requests.length === 0 && (
          <Typography component="li" variant="body2" color="text.secondary" sx={{ p: 1 }}>
            No comments yet. Press <Key>C</Key> or choose Comment, then click anything on the screen: an element, or empty space for the whole screen.
          </Typography>
        )}
        {requests.map((r, i) => (
          <Box component="li" key={i} sx={{ display: "flex", alignItems: "flex-start", gap: 0.5 }}>
            <ButtonBase
              onClick={() => onOpen(i)}
              sx={{ flex: 1, minWidth: 0, display: "flex", gap: 1, alignItems: "flex-start", justifyContent: "flex-start", textAlign: "left", borderRadius: 1.5, p: 1, "&:hover": { bgcolor: "action.hover" } }}
            >
              <Box
                aria-hidden
                sx={{
                  flexShrink: 0,
                  width: 22,
                  height: 22,
                  borderRadius: "50%",
                  bgcolor: "primary.main",
                  color: "primary.contrastText",
                  fontSize: "0.75rem",
                  fontWeight: 700,
                  display: "grid",
                  placeItems: "center",
                }}
              >
                {i + 1}
              </Box>
              <Box sx={{ minWidth: 0 }}>
                <Typography variant="body2" sx={{ overflowWrap: "anywhere", display: "-webkit-box", WebkitLineClamp: 2, WebkitBoxOrient: "vertical", overflow: "hidden" }}>
                  {r.text}
                </Typography>
                <Typography variant="caption" color="text.secondary" sx={{ overflowWrap: "anywhere" }}>
                  {placeOf(manifest, labels, r)}
                </Typography>
                {earlier.includes(i) && (
                  <Typography variant="caption" color="text.secondary" sx={{ display: "block" }}>
                    Written on an earlier version
                  </Typography>
                )}
                {orphans.includes(i) && (
                  <Typography variant="caption" color="warning.main" sx={{ display: "block" }}>
                    Element no longer on this screen
                  </Typography>
                )}
              </Box>
            </ButtonBase>
            {orphans.includes(i) && (
              <Button size="small" onClick={() => onKeepOnScreen(i)} sx={{ textTransform: "none", flexShrink: 0, mt: 0.5 }}>
                Keep as screen comment
              </Button>
            )}
            <Tooltip title="Remove">
              <IconButton size="small" aria-label={`Remove comment ${i + 1}`} onClick={() => onRemove(i)} sx={{ mt: 0.5 }}>
                <Trash2 size={16} />
              </IconButton>
            </Tooltip>
          </Box>
        ))}
      </Box>
      <Box sx={{ borderTop: 1, borderColor: "divider", p: 0.5 }}>
        <Button
          fullWidth
          color="inherit"
          startIcon={<MessageSquarePlus size={16} />}
          onClick={onCommentOnScreen}
          disabled={full}
          sx={{ justifyContent: "flex-start", textTransform: "none" }}
        >
          Comment on this screen
        </Button>
      </Box>
    </Paper>
  );
}

/**
 * The dock's comments group (#885), in place of a side panel so the prototype
 * has the full width: the comment count, whose list of every queued comment
 * opens upward over the prototype (with "Comment on this screen", the
 * keyboard's whole-screen comment, its bubble anchored at the dock; the
 * pointer's is a click on empty space), the revising status and Send to agent.
 * What the send has to say (a failed revision with Retry, a refused send, a
 * full queue, comments written on an earlier version or whose element went)
 * shows as callouts above the dock.
 */
export function CommentQueue({
  manifest,
  requests,
  labels,
  earlier,
  held,
  refused,
  sending,
  revising,
  failed,
  orphans,
  onSend,
  onKeepOnScreen,
  onCommentOnScreen,
  onOpen,
  onRemove,
}: CommentQueueProps) {
  const [expanded, setExpanded] = useState(false);
  const full = queueFull(requests.length, held);
  const blocked = requests.length === 0 || requests.length > MAX_FEEDBACK_REQUESTS || sending || revising !== null;
  const listId = useId();
  return (
    <DockGroup label="Comments">
      <Box
        sx={{
          position: "absolute",
          left: "50%",
          bottom: "calc(100% + 8px)",
          transform: "translateX(-50%)",
          width: "min(560px, calc(100vw - 32px))",
          display: "flex",
          flexDirection: "column",
          gap: 1,
          whiteSpace: "normal",
          "&:empty": { display: "none" },
        }}
      >
        {earlier.length > 0 && <Callout severity="warning" role="note">{`${commentCount(earlier.length)} ${earlier.length === 1 ? "was" : "were"} written on an earlier version of the prototype.`}</Callout>}
        {orphans.length > 0 && <Callout severity="warning" role="note">{`${commentCount(orphans.length)} ${orphans.length === 1 ? "points" : "point"} at an element no longer on this screen.`}</Callout>}
        {failed && (
          <Callout
            severity="error"
            role="alert"
            action={
              <Button color="inherit" size="small" onClick={onSend} disabled={blocked}>
                Retry
              </Button>
            }
          >
            {failed}
          </Callout>
        )}
        {full && (
          <Callout severity="info" role="note">
            {held > 0
              ? `The queue is full (${MAX_FEEDBACK_REQUESTS} comments, counting the ${commentCount(held)} the agent is working on): add more once it is done, or remove one.`
              : `The queue is full (${MAX_FEEDBACK_REQUESTS} comments): send it or remove one to add another.`}
          </Callout>
        )}
        {refused && (
          <Callout severity="warning" role="alert">
            {refused}
          </Callout>
        )}
        {expanded && (
          <QueueList
            id={listId}
            manifest={manifest}
            requests={requests}
            labels={labels}
            earlier={earlier}
            orphans={orphans}
            full={full}
            onKeepOnScreen={onKeepOnScreen}
            onRemove={onRemove}
            onOpen={(i) => {
              setExpanded(false);
              onOpen(i);
            }}
            onCommentOnScreen={() => {
              setExpanded(false);
              onCommentOnScreen();
            }}
          />
        )}
      </Box>
      <Button
        size="small"
        color="inherit"
        aria-expanded={expanded}
        aria-controls={expanded ? listId : undefined}
        endIcon={expanded ? <ChevronDown size={16} /> : <ChevronUp size={16} />}
        onClick={() => setExpanded((e) => !e)}
        sx={{ textTransform: "none", fontWeight: 600, flexShrink: 0 }}
      >
        {commentCount(requests.length)}
      </Button>
      {/* While the agent revises, Send itself says so: it is disabled anyway, so the state takes no extra room. */}
      <Box component="span" aria-live="polite" sx={visuallyHidden}>
        {revising === null ? "" : revising > 0 ? `Agent is revising… (${commentCount(revising)})` : "Agent is revising…"}
      </Box>
      <Button
        size="small"
        variant="contained"
        startIcon={revising === null ? <Send size={16} /> : <CircularProgress size={14} color="inherit" aria-hidden />}
        onClick={onSend}
        disabled={blocked}
        sx={{ textTransform: "none", flexShrink: 0 }}
      >
        {revising === null ? "Send to agent" : "Revising…"}
      </Button>
    </DockGroup>
  );
}
