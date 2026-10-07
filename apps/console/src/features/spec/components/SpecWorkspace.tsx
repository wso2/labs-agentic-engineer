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

import { useEffect, useRef, type RefObject } from "react";
import { Box, Button, Skeleton } from "@wso2/oxygen-ui";
import { EmptyState } from "../../../components/EmptyState";
import { PHONE } from "../../shell/layout";
import { useSeeDesignChanges } from "../api/specModel";
import { specTabDot } from "../model/designChanges";
import { openFile } from "../model/files";
import { useOpenSpecTarget, useSpecWorkspace } from "../useSpecWorkspace";
import { FilePicker, FileRail } from "./FileRail";
import { SpecFilePane } from "./SpecFilePane";

/** What `at` names in the open file: the first line to confirm, a line by ID, or a named place. */
function targetSelector(at: string): string {
  if (at === "assumed") return ".aep-line--assumed";
  const value = CSS.escape(at);
  return `[data-line-id="${value}"], [data-anchor="${value}"]`;
}

/**
 * Flash a revealed line. An animation, not a class: the line is ProseMirror's
 * DOM, and ProseMirror puts back any attribute it did not set.
 */
function flash(el: HTMLElement) {
  if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
  const channel = getComputedStyle(el).getPropertyValue("--oxygen-palette-primary-mainChannel").trim();
  if (!channel) return;
  el.animate([{ backgroundColor: `rgba(${channel} / 0.2)` }, { backgroundColor: "transparent" }], {
    duration: 2400,
    easing: "ease-out",
  });
}

/**
 * Bring `at` into view once the file has drawn, and flash it. The lines are
 * decorations, so they exist a few frames after the file opens: retry for a
 * bounded number of frames, then give up quietly.
 */
function useReveal(root: RefObject<HTMLElement | null>, fileKey: string, at: string | undefined) {
  useEffect(() => {
    root.current?.scrollTo({ top: 0 });
  }, [root, fileKey]);
  useEffect(() => {
    const scroller = root.current;
    if (!at || !scroller) return;
    let frames = 0;
    let handle = 0;
    const attempt = () => {
      const el = scroller.querySelector<HTMLElement>(targetSelector(at));
      if (el) {
        el.scrollIntoView({ block: "center", behavior: "smooth" });
        flash(el);
        return;
      }
      if (frames++ < 30) handle = window.requestAnimationFrame(attempt);
    };
    handle = window.requestAnimationFrame(attempt);
    return () => window.cancelAnimationFrame(handle);
  }, [root, fileKey, at]);
}

/**
 * The spec card's body: the file rail and the open file. The open file is in
 * the URL (`?file=F2`, and `&at=F2.5` for a line in it), so a link, a Next up
 * item or the overview can land on it.
 */
export function SpecWorkspace({
  projectName,
  file,
  at,
}: {
  projectName: string;
  file?: string | undefined;
  at?: string | undefined;
}) {
  const { model, doc, lines, workspace } = useSpecWorkspace(projectName);
  const onOpen = useOpenSpecTarget(projectName);
  const scroller = useRef<HTMLDivElement>(null);
  const open = model.data ? openFile(model.data, file) : null;
  useReveal(scroller, open?.key ?? "", workspace ? at : undefined);

  // Opening the spec is seeing what design feedback changed in it: the Spec
  // tab's dot goes. The changed lines stay marked until their comments are resolved.
  const seeChanges = useSeeDesignChanges(projectName);
  const unseen = model.data ? specTabDot(model.data.design.specChanges) : false;
  const { mutate: markSeen, isPending: marking, isError: markFailed } = seeChanges;
  useEffect(() => {
    if (unseen && !marking && !markFailed) markSeen();
  }, [unseen, marking, markFailed, markSeen]);

  if (model.isError) {
    return (
      <Box sx={{ p: 3.5 }}>
        <EmptyState
          title="Couldn't open the spec"
          description={model.error?.message ?? "Something went wrong."}
          action={
            <Button variant="outlined" onClick={() => void model.refetch()}>
              Try again
            </Button>
          }
        />
      </Box>
    );
  }

  const data = model.data;
  const ready = data && doc && lines && workspace && open;
  return (
    <Box sx={{ display: "flex", height: "100%", minHeight: 0 }}>
      {ready && (
        <FileRail
          projectName={projectName}
          features={workspace.features}
          documents={data.documents}
          current={open.key}
        />
      )}
      <Box
        ref={scroller}
        sx={{
          flex: 1,
          minWidth: 0,
          overflowY: "auto",
          px: 3.5,
          pt: 3,
          pb: 11,
          [PHONE]: { px: 2, pb: 17.5 },
        }}
      >
        {ready ? (
          <>
            <FilePicker
              projectName={projectName}
              features={workspace.features}
              documents={data.documents}
              current={open.key}
            />
            <SpecFilePane
              projectName={projectName}
              model={data}
              workspace={workspace}
              doc={doc}
              lines={lines}
              file={open}
              onOpen={onOpen}
            />
          </>
        ) : (
          <Box aria-busy sx={{ maxWidth: "72ch" }}>
            <Skeleton width={180} />
            <Skeleton variant="text" sx={{ fontSize: "1.5rem" }} width={260} />
            <Skeleton variant="rounded" height={160} sx={{ mt: 2 }} />
          </Box>
        )}
      </Box>
    </Box>
  );
}
