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

import { useState, type DragEvent } from "react";
import {
  Alert,
  Box,
  Button,
  IconButton,
  InputBase,
  Stack,
  Tooltip,
} from "@wso2/oxygen-ui";
import { Paperclip, Send } from "@wso2/oxygen-ui-icons-react";
import { AttachmentCardStrip } from "../../../components/AttachmentCard";
import {
  MAX_REFERENCE_FILES,
  REFERENCE_ACCEPT,
  screenReferenceFiles,
  type RejectedFile,
} from "../lib/referenceFiles";

/**
 * The create view's prompt box (#383): one composer holding the typed idea and
 * its attached reference documents, replacing the old textarea + separate
 * dashed drop zone. The whole box is the drop target, so there is no second
 * affordance to find.
 *
 * Screening (type, size, count, duplicate path) happens on selection; each
 * rejection surfaces as its own notice and clears on the next selection —
 * never a silent drop.
 */
export function PromptComposer({
  prompt,
  onPromptChange,
  files,
  onFilesChange,
  onSubmit,
}: {
  prompt: string;
  onPromptChange: (value: string) => void;
  files: File[];
  onFilesChange: (files: File[]) => void;
  onSubmit: () => void;
}) {
  const [dragOver, setDragOver] = useState(false);
  const [rejected, setRejected] = useState<RejectedFile[]>([]);

  const addFiles = (incoming: FileList | null) => {
    if (!incoming || incoming.length === 0) return;
    const screening = screenReferenceFiles(files, Array.from(incoming));
    setRejected(screening.rejected);
    if (screening.accepted.length > 0) {
      onFilesChange([...files, ...screening.accepted]);
    }
  };

  const drop = (e: DragEvent) => {
    e.preventDefault();
    setDragOver(false);
    addFiles(e.dataTransfer.files);
  };

  return (
    <Stack spacing={1}>
      <Box
        onDragOver={(e) => {
          e.preventDefault();
          setDragOver(true);
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={drop}
        sx={{
          p: 1.5,
          borderRadius: 2,
          border: "1px solid",
          borderColor: dragOver ? "primary.main" : "divider",
          bgcolor: dragOver ? "action.hover" : "background.paper",
          "&:focus-within": { borderColor: "primary.main" },
        }}
      >
        <AttachmentCardStrip
          names={files.map((f) => f.name)}
          onRemove={(name) => onFilesChange(files.filter((f) => f.name !== name))}
        />
        <InputBase
          value={prompt}
          onChange={(e) => onPromptChange(e.target.value)}
          placeholder="e.g. A service desk where employees raise IT requests and the team tracks them through to resolution"
          multiline
          minRows={3}
          autoFocus
          fullWidth
          sx={{ px: 0.5, alignItems: "flex-start" }}
        />
        <Stack
          direction="row"
          spacing={1}
          sx={{ alignItems: "center", justifyContent: "space-between", mt: 1 }}
        >
          <Tooltip
            title={
              /* The hint the old drop zone spelled out in a paragraph. It has
                 to stay reachable somewhere, and the attach control is where
                 someone with reference material looks first.

                 No extension list: it is 16 entries now, which turned a hint
                 into a wall of text nobody reads. The file picker already
                 filters by `accept`, and picking an unsupported file answers
                 with a rejection notice naming the accepted set — so the list
                 is available exactly where it matters, at the point of
                 failure, rather than in front of everyone every time. */
              `Attach reference documents — a PRD, notes, an API spec. Agents read them when deriving your requirements. Up to ${MAX_REFERENCE_FILES} files, 5 MB each.`
            }
          >
            <IconButton component="label" size="small" aria-label="Attach reference documents">
              <Paperclip size={18} />
              <input
                type="file"
                accept={REFERENCE_ACCEPT}
                multiple
                hidden
                onChange={(e) => {
                  addFiles(e.target.files);
                  // Same file re-selected after a remove must re-fire onChange.
                  e.target.value = "";
                }}
              />
            </IconButton>
          </Tooltip>
          <Button
            variant="contained"
            // Icon on both ends of the toolbar row: the paperclip opens the
            // picker, this sends. Same shape as the Back button's startIcon
            // elsewhere in the flow.
            endIcon={<Send size={16} />}
            disabled={!prompt.trim()}
            onClick={onSubmit}
          >
            Start
          </Button>
        </Stack>
      </Box>
      {/* Keyed and dismissed by position, not by name: one selection can reject
          two files under the same name (a duplicate, or two oversized copies),
          and name identity would collapse them into one notice and then close
          both at once. */}
      {rejected.map(({ name, reason }, index) => (
        <Alert
          key={`${index}-${name}`}
          severity="warning"
          onClose={() =>
            setRejected((prev) => prev.filter((_, i) => i !== index))
          }
        >
          {/* The reason renders verbatim: lower-casing it turned "Larger than
              5 MB" into "5 mb" and mangled the casing of the user's own file
              name in the collision reason. */}
          <strong>{name}</strong> was not attached — {reason}.
        </Alert>
      ))}
    </Stack>
  );
}
