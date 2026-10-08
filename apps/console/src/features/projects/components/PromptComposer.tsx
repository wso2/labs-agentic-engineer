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

import { useState, type DragEvent, type Ref } from "react";
import { Alert, Box, Button, IconButton, InputBase, Stack, Tooltip } from "@wso2/oxygen-ui";
import { Paperclip, X } from "@wso2/oxygen-ui-icons-react";
import {
  MAX_REFERENCE_FILES,
  REFERENCE_ACCEPT,
  referenceTypeLabel,
  screenReferenceFiles,
  type RejectedFile,
} from "../referenceFiles";

/** One attached document in the composer's footer: its name, its type, and a remove control. */
function Attachment({ name, onRemove }: { name: string; onRemove: () => void }) {
  return (
    <Box
      sx={{
        display: "inline-flex",
        alignItems: "center",
        gap: 0.5,
        maxWidth: "100%",
        minWidth: 0,
        pl: 1.25,
        pr: 0.25,
        py: 0.25,
        border: 1,
        borderColor: "divider",
        borderRadius: 2,
        fontSize: "0.78125rem",
      }}
    >
      <Box component="span" title={name} sx={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
        {name}
      </Box>
      <Box component="span" sx={{ color: "text.secondary", flexShrink: 0, "&::before": { content: '"· "' } }}>
        {referenceTypeLabel(name)}
      </Box>
      <IconButton size="small" aria-label={`Remove ${name}`} onClick={onRemove} sx={{ p: 0.5 }}>
        <X size={12} />
      </IconButton>
    </Box>
  );
}

/**
 * New project's prompt box: the typed idea and its attached reference
 * documents in one composer. The whole box is the drop target, so there is
 * no second affordance to find.
 *
 * Screening (type, size, count, duplicate path) happens on selection; each
 * rejection shows as its own notice and clears on the next selection, never a
 * silent drop.
 */
export function PromptComposer({
  prompt,
  onPromptChange,
  files,
  onFilesChange,
  onSubmit,
  inputRef,
}: {
  prompt: string;
  onPromptChange: (value: string) => void;
  files: File[];
  onFilesChange: (files: File[]) => void;
  onSubmit: () => void;
  /** The prompt's textarea, so an example card can hand focus back to it. */
  inputRef?: Ref<HTMLTextAreaElement>;
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
          px: 1.75,
          pt: 1.75,
          pb: 1.25,
          display: "flex",
          flexDirection: "column",
          gap: 1,
          borderRadius: 3.5,
          border: 1,
          borderColor: dragOver ? "primary.main" : "divider",
          bgcolor: dragOver ? "action.hover" : "background.paper",
          "&:focus-within": { borderColor: "primary.main" },
        }}
      >
        <InputBase
          value={prompt}
          onChange={(e) => onPromptChange(e.target.value)}
          placeholder="e.g. A service desk where employees raise IT requests and the team tracks them through to resolution"
          multiline
          minRows={4}
          autoFocus
          fullWidth
          {...(inputRef && { inputRef })}
          inputProps={{ "aria-label": "What do you want to build?" }}
          sx={{ alignItems: "flex-start", fontSize: "0.9375rem", lineHeight: 1.5 }}
        />
        <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 1, flexWrap: "wrap" }}>
          <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap", minWidth: 0 }}>
            {files.map((file) => (
              <Attachment
                key={file.name}
                name={file.name}
                onRemove={() => onFilesChange(files.filter((f) => f.name !== file.name))}
              />
            ))}
            {/* describeChild: the hint describes the button, and its own text
                stays its name. */}
            <Tooltip
              describeChild
              title={
                // No extension list: the picker already filters by `accept`,
                // and an unsupported file answers with a notice naming the
                // accepted set, at the point where it matters.
                `A PRD, notes, an API spec, a mockup. Agents read them when deriving your requirements. Up to ${MAX_REFERENCE_FILES} files, 5 MB each.`
              }
            >
              <Button component="label" size="small" variant="outlined" startIcon={<Paperclip size={14} />}>
                {files.length === 0 ? "Attach a document" : "Attach another"}
                <input
                  type="file"
                  accept={REFERENCE_ACCEPT}
                  multiple
                  hidden
                  onChange={(e) => {
                    addFiles(e.target.files);
                    // The same file re-selected after a remove must re-fire onChange.
                    e.target.value = "";
                  }}
                />
              </Button>
            </Tooltip>
          </Box>
          {/* Attaching is optional and documents alone are not a brief: the
              typed idea stays the anchor, so only a prompt enables this. */}
          <Button variant="contained" disabled={!prompt.trim()} onClick={onSubmit} sx={{ ml: "auto" }}>
            Continue
          </Button>
        </Box>
      </Box>
      {/* Keyed and dismissed by position, not by name: one selection can
          reject two files under the same name, and name identity would
          collapse them into one notice and then close both at once. */}
      {rejected.map(({ name, reason }, index) => (
        <Alert
          key={`${index}-${name}`}
          severity="warning"
          onClose={() => setRejected((prev) => prev.filter((_, i) => i !== index))}
        >
          <strong>{name}</strong> was not attached: {reason}.
        </Alert>
      ))}
    </Stack>
  );
}
