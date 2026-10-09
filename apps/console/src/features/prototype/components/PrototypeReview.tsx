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

import { useCallback, useEffect, useId, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { Alert, Box, Button, CircularProgress, Dialog, IconButton, Snackbar, Tooltip, Typography, useColorScheme } from "@wso2/oxygen-ui";
import { X } from "@wso2/oxygen-ui-icons-react";
import {
  PrototypeFrame,
  PrototypeWindow,
  frameViewOf,
  initialReview,
  newComment,
  reduceReview,
  useCommentDraft,
  useFrameAnchors,
  useReviewKeys,
  type PrototypeFrameHandle,
  type QueueUpdate,
  type ReviewEvent,
} from "@wso2/prototype-kit/host";
import type { PrototypeFeedback } from "../../agent-chat/turnScope";
import {
  dequeue,
  draftAt,
  draftOfPin,
  draftPinsOnScreen,
  earlierComments,
  editRequest,
  enqueue,
  keepOnScreen,
  orphansOnScreen,
  pinsOnScreen,
  screenPinsOnScreen,
  targetLabel,
} from "@wso2/prototype-kit/feedback";
import { commentCount, feedbackBatch, queueFull } from "../model/feedback";
import type { AppPrototype, PrototypeFiles } from "../model/prototypes";
import type { ReviewSession, RevisionNotice } from "../model/revision";
import { useFrameRuntime, usePrototypeHash } from "../useReviewAssets";
import { BubbleBounds } from "./AnchoredBubble";
import { CommentBubble } from "./CommentBubble";
import { Key } from "./Key";
import { QueuedCommentBubble } from "./QueuedCommentBubble";
import { CommentQueue } from "./CommentQueue";
import { ModeTools, PRIMARY_TINT } from "./ModeTools";
import { ReviewDock } from "./ReviewDock";
import { ViewControls } from "./ViewControls";

export interface PrototypeReviewProps {
  prototype: AppPrototype;
  /**
   * What outlives the overlay: the comments queued and drafted so far, the
   * batch out with the agent, the revision to show (kept while the agent
   * revises it) and what the last revision's end left to say.
   */
  session: ReviewSession;
  onQueue: (update: QueueUpdate) => void;
  /** Whether a turn can start now (the chat is loaded and idle). */
  ready: boolean;
  /** Send the batch as one revision turn; resolves false when it was not sent. */
  onSend: (feedback: PrototypeFeedback) => Promise<boolean>;
  /** The revision showing: the review has looked at it. */
  onSeen: (hash: string) => void;
  /** The notice was seen: the "Updated" toast closed. */
  onNoticeSeen: () => void;
  /** Read what the agent did: the chat, with the review closed. */
  onWhatChanged: () => void;
  onClose: () => void;
}

const BUSY = "The agent is working on another turn, so nothing was sent. Your comments are kept: send them once it finishes.";
const NOT_SENT = "Your comments weren't sent; the chat says why. They are kept: try again.";

/** The review's header: only its title and Close; every control is in the dock. */
function Header({ titleId, title, onClose }: { titleId: string; title: string; onClose: () => void }) {
  return (
    <Box component="header" sx={{ display: "flex", alignItems: "center", gap: 2, minHeight: 48, pl: 2.5, pr: 1, borderBottom: 1, borderColor: "divider" }}>
      <Typography component="h2" id={titleId} noWrap sx={{ flex: 1, minWidth: 0, fontSize: "1rem", fontWeight: 600 }}>
        {title}
      </Typography>
      <Tooltip title="Close (Esc)">
        <IconButton aria-label="Close" onClick={onClose}>
          <X size={18} />
        </IconButton>
      </Tooltip>
    </Box>
  );
}

/**
 * Whether the dialog should close for this Escape. Escape with focus in the
 * prototype is the prototype's: the frame reports it (`onEscape`) only when
 * the prototype left it unused. A key this document still gets with the frame
 * element as its target was aimed there, so the dialog leaves it to the frame.
 */
function closesOnEscape(event: { target: EventTarget | null }): boolean {
  return !(event.target instanceof HTMLIFrameElement);
}

/** The console's scheme as it draws now (its mode, or the system's under "system"), for the prototype to match. */
function useResolvedScheme(): "light" | "dark" | undefined {
  const { mode, systemMode } = useColorScheme();
  const resolved = mode === "system" ? systemMode : mode;
  return resolved === "light" || resolved === "dark" ? resolved : undefined;
}

/** The kit's prototype window drawn in the console's theme (light and dark follow the Oxygen palette). */
const WINDOW_LOOK = {
  "--proto-window-border": "var(--oxygen-palette-divider)",
  "--proto-window-radius": "8px",
  "--proto-window-bg": "var(--oxygen-palette-background-paper)",
  "--proto-window-shadow": "var(--oxygen-shadows-2, 0 2px 8px rgba(0,0,0,.15))",
  "--proto-window-bar-bg": "var(--oxygen-palette-action-hover)",
  "--proto-window-bar-border": "var(--oxygen-palette-divider)",
  "--proto-window-fg": "var(--oxygen-palette-text-primary)",
  "--proto-window-dot": "var(--oxygen-palette-action-disabled)",
  "--proto-window-address-bg": "var(--oxygen-palette-background-paper)",
  "--proto-window-address-fg": "var(--oxygen-palette-text-secondary)",
  fontSize: "0.8125rem",
} as const;

function screenName(manifest: PrototypeFiles["manifest"], screenId: string): string {
  return manifest.screens.find((x) => x.id === screenId)?.name ?? screenId;
}

/** What the frame last reported drawing, and for which revision and view it was told then. */
interface Rendered {
  hash: string;
  screenId: string;
  roleId: string;
  stateId: string;
  keys: string[];
}

/** Whether the frame's report is of `hash` drawn as `view` shows it (screen, role, state). */
function drawnFor(r: Rendered, hash: string, view: { screenId: string; roleId: string; stateId: string }): boolean {
  return r.hash === hash && r.screenId === view.screenId && r.roleId === view.roleId && r.stateId === view.stateId;
}

/** In Comment mode, the window's ring: a primary border with a soft halo. */
const COMMENT_RING = {
  "--proto-window-border": "var(--oxygen-palette-primary-main)",
  "--proto-window-shadow": `0 0 0 3px ${PRIMARY_TINT}`,
} as CSSProperties;

/** In Comment mode, the window bar's tag: the mode, and the key that leaves it. */
function CommentModeTag() {
  return (
    <Typography component="span" variant="caption" color="primary" sx={{ display: "inline-flex", alignItems: "center", gap: 0.75, fontWeight: 600, whiteSpace: "nowrap" }}>
      Comment mode
      <Key>Esc</Key>
    </Typography>
  );
}

function Waiting({ children }: { children: ReactNode }) {
  return <Box sx={{ flex: 1, display: "grid", placeItems: "center", p: 4 }}>{children}</Box>;
}

/**
 * The full-screen prototype review (spec #860): an Oxygen dialog over the
 * whole console, closed with its X or Escape. It runs the prototype in the
 * kit's sandboxed `PrototypeFrame` on the Oxygen theme's frame runtime; the
 * kit's view reducer owns the role, state and mode, so Preview acts and
 * Comment (the view's Annotate) only selects. A prototype that cannot be shown says why instead of
 * drawing a blank frame.
 */
export function PrototypeReview(props: PrototypeReviewProps) {
  const { prototype, onClose } = props;
  const titleId = useId();
  const runtime = useFrameRuntime();
  const files = props.session.shown;
  const revising = props.session.revising;
  const name = files?.manifest.name ?? prototype.component;

  let body: ReactNode = (
    <Waiting>
      <CircularProgress aria-label="Loading the prototype" />
    </Waiting>
  );
  if (!files) {
    body = (
      <Waiting>
        <Alert severity={prototype.problem ? "error" : "info"} sx={{ maxWidth: "60ch" }}>
          {prototype.problem ??
            (revising ? "The agent is writing this prototype. Follow along in the chat." : "There is no prototype for this web application yet.")}
        </Alert>
      </Waiting>
    );
  } else if (runtime.error) {
    body = (
      <Waiting>
        <Alert severity="error">{`Couldn't load the prototype runtime: ${runtime.error}`}</Alert>
      </Waiting>
    );
  }

  return (
    <Dialog
      fullScreen
      open
      onClose={(event: { target: EventTarget | null }, reason) => {
        if (reason !== "escapeKeyDown" || closesOnEscape(event)) onClose();
      }}
      aria-labelledby={titleId}
    >
      <Box sx={{ display: "flex", flexDirection: "column", height: "100%", minHeight: 0 }}>
        {files && runtime.value ? (
          <Session {...props} titleId={titleId} files={files} runtime={runtime.value} revising={revising} />
        ) : (
          <>
            <Header titleId={titleId} title={`Prototype · ${name}`} onClose={onClose} />
            {body}
          </>
        )}
      </Box>
    </Dialog>
  );
}

function Session({
  titleId,
  prototype,
  files,
  runtime,
  revising,
  session,
  onQueue,
  ready,
  onSend,
  onSeen,
  onNoticeSeen,
  onWhatChanged,
  onClose,
}: PrototypeReviewProps & { titleId: string; files: PrototypeFiles; runtime: string; revising: boolean }) {
  const { queue, notice } = session;
  const hash = usePrototypeHash(files.manifestText, files.source);
  const colorScheme = useResolvedScheme();
  const { manifest } = files;
  useEffect(() => {
    onSeen(hash);
  }, [hash, onSeen]);
  const [state, setState] = useState(() => ({ manifest, review: initialReview(manifest) }));
  // A revision that lands while the review is open repairs the view in the same render, as the kit CLI's host does.
  let current = state;
  if (state.manifest !== manifest) {
    current = { manifest, review: reduceReview(manifest, state.review, { type: "MANIFEST_REPLACED", manifest }) };
    setState(current);
  }
  const { view, bubble } = current.review;
  const dispatch = useCallback(
    (event: ReviewEvent) => setState((s) => ({ ...s, review: reduceReview(s.manifest, s.review, event) })),
    [],
  );

  const { requests } = queue;
  // Each visited screen's element labels, as the frame last reported them, which comments are named by.
  const [labels, setLabels] = useState<Readonly<Record<string, Readonly<Record<string, string>>>>>({});
  // The elements the frame last said it draws, and for what (revision, screen, role, state): what comments are checked against.
  const [rendered, setRendered] = useState<Rendered | null>(null);
  const [resetToken, setResetToken] = useState(0);
  const [refused, setRefused] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  const pins = useMemo(() => pinsOnScreen(requests, view.screenId), [requests, view.screenId]);
  const drafts = useMemo(() => draftPinsOnScreen(queue, view.screenId), [queue, view.screenId]);
  const writing = bubble?.on === "screen" ? bubble : null;
  const screenPins = useMemo(() => screenPinsOnScreen(queue, view.screenId, writing), [queue, view.screenId, writing]);
  const frameView = useMemo(() => frameViewOf(view, pins, drafts, screenPins), [view, pins, drafts, screenPins]);
  const frame = useRef<PrototypeFrameHandle>(null);
  const draft = useCommentDraft({ review: current.review, queue, onQueue });
  const anchors = useFrameAnchors();
  // The dock, which a whole-screen comment's bubble points at when it has no spot.
  const [dock, setDock] = useState<HTMLElement | null>(null);
  // The stage: the prototype window's area, above the dock, which bubbles keep within.
  const [stage, setStage] = useState<HTMLDivElement | null>(null);
  // The comments out with the agent keep their places in the queue until their turn ends.
  const held = session.sent?.feedback.requests.length ?? 0;
  const full = queueFull(requests.length, held);
  // Comments written on an earlier revision may point at elements it took away.
  const earlier = useMemo(() => earlierComments(queue, hash), [queue, hash]);
  // Checked only against what the frame drew for this very revision and view, never a report from before a swap or a switch.
  const orphans = useMemo(
    () => (rendered !== null && drawnFor(rendered, hash, view) ? orphansOnScreen(queue, hash, view, rendered.keys) : []),
    [rendered, queue, hash, view],
  );
  const opened = bubble?.on === "comment" ? requests[bubble.index] : undefined;
  // Comment mode shows itself: a ring round the window, a tag in its bar, a hint in the dock.
  const commenting = view.mode === "annotate";

  /** Keyboard focus back into the prototype, on the element a bubble was on (or the pin that opened it). */
  const focusBack = (key: string | undefined, requests?: readonly number[]) => {
    if (key !== undefined) frame.current?.focusElement(key, requests);
  };
  /** Close the bubble; `refocus`: keyboard focus goes back to where it pointed (its element, its spot's pin, or the pin that opened it). */
  const closeBubble = (refocus: boolean) => {
    if (refocus && bubble?.on === "selection") focusBack(view.selectedKeys[0]);
    else if (refocus && bubble?.on === "screen" && bubble.at) frame.current?.focusScreenPin([]);
    else if (refocus && bubble?.on === "comment" && bubble.pin?.key !== undefined) focusBack(bubble.pin.key, bubble.pin.requests);
    else if (refocus && bubble?.on === "comment" && bubble.pin) frame.current?.focusScreenPin(bubble.pin.requests);
    dispatch({ type: "CLOSE_BUBBLE" });
  };
  /** Escape, wherever it came from: the bubble, then the selection, then (false) the review. */
  const escape = () => {
    if (bubble) closeBubble(true);
    else if (view.selectedKeys.length > 0) dispatch({ type: "CLEAR_SELECTION" });
    else return false;
    return true;
  };
  // Captured, so an Escape the review used never reaches its dialog.
  useReviewKeys(
    {
      onEscape: escape,
      onToggleAnnotate: () => dispatch({ type: view.mode === "annotate" ? "EXIT_ANNOTATE" : "ENTER_ANNOTATE" }),
      onPreview: () => dispatch({ type: "EXIT_ANNOTATE" }),
    },
    { capture: true },
  );

  const add = (text: string) => {
    const comment = newComment(current.review, text);
    if (!comment || full) return;
    onQueue((q) => enqueue(q, hash, comment));
    // Added, the comment is no longer a draft to keep.
    draft.setText("");
    setRefused(null);
    focusBack(view.selectedKeys[0]);
    dispatch({ type: "CLEAR_SELECTION" });
  };
  const remove = (index: number) => {
    // An open comment's number would shift under it; its pin goes, so focus goes to its element.
    if (bubble?.on === "comment") {
      focusBack(bubble.pin?.key);
      dispatch({ type: "CLOSE_BUBBLE" });
    }
    onQueue((q) => dequeue(q, index));
  };
  /** A pin in the frame: a queued comment's opens it; a draft pin reopens the draft on its elements (in Comment mode). */
  const openPin = (key: string, numbers: number[]) => {
    if (numbers.length === 0) {
      const kept = draftOfPin(queue, view.screenId, key);
      if (kept) dispatch({ type: "SELECT_ELEMENTS", elementKeys: kept.elementIds });
      return;
    }
    const index = (numbers[0] ?? 0) - 1;
    if (requests[index]) dispatch({ type: "OPEN_PIN", index, pin: { key, requests: numbers } });
  };
  /** A whole-screen comment's pin: a queued one's opens it; the hollow one reopens the screen's draft at its spot (in Comment mode). */
  const openScreenPin = (numbers: number[]) => {
    if (numbers.length === 0) {
      const at = draftAt(queue, view.screenId, [])?.at;
      if (at) dispatch({ type: "COMMENT_ON_SCREEN", at });
      return;
    }
    const index = (numbers[0] ?? 0) - 1;
    if (requests[index]) dispatch({ type: "OPEN_PIN", index, pin: { requests: numbers } });
  };
  const open = (index: number) => {
    const request = requests[index];
    if (request) dispatch({ type: "OPEN_COMMENT", index, request });
  };
  const labelsOf = (keys: readonly string[]) => keys.map((k) => labels[view.screenId]?.[k] ?? k);
  const send = async () => {
    const feedback = feedbackBatch(prototype.component, queue);
    if (!feedback) return;
    if (!ready) {
      setRefused(BUSY);
      return;
    }
    setSending(true);
    const delivered = await onSend(feedback);
    setSending(false);
    setRefused(delivered ? null : NOT_SENT);
  };

  return (
    <>
      <Header titleId={titleId} title={`Prototype · ${manifest.name}`} onClose={onClose} />
      {/* The window, then the dock below it: the dock's own space is reserved, so it never covers the prototype. */}
      <Box sx={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column", gap: 1.5, p: 2, bgcolor: "background.default" }}>
        <Box ref={setStage} sx={{ flex: 1, minHeight: 0, display: "flex", ...WINDOW_LOOK }}>
          <PrototypeWindow
            title={manifest.name}
            manifest={manifest}
            view={view}
            style={commenting ? COMMENT_RING : undefined}
            tag={commenting && <CommentModeTag />}
          >
            <PrototypeFrame
              ref={frame}
              title={manifest.name}
              runtime={runtime}
              manifest={manifest}
              source={files.source}
              version={hash}
              view={frameView}
              resetToken={resetToken}
              colorScheme={colorScheme}
              onNavigate={(screenId) => {
                // The frame is untrusted: only Preview navigates (the reducer checks the target against the role).
                if (view.mode === "preview") dispatch({ type: "NAVIGATE", screenId });
              }}
              onToggle={(elementKey, additive) => {
                if (additive) draft.carryNext();
                dispatch({ type: additive ? "TOGGLE_SELECTION" : "SELECT_ONLY", elementKey });
              }}
              onPin={openPin}
              onScreenClick={(at) => dispatch({ type: "SCREEN_CLICK", at })}
              onScreenPin={openScreenPin}
              onGeometry={anchors.onGeometry}
              onEscape={() => escape() || onClose()}
              onElements={(screenId, elements) => {
                setLabels((all) => ({ ...all, [screenId]: Object.fromEntries(elements.map((e) => [e.key, e.label])) }));
                setRendered({ hash, screenId, roleId: view.roleId, stateId: view.stateId, keys: elements.map((e) => e.key) });
              }}
              loading={
                <Box sx={{ position: "absolute", inset: 0, display: "grid", placeItems: "center", bgcolor: "background.paper" }}>
                  <Box sx={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 1.5 }}>
                    <CircularProgress size={28} aria-hidden />
                    <Typography variant="body2" color="text.secondary">
                      Starting the prototype…
                    </Typography>
                  </Box>
                </Box>
              }
            />
          </PrototypeWindow>
          <BubbleBounds.Provider value={stage}>
            {(bubble?.on === "selection" || bubble?.on === "screen") && (
              <CommentBubble
                // A new bubble (another selection, or the screen) starts empty.
                key={bubble.on}
                anchor={bubble.on === "selection" ? anchors.anchor(view.selectedKeys) : bubble.at ? anchors.point(bubble.at) : dock}
                labels={bubble.on === "screen" ? [`${screenName(manifest, view.screenId)} (whole screen)`] : labelsOf(view.selectedKeys)}
                full={full}
                text={draft.text}
                onText={draft.setText}
                onAdd={add}
                // Typed text is kept as a draft (useCommentDraft).
                onClose={closeBubble}
              />
            )}
            {bubble?.on === "comment" && opened && (
              <QueuedCommentBubble
                key={bubble.index}
                anchor={opened.elementIds.length > 0 ? anchors.anchor(opened.elementIds) : opened.at ? anchors.point(opened.at) : dock}
                number={bubble.index + 1}
                request={opened}
                on={targetLabel(opened, labels[opened.screenId] ?? {})}
                onEdit={(text) => onQueue((q) => editRequest(q, bubble.index, text))}
                onRemove={() => remove(bubble.index)}
                onClose={closeBubble}
              />
            )}
          </BubbleBounds.Provider>
        </Box>
        <ReviewDock ref={setDock}>
          <ViewControls manifest={manifest} view={view} dispatch={dispatch} onReset={() => setResetToken((t) => t + 1)} />
          <ModeTools view={view} dispatch={dispatch} />
          <CommentQueue
            manifest={manifest}
            requests={requests}
            labels={labels}
            earlier={earlier}
            held={held}
            refused={refused}
            sending={sending}
            revising={revising ? (session.sent?.feedback.requests.length ?? 0) : null}
            failed={notice?.kind === "failed" ? notice.reason : null}
            orphans={orphans}
            onSend={() => void send()}
            onKeepOnScreen={(index) => onQueue((q) => keepOnScreen(q, index))}
            onCommentOnScreen={() => dispatch({ type: "COMMENT_ON_SCREEN" })}
            onOpen={open}
            onRemove={remove}
          />
        </ReviewDock>
      </Box>
      <UpdatedToast notice={notice} onWhatChanged={onWhatChanged} onClose={onNoticeSeen} />
    </>
  );
}

/** "Updated" once a revision landed in the open review, with the way to what the agent did. */
function UpdatedToast({ notice, onWhatChanged, onClose }: { notice: RevisionNotice; onWhatChanged: () => void; onClose: () => void }) {
  const updated = notice?.kind === "updated" ? notice : null;
  return (
    <Snackbar
      open={updated !== null}
      autoHideDuration={8000}
      anchorOrigin={{ vertical: "top", horizontal: "center" }}
      onClose={(_event, reason) => reason !== "clickaway" && onClose()}
    >
      <Alert
        severity="success"
        variant="filled"
        action={
          <Button color="inherit" size="small" onClick={onWhatChanged}>
            What changed
          </Button>
        }
      >
        {updated && (updated.addressed > 0 ? `Updated · ${commentCount(updated.addressed)} addressed` : "Updated")}
      </Alert>
    </Snackbar>
  );
}
