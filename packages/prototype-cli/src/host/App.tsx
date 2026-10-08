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

/** The preview host: the live prototype in a browser window, the review controls, the findings overlay and (in preview) Annotate. */

import { useCallback, useMemo, useState } from "react";
import { PrototypeFrame, PrototypeWindow, frameViewOf, initialPrototypeView, reducePrototypeView, type DataSnapshot, type PrototypeViewEvent } from "@wso2/prototype-kit/host";
import { pinsOnScreen, requestFor, type FeedbackRequest, type FeedbackSubmission } from "@wso2/prototype-kit/feedback";
import { FEEDBACK_PATH } from "../feedback.js";
import type { HostConfig, PrototypeRevision } from "../host-config.js";
import { FeedbackPanel } from "./FeedbackPanel.js";
import { FindingsOverlay } from "./FindingsOverlay.js";
import { useLivePrototype } from "./live.js";
import { clearSnapshot, loadSnapshot, saveSnapshot } from "./persistence.js";
import { HOST_CSS } from "./styles.js";
import { Toolbar } from "./Toolbar.js";

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

function Review({ config, runtime, revision }: { config: HostConfig; runtime: string; revision: PrototypeRevision }) {
  const { manifest } = revision;
  const [state, setState] = useState(() => ({ manifest, view: initialPrototypeView(manifest) }));
  // A replaced manifest repairs the view in the same render, so the frame is never sent a view naming a screen the new manifest lacks.
  let current = state;
  if (state.manifest !== manifest) {
    current = { manifest, view: reducePrototypeView(manifest, state.view, { type: "MANIFEST_REPLACED", manifest }) };
    setState(current);
  }
  const { view } = current;
  const dispatch = useCallback((event: PrototypeViewEvent) => setState((s) => ({ ...s, view: reducePrototypeView(s.manifest, s.view, event) })), []);

  // Annotate (preview only): the screen's element labels, the queued requests and their pins.
  const annotate = config.mode === "preview";
  const [labels, setLabels] = useState<Record<string, string>>({});
  const [queue, setQueue] = useState<FeedbackRequest[]>([]);
  const [queueHash, setQueueHash] = useState(revision.hash);
  const pins = useMemo(() => pinsOnScreen(queue, view.screenId), [queue, view.screenId]);
  const frameView = useMemo(() => frameViewOf(view, pins), [view, pins]);
  const add = (text: string) => {
    // The feedback is given against the revision showing when its first request was queued.
    if (queue.length === 0) setQueueHash(revision.hash);
    setQueue((q) => [...q, requestFor(view, text)]);
    dispatch({ type: "CLEAR_SELECTION" });
  };
  const save = async () => {
    const body: FeedbackSubmission = { prototypeHash: queueHash, requests: queue };
    const response = await fetch("feedback", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
    if (!response.ok) throw new Error(await response.text());
    return `Saved ${queue.length} request${queue.length === 1 ? "" : "s"} to ${FEEDBACK_PATH}`;
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
      <Toolbar manifest={manifest} view={view} dispatch={dispatch} onReset={reset} annotate={annotate} />
      <div className="ph-body">
        <PrototypeWindow title={manifest.name} manifest={manifest} view={view}>
          <PrototypeFrame
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
            onToggle={(elementKey) => dispatch({ type: "TOGGLE_SELECTION", elementKey })}
            onEscape={() => dispatch({ type: "CLEAR_SELECTION" })}
            onElements={(_screenId, elements) => setLabels(Object.fromEntries(elements.map((e) => [e.key, e.label])))}
            onData={onData}
          />
        </PrototypeWindow>
        {annotate && view.mode === "annotate" && (
          <FeedbackPanel
            selection={view.selectedKeys.map((k) => labels[k] ?? k)}
            queue={queue}
            stale={queueHash !== revision.hash}
            onAdd={add}
            onRemove={(index) => setQueue((q) => q.filter((_, i) => i !== index))}
            onSave={save}
          />
        )}
      </div>
    </>
  );
}
