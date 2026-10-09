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

/**
 * The preview host: a header with the prototype's name, the live prototype in
 * a browser window, the review's dock below it (every control), the findings
 * overlay and (in preview) Comment mode: comment bubbles at the elements (or,
 * clicking empty space, on the whole screen at that spot), pins that open
 * them again, and the dock's comments, which save the feedback file.
 */

import { useCallback, useMemo, useRef, useState } from "react";
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
  type DataSnapshot,
  type PrototypeFrameHandle,
  type PrototypeManifest,
  type QueueUpdate,
  type ReviewEvent,
} from "@wso2/prototype-kit/host";
import {
  EMPTY_FEEDBACK_QUEUE,
  MAX_FEEDBACK_REQUESTS,
  dequeue,
  earlierComments,
  draftAt,
  draftOfPin,
  draftPinsOnScreen,
  editRequest,
  enqueue,
  pinsOnScreen,
  screenPinsOnScreen,
  submissionOf,
  targetLabel,
  type FeedbackQueue,
} from "@wso2/prototype-kit/feedback";
import { FEEDBACK_PATH } from "../feedback.js";
import type { HostConfig, PrototypeRevision } from "../host-config.js";
import { BubbleBounds } from "./AnchoredBubble.js";
import { CommentBubble } from "./CommentBubble.js";
import { CommentQueue, count } from "./CommentQueue.js";
import { Dock } from "./Dock.js";
import { FindingsOverlay } from "./FindingsOverlay.js";
import { useLivePrototype } from "./live.js";
import { clearSnapshot, loadSnapshot, saveSnapshot } from "./persistence.js";
import { HOST_CSS } from "./styles.js";
import { ModeTools } from "./ModeTools.js";
import { QueuedCommentBubble } from "./QueuedCommentBubble.js";
import { ViewControls } from "./ViewControls.js";

export function App({ config }: { config: HostConfig }) {
  const live = useLivePrototype(config);
  const waiting = live.error ?? (live.findings.length > 0 ? "The prototype has check findings; it shows once they are fixed." : "Loading the prototype…");
  return (
    <div className="ph-app">
      <style>{HOST_CSS}</style>
      {live.revision && live.runtime ? <Review config={config} runtime={live.runtime} revision={live.revision} /> : <p className="ph-waiting">{waiting}</p>}
      {live.findings.length > 0 && <FindingsOverlay findings={live.findings} showingLastGood={live.revision !== null} />}
    </div>
  );
}

function screenName(manifest: PrototypeManifest, screenId: string): string {
  return manifest.screens.find((x) => x.id === screenId)?.name ?? screenId;
}

function Review({ config, runtime, revision }: { config: HostConfig; runtime: string; revision: PrototypeRevision }) {
  const { manifest } = revision;
  const [state, setState] = useState(() => ({ manifest, review: initialReview(manifest) }));
  // A replaced manifest repairs the view in the same render, so the frame is never sent a view naming a screen the new manifest lacks.
  let current = state;
  if (state.manifest !== manifest) {
    current = { manifest, review: reduceReview(manifest, state.review, { type: "MANIFEST_REPLACED", manifest }) };
    setState(current);
  }
  const { view, bubble } = current.review;
  const dispatch = useCallback((event: ReviewEvent) => setState((s) => ({ ...s, review: reduceReview(s.manifest, s.review, event) })), []);

  // Annotate (preview only): the screen's element labels, the comment queue with its drafts, and their pins.
  const annotate = config.mode === "preview";
  // Each visited screen's element labels, as the frame last reported them, which comments are named by.
  const [labels, setLabels] = useState<Readonly<Record<string, Readonly<Record<string, string>>>>>({});
  const [queue, setQueue] = useState<FeedbackQueue>(EMPTY_FEEDBACK_QUEUE);
  const onQueue = useCallback((update: QueueUpdate) => setQueue(update), []);
  const earlier = useMemo(() => earlierComments(queue, revision.hash), [queue, revision.hash]);
  const { requests } = queue;
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
  const full = requests.length >= MAX_FEEDBACK_REQUESTS;
  const opened = bubble?.on === "comment" ? requests[bubble.index] : undefined;
  // Comment mode shows itself: a ring round the window, a tag in its bar, a hint in the dock.
  const commenting = annotate && view.mode === "annotate";
  const labelsOf = (keys: readonly string[]) => keys.map((k) => labels[view.screenId]?.[k] ?? k);

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
  /** Escape, wherever it came from: the bubble, then the selection; false when there was neither. */
  const escape = () => {
    if (bubble) closeBubble(true);
    else if (view.selectedKeys.length > 0) dispatch({ type: "CLEAR_SELECTION" });
    else return false;
    return true;
  };
  useReviewKeys(
    annotate
      ? {
          onEscape: escape,
          onToggleAnnotate: () => dispatch({ type: view.mode === "annotate" ? "EXIT_ANNOTATE" : "ENTER_ANNOTATE" }),
          onPreview: () => dispatch({ type: "EXIT_ANNOTATE" }),
        }
      : null,
  );

  const add = (text: string) => {
    const comment = newComment(current.review, text);
    if (!comment) return;
    // The feedback is given against the revision showing when its first comment was queued.
    onQueue((q) => enqueue(q, revision.hash, comment));
    // Added, the comment is no longer a draft to keep.
    draft.setText("");
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
  /** A pin in the frame: a queued comment's opens it; a draft pin reopens the draft on its elements (in Annotate). */
  const openPin = (key: string, numbers: number[]) => {
    if (!annotate) return;
    if (numbers.length === 0) {
      const kept = draftOfPin(queue, view.screenId, key);
      if (kept) dispatch({ type: "SELECT_ELEMENTS", elementKeys: kept.elementIds });
      return;
    }
    const index = (numbers[0] ?? 0) - 1;
    if (requests[index]) dispatch({ type: "OPEN_PIN", index, pin: { key, requests: numbers } });
  };
  /** A whole-screen comment's pin: a queued one's opens it; the hollow one reopens the screen's draft at its spot (in Annotate). */
  const openScreenPin = (numbers: number[]) => {
    if (!annotate) return;
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
  const save = async () => {
    const submission = submissionOf(queue);
    if (!submission) throw new Error("there are no comments to save");
    const response = await fetch("feedback", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(submission) });
    if (!response.ok) throw new Error(await response.text());
    return `Saved ${count(submission.requests.length)} to ${FEEDBACK_PATH}`;
  };

  // Mock data: persisted per revision when --persist is on; Reset starts from the seed.
  const persist = config.mode === "preview" && config.persist;
  const initialData = useMemo(() => (persist ? loadSnapshot(revision.hash) : undefined), [persist, revision.hash]);
  const [resetToken, setResetToken] = useState(0);
  const onData = useCallback((data: DataSnapshot) => persist && saveSnapshot(revision.hash, data), [persist, revision.hash]);
  const reset = () => {
    clearSnapshot(revision.hash);
    setResetToken((t) => t + 1);
  };

  return (
    <>
      <header className="ph-header">
        <h1 className="ph-name">{manifest.name}</h1>
      </header>
      {/* The window, then the dock below it: the dock's own space is reserved, so it never covers the prototype. */}
      <div className="ph-body">
        <div ref={setStage} className="ph-stage">
          <PrototypeWindow
            title={manifest.name}
            manifest={manifest}
            view={view}
            className={commenting ? "ph-commenting" : undefined}
            tag={
              commenting && (
                <span className="ph-mode-tag">
                  Comment mode <kbd>Esc</kbd>
                </span>
              )
            }
          >
            <PrototypeFrame
              ref={frame}
              title={manifest.name}
              runtime={runtime}
              manifest={manifest}
              source={revision.source}
              version={revision.hash}
              view={frameView}
              initialData={initialData}
              resetToken={resetToken}
              onNavigate={(screenId) => {
                // The frame is untrusted: only Preview navigates (the reducer checks the target against the role).
                if (view.mode === "preview") dispatch({ type: "NAVIGATE", screenId });
              }}
              onToggle={(elementKey, additive) => {
                if (additive) draft.carryNext();
                dispatch({ type: additive ? "TOGGLE_SELECTION" : "SELECT_ONLY", elementKey });
              }}
              onPin={openPin}
              onScreenClick={(at) => {
                if (annotate) dispatch({ type: "SCREEN_CLICK", at });
              }}
              onScreenPin={openScreenPin}
              onGeometry={anchors.onGeometry}
              onEscape={escape}
              onElements={(screenId, elements) => setLabels((all) => ({ ...all, [screenId]: Object.fromEntries(elements.map((e) => [e.key, e.label])) }))}
              onData={onData}
            />
          </PrototypeWindow>
        </div>
        <BubbleBounds.Provider value={stage}>
          {annotate && (bubble?.on === "selection" || bubble?.on === "screen") && (
            <CommentBubble
              // A new bubble (another selection, or the screen) starts afresh.
              key={bubble.on}
              anchor={bubble.on === "selection" ? anchors.anchor(view.selectedKeys) : bubble.at ? anchors.point(bubble.at) : dock}
              labels={bubble.on === "screen" ? [`${screenName(manifest, view.screenId)} (whole screen)`] : labelsOf(view.selectedKeys)}
              full={full}
              text={draft.text}
              onText={draft.setText}
              onAdd={add}
              // Typed text is kept as a draft where it was written.
              onClose={closeBubble}
            />
          )}
          {annotate && bubble?.on === "comment" && opened && (
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
        <Dock ref={setDock}>
          <ViewControls manifest={manifest} view={view} dispatch={dispatch} onReset={reset} />
          {annotate && <ModeTools view={view} dispatch={dispatch} />}
          {annotate && (
            <CommentQueue
              manifest={manifest}
              requests={requests}
              labels={labels}
              earlier={earlier}
              onCommentOnScreen={() => dispatch({ type: "COMMENT_ON_SCREEN" })}
              onOpen={open}
              onRemove={remove}
              onSave={save}
            />
          )}
        </Dock>
      </div>
    </>
  );
}
