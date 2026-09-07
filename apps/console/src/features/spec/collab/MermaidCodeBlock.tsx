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

// Mermaid rendering inside the spec markdown editor: a ```mermaid code block
// renders as its live diagram, and shows its source when you ask to edit it.
// The design skill emits domain-model.md (one erDiagram) and flows/<slug>.md
// (one sequenceDiagram each), so this is what makes the design read as diagrams.
//
// The Yjs-backed source text stays the single truth — the diagram is a pure
// view, so collaboration is untouched.
//
// This is a React node view rather than a hand-built DOM one for two reasons:
// the console's controls must come from Oxygen UI (apps/console/AGENTS.md), and
// Tiptap's React renderer supplies the `ignoreMutation` that keeps the injected
// SVG out of the document. Without that, writing the diagram into the DOM reads
// back as a user edit, and under Yjs the block duplicates on every render.

import CodeBlock from "@tiptap/extension-code-block";
import {
  NodeViewContent,
  NodeViewWrapper,
  ReactNodeViewRenderer,
  type ReactNodeViewProps,
} from "@tiptap/react";
import { Box, Button } from "@wso2/oxygen-ui";
import { Check, Pencil } from "@wso2/oxygen-ui-icons-react";
import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { renderMermaid } from "./mermaidRenderer";

const MERMAID = "mermaid";

/**
 * True while the agent is writing into the document. The control is hidden then:
 * the source is mid-write, so entering edit mode would put the caret in text the
 * agent is actively rewriting. Node views render through React portals into the
 * editor's tree, so this context reaches them.
 */
export const AgentStreamingContext = createContext(false);

function MermaidNodeView({ node, editor, getPos }: ReactNodeViewProps) {
  const isMermaid = (node.attrs.language ?? "") === MERMAID;
  const source = node.textContent.trim();
  const streaming = useContext(AgentStreamingContext);

  const [svg, setSvg] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  // The caret being inside the block is what reveals the source; the Edit and
  // Done controls are just two ways of moving it. Keeping selection as the only
  // state avoids a second source of truth that could disagree with it.
  const [caretInside, setCaretInside] = useState(false);

  useEffect(() => {
    const sync = () => {
      const pos = getPos();
      if (pos === undefined) {
        setCaretInside(false);
        return;
      }
      const { from, to } = editor.state.selection;
      setCaretInside(editor.isFocused && from >= pos && to <= pos + node.nodeSize);
    };
    sync();
    editor.on("selectionUpdate", sync);
    editor.on("focus", sync);
    editor.on("blur", sync);
    return () => {
      editor.off("selectionUpdate", sync);
      editor.off("focus", sync);
      editor.off("blur", sync);
    };
  }, [editor, getPos, node]);

  useEffect(() => {
    if (!isMermaid || source === "") {
      setSvg(null);
      setError(null);
      return;
    }
    let cancelled = false;
    renderMermaid(source)
      .then((markup) => {
        if (cancelled) return;
        setSvg(markup);
        setError(null);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setSvg(null);
        setError(err instanceof Error ? (err.message.split("\n")[0] ?? "parse error") : "parse error");
      });
    return () => {
      cancelled = true;
    };
  }, [isMermaid, source]);

  const enterEdit = useCallback(() => {
    const pos = getPos();
    if (pos !== undefined) editor.chain().focus(pos + 1).run();
  }, [editor, getPos]);

  // "Done" is defined as moving the caret past the node, so the selection rule
  // above puts the diagram back on its own — no separate "forced preview" state.
  const exitEdit = useCallback(() => {
    const pos = getPos();
    if (pos === undefined) return;
    const after = Math.min(pos + node.nodeSize, editor.state.doc.content.size);
    editor.chain().focus().setTextSelection(after).run();
  }, [editor, getPos, node]);

  // A non-mermaid fence is already showing its source and is directly editable,
  // so it keeps the stock rendering with no control.
  if (!isMermaid) {
    return (
      <NodeViewWrapper>
        <pre>
          <NodeViewContent<"code"> as="code" />
        </pre>
      </NodeViewWrapper>
    );
  }

  const showingSource = caretInside || source === "" || svg === null;

  return (
    <NodeViewWrapper>
      <Box sx={{ position: "relative" }}>
        {!streaming && (
          <Button
            size="small"
            variant="text"
            color="inherit"
            startIcon={showingSource ? <Check size={14} /> : <Pencil size={14} />}
            onClick={showingSource ? exitEdit : enterEdit}
            // contentEditable=false keeps ProseMirror from treating the control
            // as document content.
            contentEditable={false}
            sx={{
              position: "absolute",
              top: 0,
              right: 0,
              zIndex: 1,
              minWidth: 0,
              opacity: 0.55,
              transition: "opacity 120ms",
              "&:hover, &:focus-visible": { opacity: 1 },
            }}
          >
            {showingSource ? "Done" : "Edit"}
          </Button>
        )}

        <Box component="pre" sx={{ display: showingSource ? "block" : "none", m: 0 }}>
          <NodeViewContent<"code"> as="code" />
        </Box>

        {/* The diagram is presentation, never document content. */}
        <Box
          contentEditable={false}
          onDoubleClick={enterEdit}
          data-testid="mermaid-preview"
          sx={{ display: showingSource ? "none" : "block", overflowX: "auto" }}
          dangerouslySetInnerHTML={{ __html: svg ?? "" }}
        />

        {error !== null && (
          <Box contentEditable={false} sx={{ fontSize: "0.75rem", color: "warning.main", py: 0.25 }}>
            {`mermaid: ${error}`}
          </Box>
        )}
      </Box>
    </NodeViewWrapper>
  );
}

/**
 * The CodeBlock extension with a mermaid-aware node view. Non-mermaid languages
 * keep the stock code-block rendering.
 */
export const MermaidCodeBlock = CodeBlock.extend({
  addNodeView() {
    return ReactNodeViewRenderer(MermaidNodeView);
  },

  addKeyboardShortcuts() {
    return {
      // Escape leaves the block the same way Done does — the caret moves past
      // the node and the diagram comes back.
      Escape: ({ editor }) => {
        const { $from, empty } = editor.state.selection;
        if (!empty) return false;
        const parent = $from.parent;
        if (parent.type.name !== this.name) return false;
        if ((parent.attrs.language ?? "") !== MERMAID) return false;
        return editor.commands.setTextSelection($from.after());
      },
    };
  },
});
