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

import { useCallback, useEffect, useId, useMemo, useState, type ReactNode } from "react";
import { Alert, Box, Chip, CircularProgress, Dialog, IconButton, Tooltip, Typography, useColorScheme } from "@wso2/oxygen-ui";
import { X } from "@wso2/oxygen-ui-icons-react";
import {
  PrototypeFrame,
  PrototypeWindow,
  frameViewOf,
  initialPrototypeView,
  reducePrototypeView,
  type PrototypeViewEvent,
} from "@wso2/prototype-kit/host";
import type { PrototypeFeedback } from "../../agent-chat/turnScope";
import { pinsOnScreen, requestFor } from "@wso2/prototype-kit/feedback";
import { dequeue, enqueue, feedbackBatch, type ReviewQueue } from "../model/feedback";
import type { AppPrototype, PrototypeFiles } from "../model/prototypes";
import { useFrameRuntime, usePrototypeHash } from "../useReviewAssets";
import { FeedbackPanel } from "./FeedbackPanel";
import { ReviewToolbar } from "./ReviewToolbar";

export interface PrototypeReviewProps {
  prototype: AppPrototype;
  /** The requests queued on this prototype so far; they outlive the overlay. */
  queue: ReviewQueue | null;
  onQueue: (queue: ReviewQueue | null) => void;
  /** Whether a turn can start now (the chat is loaded and idle). */
  ready: boolean;
  /** Send the batch as one revision turn; resolves false when it was not sent. */
  onSend: (feedback: PrototypeFeedback) => Promise<boolean>;
  /** The revision showing: the review has looked at it. */
  onSeen: (hash: string) => void;
  onClose: () => void;
}

const BUSY = "The agent is working on another turn, so nothing was sent. Your requests are kept: send them once it finishes.";
const NOT_SENT = "Your requests weren't sent; the chat says why. They are kept: try again.";

function Header({
  titleId,
  title,
  revising,
  onClose,
  children,
}: {
  titleId: string;
  title: string;
  revising: boolean;
  onClose: () => void;
  children?: ReactNode;
}) {
  return (
    <Box
      component="header"
      sx={{ display: "flex", alignItems: "center", flexWrap: "wrap", gap: 2, px: 2.5, py: 1.25, borderBottom: 1, borderColor: "divider" }}
    >
      <Typography component="h2" id={titleId} sx={{ fontSize: "1rem", fontWeight: 600 }}>
        {title}
      </Typography>
      {revising && <Chip size="small" color="info" label="Revising…" />}
      <Box sx={{ flex: 1, display: "flex", justifyContent: "flex-end", minWidth: 0 }}>{children}</Box>
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

function Waiting({ children }: { children: ReactNode }) {
  return <Box sx={{ flex: 1, display: "grid", placeItems: "center", p: 4 }}>{children}</Box>;
}

/**
 * The full-screen prototype review (spec #860): an Oxygen dialog over the
 * whole console, closed with its X or Escape. It runs the prototype in the
 * kit's sandboxed `PrototypeFrame` on the Oxygen theme's frame runtime; the
 * kit's view reducer owns the pickers and Annotate, so Preview acts and
 * Annotate only selects. A prototype that cannot be shown says why instead of
 * drawing a blank frame.
 */
export function PrototypeReview(props: PrototypeReviewProps) {
  const { prototype, onClose } = props;
  const titleId = useId();
  const runtime = useFrameRuntime();
  const files = prototype.files;
  const revising = prototype.status === "revising";
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
            <Header titleId={titleId} title={`Prototype · ${name}`} revising={revising} onClose={onClose} />
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
  queue,
  onQueue,
  ready,
  onSend,
  onSeen,
  onClose,
}: PrototypeReviewProps & { titleId: string; files: PrototypeFiles; runtime: string; revising: boolean }) {
  const hash = usePrototypeHash(files.manifestText, files.source);
  const colorScheme = useResolvedScheme();
  const { manifest } = files;
  useEffect(() => {
    onSeen(hash);
  }, [hash, onSeen]);
  const [state, setState] = useState(() => ({ manifest, view: initialPrototypeView(manifest) }));
  // A revision that lands while the review is open repairs the view in the same render, as the kit CLI's host does.
  let current = state;
  if (state.manifest !== manifest) {
    current = { manifest, view: reducePrototypeView(manifest, state.view, { type: "MANIFEST_REPLACED", manifest }) };
    setState(current);
  }
  const { view } = current;
  const dispatch = useCallback(
    (event: PrototypeViewEvent) => setState((s) => ({ ...s, view: reducePrototypeView(s.manifest, s.view, event) })),
    [],
  );

  const requests = useMemo(() => queue?.requests ?? [], [queue]);
  const [labels, setLabels] = useState<Record<string, string>>({});
  const [resetToken, setResetToken] = useState(0);
  const [refused, setRefused] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  const pins = useMemo(() => pinsOnScreen(requests, view.screenId), [requests, view.screenId]);
  const frameView = useMemo(() => frameViewOf(view, pins), [view, pins]);
  const annotating = view.mode === "annotate";

  const add = (text: string) => {
    onQueue(enqueue(queue, hash, requestFor(view, text)));
    setRefused(null);
    dispatch({ type: "CLEAR_SELECTION" });
  };
  const remove = (index: number) => queue && onQueue(dequeue(queue, index));
  const send = async () => {
    if (!queue || queue.requests.length === 0) return;
    if (!ready) {
      setRefused(BUSY);
      return;
    }
    setSending(true);
    const feedback = feedbackBatch(prototype.component, queue);
    const sent = await onSend(feedback);
    setSending(false);
    if (!sent) {
      setRefused(NOT_SENT);
      return;
    }
    onQueue(null);
    onClose();
  };

  return (
    <>
      <Header titleId={titleId} title={`Prototype · ${manifest.name}`} revising={revising} onClose={onClose}>
        <ReviewToolbar manifest={manifest} view={view} dispatch={dispatch} onReset={() => setResetToken((t) => t + 1)} />
      </Header>
      <Box sx={{ flex: 1, minHeight: 0, display: "flex", bgcolor: "background.default" }}>
        <Box sx={{ flex: 1, minWidth: 0, display: "flex", p: 2, ...WINDOW_LOOK }}>
          <PrototypeWindow title={manifest.name} manifest={manifest} view={view}>
            <PrototypeFrame
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
              onToggle={(elementKey) => dispatch({ type: "TOGGLE_SELECTION", elementKey })}
              onEscape={() => (view.selectedKeys.length > 0 ? dispatch({ type: "CLEAR_SELECTION" }) : onClose())}
              onElements={(_screenId, elements) => setLabels(Object.fromEntries(elements.map((e) => [e.key, e.label])))}
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
        </Box>
        {(annotating || requests.length > 0) && (
          <FeedbackPanel
            selection={view.selectedKeys.map((k) => labels[k] ?? k)}
            annotating={annotating}
            requests={requests}
            stale={queue !== null && queue.hash !== hash}
            refused={refused}
            sending={sending}
            onAdd={add}
            onRemove={remove}
            onSend={() => void send()}
          />
        )}
      </Box>
    </>
  );
}
