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

import { useEffect, useState, type MouseEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Box, Paper, Popper } from "@wso2/oxygen-ui";
import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import Collaboration from "@tiptap/extension-collaboration";
import type * as Y from "yjs";
import { AgentInsertion } from "@aep/collab-doc";
import { proseSx } from "../../../components/proseSx";
import { fileKeyForHref, type MarkdownFile } from "../model/files";
import { idTarget, resolveId, type IdIndex } from "../model/ids";
import type { SpecTarget } from "../useSpecWorkspace";
import { IdPreview } from "../components/IdPreview";
import { quietLinkSx } from "../components/QuietId";
import { soft } from "../components/Tag";
import { actOnAssumed, AssumedLines, leaveAssumedEdit } from "./assumedLines";
import { ROWS_SLOT, SpecLines, setSpecLinesOptions, type AssumedAction } from "./specLinesPlugin";

/** A pill button inside the document, in one palette colour. */
function pillSx(tone: "warning") {
  return {
    fontFamily: "inherit",
    fontStyle: "normal",
    fontWeight: 600,
    fontSize: "0.6875rem",
    lineHeight: 1.6,
    px: 1,
    borderRadius: 999,
    border: 1,
    borderColor: `${tone}.main`,
    color: `${tone}.main`,
    bgcolor: "background.paper",
    cursor: "pointer",
    "&:hover": { bgcolor: soft(tone) },
    "&:focus-visible": { outline: 2, outlineColor: `${tone}.main`, outlineOffset: 1 },
  } as const;
}

const ptagSx = { fontSize: "0.6875rem", fontWeight: 600, color: "info.main", whiteSpace: "nowrap", ml: 1 } as const;

// How a spec document reads: the app's prose (proseSx), and the spec's own
// line decorations on top. The decorations (specLinesPlugin.ts) only name
// things; the look is here, from the theme.
const documentSx = {
  ...proseSx,
  "& .aep-line": { borderRadius: 1.5 },
  "& .aep-line--assumed": { bgcolor: soft("warning", 0.14) },
  // A blocking question and its options: the warning's wash, a rule down the left.
  "& .aep-line--blocking": { bgcolor: soft("warning", 0.14), boxShadow: "inset 2px 0 0 var(--oxygen-palette-warning-main)" },
  // What the agent just wrote: a green wash that fades as it lands (FRESH_MS).
  "@keyframes aep-fresh": { from: { backgroundColor: soft("success", 0.28) }, to: { backgroundColor: "transparent" } },
  "& .aep-fresh": { borderRadius: 0.5, animation: "aep-fresh 5s ease-out forwards" },
  // Changed by a design comment: the accent's wash, a rule down the left.
  "& .aep-line--design": { bgcolor: soft("primary"), boxShadow: "inset 2px 0 0 var(--oxygen-palette-primary-main)" },
  "& .aep-dtag": { ...ptagSx, color: "primary.main", display: "inline-block" },
  "& .aep-asm": { display: "inline-flex", flexWrap: "wrap", gap: 0.5, ml: 0.75, verticalAlign: "baseline", "& button": pillSx("warning") },
  "& .aep-line--by-you": { textDecoration: "underline dotted var(--oxygen-palette-success-main)", textUnderlineOffset: "4px" },
  "& .aep-you": { fontSize: "0.6875rem", color: "success.main", whiteSpace: "nowrap", ml: 1, textDecoration: "none", display: "inline-block" },
  "& .aep-sid": { fontFamily: "monospace", fontSize: "0.75rem", color: "text.secondary" },
  "& .aep-was": { fontSize: "0.75rem", color: "text.secondary" },
  // A Needs or Applies to clause: quiet words, not a tag, as the IDs in it are links.
  "& .aep-clause": { fontSize: "0.75rem", color: "text.secondary" },
  "& .aep-source": {
    fontFamily: "monospace",
    fontSize: "0.6875rem",
    color: "text.secondary",
    border: 1,
    borderColor: "divider",
    borderRadius: 1,
    px: 0.5,
    whiteSpace: "nowrap",
  },
  "& .aep-assumed, & .aep-blocking": { fontSize: "0.75rem", color: "warning.main" },
  "& .aep-ref": quietLinkSx,
  "& .aep-hidden": { display: "none" },
  "& .aep-fog": {
    listStyle: "none",
    pl: 0,
    display: "flex",
    flexDirection: "column",
    gap: 0.75,
    "& > li": { border: "1px dashed", borderColor: "divider", borderRadius: 2, px: 1.5, py: 1, color: "text.secondary" },
    "& > li + li": { mt: 0 },
  },
  // The feature rows are the app's own component, not document text: none of
  // the document's list or link styling reaches them.
  "& .ProseMirror .aep-rows": { my: 1 },
  "& .ProseMirror .aep-rows ul": { pl: 0, my: 0 },
  "& .ProseMirror .aep-rows li + li": { mt: 0 },
  "& .ProseMirror .aep-rows a": { color: "inherit", textDecoration: "none" },
} as const;

/**
 * One markdown file of the spec, editable, bound to its fragment in the spec
 * doc: every keystroke is a Yjs update, so the edit is in the doc the rest of
 * the workspace reads (and, once the room is wired, what the committer
 * writes). Yjs owns history, so StarterKit's undo is off; the editor lasts
 * as long as the file is open, so that history does too.
 *
 * IDs in the text are quiet links: hover shows the line, click opens it. A
 * link to another spec file opens that file.
 */
export function SpecEditor({
  fragment,
  path,
  files,
  index,
  onOpen,
  featureRows,
  hideFog = false,
  designChanged = null,
}: {
  fragment: Y.XmlFragment;
  /** The file's room path, which its relative links resolve against. */
  path: string;
  files: MarkdownFile[];
  index: IdIndex;
  onOpen: (target: SpecTarget) => void;
  /** Drawn in place of the Features list (the product page). */
  featureRows?: ReactNode;
  hideFog?: boolean;
  designChanged?: ReadonlyMap<string, string> | null;
}) {
  const withRows = featureRows !== undefined;

  const editor = useEditor(
    {
      extensions: [
        StarterKit.configure({ undoRedo: false, link: { openOnClick: false } }),
        AgentInsertion,
        Collaboration.configure({ fragment }),
        SpecLines.configure({ featureRows: withRows, hideFog, designChanged }),
        AssumedLines,
      ],
      onBlur: ({ editor: blurred }) => leaveAssumedEdit(blurred.view),
    },
    // Only another file makes another editor. The options below change how
    // the document is drawn and go to the open editor, which keeps its undo
    // history and the cursor through them.
    [fragment],
  );
  useEffect(() => {
    if (editor) setSpecLinesOptions(editor.view, { featureRows: withRows, hideFog, designChanged });
  }, [editor, withRows, hideFog, designChanged]);

  // The rows draw into the slot the decorations leave in the document. The
  // slot is ProseMirror's DOM, so it is looked up after every transaction,
  // not held: a redraw may replace it, and the rows follow.
  const [rowsSlot, setRowsSlot] = useState<HTMLElement | null>(null);
  useEffect(() => {
    if (!editor || !withRows) return;
    const find = () => setRowsSlot(editor.view.dom.querySelector<HTMLElement>(`:scope > .${ROWS_SLOT}`));
    find();
    editor.on("create", find);
    editor.on("transaction", find);
    return () => {
      editor.off("create", find);
      editor.off("transaction", find);
    };
  }, [editor, withRows]);

  const [hover, setHover] = useState<{ el: HTMLElement; id: string } | null>(null);

  const onMouseOver = (e: MouseEvent) => {
    const el = (e.target as HTMLElement).closest<HTMLElement>("[data-spec-id]");
    if (el === hover?.el) return;
    setHover(el?.dataset.specId ? { el, id: el.dataset.specId } : null);
  };

  const onClick = (e: MouseEvent) => {
    const target = e.target as HTMLElement;
    const button = target.closest<HTMLElement>("[data-assumed]");
    if (button && editor) {
      actOnAssumed(editor.view, editor.view.posAtDOM(button, 0), button.dataset.assumed as AssumedAction);
      return;
    }
    // A drag that selected text is an edit gesture, not a click on a link.
    if (window.getSelection()?.isCollapsed === false) return;
    const ref = target.closest<HTMLElement>("[data-spec-id]");
    if (ref?.dataset.specId) {
      const resolved = resolveId(index, ref.dataset.specId);
      if (resolved) onOpen(idTarget(resolved.entry));
      return;
    }
    const href = target.closest("a")?.getAttribute("href");
    const key = href ? fileKeyForHref(files, path, href) : null;
    if (key) {
      e.preventDefault();
      onOpen({ file: key });
    }
  };

  return (
    <Box sx={documentSx} onMouseOver={onMouseOver} onMouseLeave={() => setHover(null)} onClick={onClick}>
      <EditorContent editor={editor} aria-label="Document" />
      {withRows && rowsSlot && createPortal(featureRows, rowsSlot)}
      <Popper
        open={hover !== null && hover.el.isConnected}
        anchorEl={hover?.el ?? null}
        placement="top-start"
        sx={{ zIndex: (t) => t.zIndex.tooltip, pointerEvents: "none" }}
      >
        <Paper variant="outlined" sx={{ px: 1.5, py: 1, mb: 0.75, boxShadow: "var(--aep-shell-card-shadow)" }}>
          {hover && <IdPreview id={hover.id} resolved={resolveId(index, hover.id)} />}
        </Paper>
      </Popper>
    </Box>
  );
}
