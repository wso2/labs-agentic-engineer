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

import { useEffect, useRef } from "react";
import { Box } from "@wso2/oxygen-ui";
import { EditorContent, useEditor } from "@tiptap/react";
import { proseSx } from "../../../components/proseSx";
import { skillEditorExtensions } from "./skillEditorExtensions";

// What a skill body holds beyond the spec's prose: deeper headings, code,
// quotes and tables (most of the platform's skills carry one).
const bodySx = {
  ...proseSx,
  "& .ProseMirror": { ...proseSx["& .ProseMirror"], minHeight: 240 },
  "& .ProseMirror h3": { fontSize: "0.875rem", fontWeight: 600, mt: 2, mb: 0.5 },
  "& .ProseMirror code": { fontFamily: "monospace", fontSize: "0.8125rem", bgcolor: "action.hover", borderRadius: 0.75, px: 0.5 },
  "& .ProseMirror pre": { bgcolor: "action.hover", borderRadius: 1.5, p: 1.5, overflowX: "auto", "& code": { bgcolor: "transparent", p: 0 } },
  "& .ProseMirror blockquote": { borderLeft: 3, borderColor: "divider", pl: 1.5, ml: 0, color: "text.secondary" },
  "& .ProseMirror hr": { border: 0, borderTop: 1, borderColor: "divider", my: 2 },
  "& .ProseMirror table": { borderCollapse: "collapse", my: 1, width: "100%", tableLayout: "fixed" },
  "& .ProseMirror th, & .ProseMirror td": { border: 1, borderColor: "divider", px: 1, py: 0.5, verticalAlign: "top", "& p": { my: 0 } },
  "& .ProseMirror th": { fontWeight: 600, bgcolor: "action.hover", textAlign: "start" },
} as const;

/**
 * A skill's markdown body, edited as a document: the Spec editor's look and
 * Tiptap, with no room behind it. It edits the Skill card's draft and nothing
 * else: `onChange` hands the body as the editor now writes it, or null while
 * it reads as it did when opened (an edit undone is no edit). The body the
 * card saves is the editor's markdown only once it changed, so a skill whose
 * body nobody touched is written back byte for byte.
 *
 * A new `docKey` is a new document (another skill, or the one just saved).
 */
export function SkillBodyEditor({
  markdown,
  editable,
  onChange,
  docKey,
}: {
  markdown: string;
  editable: boolean;
  onChange: (body: string | null) => void;
  docKey: string;
}) {
  const baseline = useRef<string | null>(null);
  const report = useRef(onChange);
  report.current = onChange;

  const editor = useEditor(
    {
      extensions: skillEditorExtensions,
      content: markdown,
      contentType: "markdown",
      editable,
      editorProps: {
        handleDOMEvents: {
          // ProseMirror marks every Escape handled, which the card reads as a
          // menu above it having taken the key. Nothing here binds Escape, so
          // it goes on to close the card; an IME composition still keeps it.
          keydown: (_view, event) => event.key === "Escape" && !event.isComposing,
        },
      },
      onUpdate: ({ editor: changed }) => {
        // The document as opened, written the way the editor writes it: the
        // editor reports its own loading as an update, before it is created.
        baseline.current ??= changed.markdown ? changed.markdown.serialize(changed.markdown.parse(markdown)) : markdown;
        const now = changed.getMarkdown();
        report.current(now === baseline.current ? null : now);
      },
    },
    // Only another document makes another editor; the rest goes to the open one.
    [docKey],
  );

  useEffect(() => {
    editor?.setEditable(editable);
  }, [editor, editable]);

  return (
    <Box sx={bodySx}>
      <EditorContent editor={editor} aria-label="The skill's instructions" />
    </Box>
  );
}
