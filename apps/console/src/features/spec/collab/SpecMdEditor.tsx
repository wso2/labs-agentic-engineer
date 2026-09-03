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

import { useEffect, useRef, useState } from "react";
import { alpha, Box, Button, Paper, Stack, Typography } from "@wso2/oxygen-ui";
import { Check, Sparkles, X } from "@wso2/oxygen-ui-icons-react";
import { EditorContent, useEditor } from "@tiptap/react";
import { BubbleMenu } from "@tiptap/react/menus";
import StarterKit from "@tiptap/starter-kit";
import { AgentStreamingContext, MermaidCodeBlock } from "./MermaidCodeBlock";
import { Markdown } from "@tiptap/markdown";
import Collaboration from "@tiptap/extension-collaboration";
import CollaborationCaret from "@tiptap/extension-collaboration-caret";
import type { Transaction } from "@tiptap/pm/state";
import type { HocuspocusProvider } from "@hocuspocus/provider";
import type * as Y from "yjs";
import { AgentInsertion } from "@aep/collab-doc";
import {
  acceptAll,
  acceptRange,
  agentRanges,
  rangeAt,
  rejectAll,
  rejectRange,
} from "./agentReview";
import { SpecMdToolbar } from "./SpecMdToolbar";
import { PrdLenses, refreshPrdLenses, type PrdLensBinding } from "./prdLensPlugin";
import { SpecLinks, refreshSpecLinks, type SpecLinkBinding } from "./specLinkPlugin";
import { SpecAimMenu, type AimRequest, type SpecAimBinding } from "./SpecAimMenu";
import { AimHighlight, AIM_SELECTED_CLASS } from "./aimHighlightPlugin";
import { UndoScrollGuard } from "./undoScrollGuard";

/**
 * Where a transaction's change ends, as a position in the document it
 * produced. Null for a transaction that changed nothing.
 *
 * Found by DIFFING the documents before and after, not by reading the step
 * map: y-prosemirror applies a remote change by replacing the whole document
 * content in one step, so its map says every change touched everything — and
 * "everything" ends at the tail, which is exactly the wrong answer for a
 * paragraph rewritten in the middle. The diff walks node by node and stops at
 * the first and last differing child, so it costs nothing on a document this
 * size.
 */
function changeEnd(transaction: Transaction): number | null {
  const before = transaction.before.content;
  const after = transaction.doc.content;
  const start = before.findDiffStart(after);
  if (start === null) return null;
  const end = before.findDiffEnd(after);
  return end ? Math.max(start, end.b) : start;
}

// Collaborative WYSIWYG editor for markdown spec files (#86 phase 6).
// The shared source of truth is the file's Y.XmlFragment (seeded server-side
// from the repo markdown; serialized back to markdown by the committer,
// phase 3). Yjs owns history, so StarterKit's undo/redo is disabled.
// Agent insertions arrive marked (agentInsertion) — highlighted for review;
// accept keeps the text, reject deletes it; the committer holds marked files
// out of interim flushes until reviewed.
export function SpecMdEditor({
  fragment,
  provider,
  self,
  agentStreaming,
  lenses,
  links,
  aim,
  revealUnsettled = 0,
}: {
  fragment: Y.XmlFragment;
  provider: HocuspocusProvider;
  self: { name: string; color: string };
  // True while an agent peer is writing into the room. Drives tail-following:
  // the document area follows the streamed text only while this is set.
  agentStreaming: boolean;
  /** The PRD's code lenses (#579) — passed for that file only, absent elsewhere. */
  lenses?: PrdLensBinding | undefined;
  /** Cross-references to sibling spec documents; every markdown file carries them. */
  links?: SpecLinkBinding | undefined;
  /**
   * Aiming the agent at a selection (#666). Every markdown file carries it —
   * unlike the lenses, which are the PRD's own: a selection is a thing any
   * document has, so the affordance cannot be one document's privilege.
   */
  aim?: SpecAimBinding | undefined;
  /**
   * Scroll the first unsettled line into view (#666). A counter, not a flag:
   * every increment is one request, so "Review them first" pressed twice
   * scrolls twice. Zero or absent asks for nothing.
   */
  revealUnsettled?: number | undefined;
}) {
  // The extension list is built once per (fragment, provider) — a file swap
  // remounts this component under a new key — so the plugin reaches the CURRENT
  // binding through a ref rather than the closure it was configured with.
  const lensRef = useRef(lenses);
  const linkRef = useRef(links);
  // A Discuss lens (#652) opens the aim surface on its block. The setter is
  // stable, so the extension — built once — can hold it directly.
  const [aimRequest, setAimRequest] = useState<AimRequest | null>(null);
  useEffect(() => {
    lensRef.current = lenses;
    linkRef.current = links;
  });
  const editor = useEditor(
    {
      extensions: [
        // codeBlock is disabled in StarterKit and re-added mermaid-aware:
        // ```mermaid blocks render as live diagrams, click to edit source.
        StarterKit.configure({ undoRedo: false, codeBlock: false }),
        MermaidCodeBlock,
        Markdown,
        AgentInsertion,
        Collaboration.configure({ fragment }),
        CollaborationCaret.configure({ provider, user: self }),
        SpecLinks.configure({ binding: () => linkRef.current }),
        AimHighlight,
        UndoScrollGuard,
        ...(lenses
          ? [
              PrdLenses.configure({
                run: (command: string) => lensRef.current?.run(command),
                isBusy: () => Boolean(lensRef.current?.busyReason),
                busyReason: () => lensRef.current?.busyReason ?? "",
                discuss: (from: number, to: number) =>
                  setAimRequest((prev) => ({
                    kind: "block",
                    from,
                    to,
                    intent: "discuss",
                    seq: (prev?.seq ?? 0) + 1,
                  })),
                compose: (command, prompt, at) =>
                  setAimRequest((prev) => ({
                    kind: "command",
                    command,
                    ...prompt,
                    at,
                    seq: (prev?.seq ?? 0) + 1,
                  })),
              }),
            ]
          : []),
      ],
    },
    [fragment, provider],
  );

  // Whether a lens is clickable is console state, not document state, so a
  // change to it has to ask ProseMirror to re-render the widgets. The ref
  // effect above is declared first, so it has already landed by the time this
  // rebuild reads the binding.
  const busyReason = lenses?.busyReason ?? "";
  useEffect(() => {
    if (editor && lensRef.current) refreshPrdLenses(editor.view);
  }, [editor, busyReason]);

  // Same for the project's file list: which references resolve is console
  // state, and a feature doc the agent just wrote makes one more of them live.
  // Compared by value — the caller rebuilds the array on every render, and a
  // rebuild per keystroke would be a decoration sweep for nothing.
  const knownPaths = (links?.knownPaths ?? []).join("\n");
  useEffect(() => {
    if (editor && linkRef.current) refreshSpecLinks(editor.view);
  }, [editor, knownPaths]);

  // "Review them first" lands the user ON the first flagged line, not at the
  // top of a document they then have to search. The flags are decorations, so
  // they exist only once the editor has rendered — which, when the click also
  // switched files, is a few frames after this effect runs. Hence the retry:
  // a bounded number of frames, then give up quietly rather than scroll late
  // into something the user has moved on from.
  useEffect(() => {
    if (!editor || revealUnsettled === 0) return;
    let frames = 0;
    let handle = 0;
    const attempt = () => {
      const flag = editor.view.dom.querySelector<HTMLElement>(
        ".prd-flag--assumed, .prd-flag--question",
      );
      if (flag) {
        flag.scrollIntoView({ block: "center", behavior: "smooth" });
        flag.classList.add("prd-flag--revealed");
        window.setTimeout(() => flag.classList.remove("prd-flag--revealed"), 1600);
        return;
      }
      if (frames++ < 30) handle = window.requestAnimationFrame(attempt);
    };
    handle = window.requestAnimationFrame(attempt);
    return () => window.cancelAnimationFrame(handle);
  }, [editor, revealUnsettled]);

  // Pending-review count, refreshed on every document change.
  const [pending, setPending] = useState(0);
  useEffect(() => {
    if (!editor) return;
    const update = () => setPending(agentRanges(editor.state.doc).length);
    update();
    editor.on("transaction", update);
    return () => {
      editor.off("transaction", update);
    };
  }, [editor]);

  // Follow the EDIT while an agent writes into this doc — not the tail.
  //
  // The tail was the original rule (#206): generation appends, so the bottom
  // was where the writing was. An anchored Change (#666) rewrites a paragraph
  // in the middle, and a Discuss writes nothing at all — and both used to
  // throw the reader to the end of the document the moment the turn began,
  // away from the very passage they had just pointed at. So the view moves
  // only on a document change that ARRIVED FROM THE ROOM (a Yjs-origin
  // transaction — the user's own typing never qualifies), and it moves to
  // where that change landed. Generation still lands at the tail, and a change
  // inside the last block scrolls flush to the bottom, so the streaming feel
  // is unchanged.
  //
  // The user scrolling — a wheel or a touch on the scroller — breaks the follow
  // for the rest of the turn: they went to read something, and the agent
  // writing elsewhere is not a reason to take them from it. The next turn arms
  // it again.
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const followRef = useRef(false);
  useEffect(() => {
    followRef.current = agentStreaming;
  }, [agentStreaming]);
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const disarm = () => {
      followRef.current = false;
    };
    el.addEventListener("wheel", disarm, { passive: true });
    el.addEventListener("touchmove", disarm, { passive: true });
    return () => {
      el.removeEventListener("wheel", disarm);
      el.removeEventListener("touchmove", disarm);
    };
  }, []);
  useEffect(() => {
    if (!editor) return;
    // y-prosemirror marks a change that arrived from the Y.Doc by setting meta
    // under its sync plugin's key. That key is read off the editor's OWN plugin
    // list rather than imported: the Collaboration extension and this file can
    // resolve two instances of y-prosemirror (Vite pre-bundles the extension's
    // copy), and a PluginKey from the other instance never matches — the meta
    // reads as absent and the view never moves.
    const syncKey = editor.state.plugins
      .map((plugin) => (plugin as unknown as { key?: unknown }).key)
      .find((key): key is string => typeof key === "string" && key.startsWith("y-sync$"));
    const follow = ({ transaction }: { transaction: Transaction }) => {
      if (!followRef.current || !transaction.docChanged || !syncKey) return;
      const origin = transaction.getMeta(syncKey) as
        | { isChangeOrigin?: boolean; isUndoRedoOperation?: boolean }
        | undefined;
      if (origin?.isChangeOrigin !== true) return;
      // An undo is the USER's act, not the agent's writing — following it
      // mid-turn took the reader from the thing they had just put back.
      if (origin.isUndoRedoOperation === true) return;
      const el = scrollRef.current;
      if (!el) return;
      const pos = changeEnd(transaction);
      if (pos === null) return;
      const doc = editor.state.doc;
      const last = doc.lastChild;
      if (last && pos >= doc.content.size - last.nodeSize) {
        el.scrollTop = el.scrollHeight;
        return;
      }
      let coords: { top: number; bottom: number };
      try {
        coords = editor.view.coordsAtPos(Math.min(pos, doc.content.size));
      } catch {
        return;
      }
      const rect = el.getBoundingClientRect();
      const margin = 48;
      if (coords.bottom > rect.bottom - margin) el.scrollTop += coords.bottom - (rect.bottom - margin);
      else if (coords.top < rect.top + margin) el.scrollTop -= rect.top + margin - coords.top;
    };
    editor.on("transaction", follow);
    return () => {
      editor.off("transaction", follow);
    };
  }, [editor]);

  return (
    <Box
      sx={{
        // The editor is a single framed card that fills the pane: toolbar
        // (and review bar) docked as header rows, only the document area
        // below them scrolls — text can never pass the toolbar (#206 rework).
        flexGrow: 1,
        minHeight: 0,
        display: "flex",
        flexDirection: "column",
        border: 1,
        borderColor: "divider",
        borderRadius: 1,
        overflow: "hidden",
        // The aim chip and its box are positioned against this frame (#666).
        position: "relative",
        // Plain paper token, same as AgentChatPanel — it re-resolves per
        // color scheme (a JS-resolved gradient froze the light paper into
        // dark mode and rendered gray; #206 D10 revision, 2026-07-12).
        bgcolor: "background.paper",
        "&:focus-within": { borderColor: "primary.main" },
      }}
    >
      {editor && (
        <>
          <SpecMdToolbar editor={editor} />
          {pending > 0 && (
            <Stack
              direction="row"
              alignItems="center"
              spacing={1}
              sx={{
                flexShrink: 0,
                px: 1.5,
                py: 0.75,
                borderBottom: 1,
                borderColor: "divider",
                backgroundImage: (theme) =>
                  `linear-gradient(${alpha(theme.palette.primary.main, 0.08)}, ${alpha(
                    theme.palette.primary.main,
                    0.08,
                  )})`,
              }}
            >
              <Sparkles size={16} />
              <Typography variant="body2" sx={{ flexGrow: 1 }}>
                {pending} agent suggestion{pending === 1 ? "" : "s"} pending review
              </Typography>
              <Button
                size="small"
                color="inherit"
                startIcon={<X size={14} />}
                onClick={() => rejectAll(editor)}
              >
                Reject all
              </Button>
              <Button
                size="small"
                variant="contained"
                startIcon={<Check size={14} />}
                onClick={() => acceptAll(editor)}
              >
                Accept all
              </Button>
            </Stack>
          )}
        </>
      )}
      {editor && (
        <BubbleMenu
          editor={editor}
          shouldShow={({ state }) =>
            rangeAt(state.doc, state.selection.from) !== null
          }
        >
          <Paper elevation={3} sx={{ px: 1, py: 0.5 }}>
            <Stack direction="row" alignItems="center" spacing={0.5}>
              <Typography variant="caption" color="text.secondary" sx={{ pr: 0.5 }}>
                {rangeAt(editor.state.doc, editor.state.selection.from)?.agent ||
                  "agent"}
              </Typography>
              <Button
                size="small"
                startIcon={<Check size={14} />}
                onClick={() => {
                  const r = rangeAt(editor.state.doc, editor.state.selection.from);
                  if (r) acceptRange(editor, r);
                }}
              >
                Accept
              </Button>
              <Button
                size="small"
                color="error"
                startIcon={<X size={14} />}
                onClick={() => {
                  const r = rangeAt(editor.state.doc, editor.state.selection.from);
                  if (r) rejectRange(editor, r);
                }}
              >
                Reject
              </Button>
            </Stack>
          </Paper>
        </BubbleMenu>
      )}
      <Box
        ref={scrollRef}
        // Scrolling moves the text without changing the document, so the aim
        // surface has to re-measure off this element's own scroll events.
        data-aim-scroll=""
        sx={{
          flexGrow: 1,
          minHeight: 0,
          overflow: "auto",
          px: 2,
          py: 1,
          cursor: "text",
          "& .tiptap": { outline: "none" },
          "& .tiptap p": { my: 1 },
          "& .tiptap h1, & .tiptap h2, & .tiptap h3": { mt: 2, mb: 1 },
          "& .tiptap ul, & .tiptap ol": { pl: 3 },
          "& .tiptap table": { borderCollapse: "collapse" },
          "& .tiptap th, & .tiptap td": {
            border: 1,
            borderColor: "divider",
            px: 1,
            py: 0.5,
          },
          // The PRD's code lenses (#579). A SECTION lens is always on show —
          // it is how the command is discovered at all — while a LINE lens
          // appears on its entry's hover (or focus, for the keyboard), so a
          // twenty-story list carries three visible controls rather than
          // twenty-three.
          // The blocks an aimed turn would receive (#666). Softer than the
          // native selection and painted UNDER it, so while the editor still
          // has focus both read at once — the exact characters dragged, inside
          // the whole blocks the agent gets after the snap.
          [`& .${AIM_SELECTED_CLASS}`]: {
            backgroundColor: (theme: { palette: { primary: { main: string } } }) =>
              alpha(theme.palette.primary.main, 0.14),
            borderRadius: "3px",
            boxShadow: (theme: { palette: { primary: { main: string } } }) =>
              `0 0 0 2px ${alpha(theme.palette.primary.main, 0.14)}`,
          },
          "& .prd-lens": {
            all: "unset",
            cursor: "pointer",
            userSelect: "none",
            whiteSpace: "nowrap",
            verticalAlign: "baseline",
            display: "inline-block",
            ml: 1,
            px: 0.75,
            py: 0.125,
            // Same size as the remote-caret label below: the smallest thing
            // this editor already asks anyone to read.
            fontSize: "0.7rem",
            lineHeight: 1.6,
            fontStyle: "normal",
            fontWeight: 500,
            borderRadius: "999px",
            border: "1px solid",
            borderColor: "divider",
            color: "text.secondary",
            transition: "opacity 120ms, color 120ms, border-color 120ms",
          },
          "& .prd-lens:hover:not(:disabled)": {
            color: "primary.main",
            borderColor: "primary.main",
            bgcolor: (theme) => alpha(theme.palette.primary.main, 0.08),
          },
          "& .prd-lens:disabled": { cursor: "default", opacity: 0.4 },
          // AFTER the :disabled rule, and carrying its own :disabled variant:
          // `.prd-lens:disabled`'s 0.4 outweighs a bare `.prd-lens--line`, so
          // the moment a turn disabled the line lenses it REVEALED all of them
          // at once — a page of controls nobody hovered. Hidden is hidden,
          // inert or not; the hover still reveals, dimmed to the inert weight.
          "& .prd-lens--line, & .prd-lens--line:disabled": { opacity: 0 },
          "& .tiptap li:hover .prd-lens--line, & .tiptap p:hover .prd-lens--line, & .prd-lens--line:focus-visible":
            { opacity: 1 },
          "& .tiptap li:hover .prd-lens--line:disabled, & .tiptap p:hover .prd-lens--line:disabled":
            { opacity: 0.4 },
          // A reference to a sibling spec document reads as a link and acts
          // like one; an ordinary external link keeps the editor's default.
          "& .spec-link": {
            cursor: "pointer",
            color: "primary.main",
            textDecoration: "underline",
            textUnderlineOffset: "2px",
          },
          // The two kinds of unsettled read differently because they ARE
          // different: an assumption is one word of an otherwise-settled
          // decision, an open question is a whole entry nobody has answered.
          // A deferred one keeps its entry but drops to a resting grey.
          // The `*assumed*` marker reads as a PILL, not highlighted prose: the
          // word is a tag the agent put on the decision, not part of the
          // decision, and a wash over it read as text someone had selected.
          // Same shape as the lens pill beside it, in the warning colour, so
          // flag and control sit together as one vocabulary.
          "& .prd-flag--assumed": {
            display: "inline-block",
            fontStyle: "normal",
            fontSize: "0.7rem",
            fontWeight: 600,
            lineHeight: 1.6,
            letterSpacing: "0.02em",
            px: 0.75,
            ml: 0.5,
            verticalAlign: "baseline",
            borderRadius: "999px",
            border: "1px solid",
            borderColor: "warning.main",
            color: "warning.main",
            userSelect: "none",
          },
          // The line "Review them first" just landed on, for long enough to
          // catch the eye and no longer.
          "@keyframes prd-flag-reveal": {
            from: { boxShadow: (theme) => `0 0 0 6px ${alpha(theme.palette.warning.main, 0.45)}` },
            to: { boxShadow: "0 0 0 0 transparent" },
          },
          "& .prd-flag--revealed": { animation: "prd-flag-reveal 1.4s ease-out" },
          "& .tiptap .prd-flag--question, & .tiptap .prd-flag--deferred": {
            pl: 1,
            borderLeft: "3px solid",
          },
          "& .tiptap .prd-flag--question": { borderLeftColor: "info.main" },
          "& .tiptap .prd-flag--deferred": {
            borderLeftColor: "divider",
            color: "text.secondary",
          },
          // Unreviewed agent insertions (#86 ph6) — soft primary wash until
          // accepted (mark removed) or rejected (range deleted).
          "& .tiptap span.agent-insertion": {
            bgcolor: (theme) => alpha(theme.palette.primary.main, 0.12),
            borderBottom: "2px solid",
            borderBottomColor: (theme) => alpha(theme.palette.primary.main, 0.4),
            borderRadius: "2px",
          },
          // Remote carets (CollaborationCaret): colored bar + name label.
          "& .collaboration-carets__caret": {
            borderLeft: "1px solid",
            borderRight: "1px solid",
            mx: "-1px",
            position: "relative",
            wordBreak: "normal",
          },
          "& .collaboration-carets__label": {
            borderRadius: "3px 3px 3px 0",
            color: "common.white",
            fontSize: "0.7rem",
            fontWeight: 600,
            left: "-1px",
            lineHeight: 1.2,
            px: 0.5,
            position: "absolute",
            top: "-1.3em",
            userSelect: "none",
            whiteSpace: "nowrap",
          },
        }}
        onClick={() => editor?.commands.focus()}
      >
        <Box>
          {/* Node views render through portals into this tree, so the mermaid
              blocks read the streaming flag from here to hide their control. */}
          <AgentStreamingContext.Provider value={agentStreaming}>
            <EditorContent editor={editor} />
          </AgentStreamingContext.Provider>
        </Box>
      </Box>
      {/* Rendered last so it paints over the document, and OUTSIDE the
          scroller so it is positioned against the frame — the surface follows
          the text by re-measuring, not by scrolling with it. */}
      {editor && aim && (
        <SpecAimMenu
          editor={editor}
          aim={aim}
          request={aimRequest}
          runCommand={(line) => lensRef.current?.run(line)}
        />
      )}
    </Box>
  );
}
