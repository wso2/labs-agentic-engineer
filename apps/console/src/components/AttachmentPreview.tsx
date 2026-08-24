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

import { useEffect, useState } from "react";
import { Box, IconButton, Stack, Tooltip, Typography } from "@wso2/oxygen-ui";
import { File as FileIcon, X } from "@wso2/oxygen-ui-icons-react";
import { isImageAttachment } from "../lib/attachments";

/**
 * Compact attachments, rendered INSIDE the chat composer (#428).
 *
 * Deliberately not the create view's `AttachmentCard`. That card is a 132x108
 * tile carrying a name and a type badge, and it earns that size because the
 * create view's composer IS the page — there is room, and the documents are the
 * point of the screen. The chat composer is a strip at the bottom of a side
 * panel, and its attachments are an aside to a sentence, so they get the space
 * an aside deserves.
 *
 * The rule for what each one shows:
 *
 *  - **An image shows itself.** A thumbnail answers "did I attach the right
 *    screenshot?" instantly, which is the only question the user has; its file
 *    name (`Screenshot 2026-08-20 at 11.42.13.png`) answers nothing and costs
 *    the width of the whole strip.
 *  - **Anything else shows its name**, because there is nothing to look at. A
 *    PDF is read natively by the model but cannot be drawn by an `<img>`, so it
 *    lands here rather than with the images.
 *
 * The name is never LOST on a thumbnail — it stays as the `alt` text and the
 * tooltip, so screen readers get it unconditionally and a hover recovers it.
 */
const SIZE = 30;

/** An object URL for `file`, revoked when the file changes or unmounts. */
function useObjectUrl(file: File | null): string | null {
  const [url, setUrl] = useState<string | null>(null);
  useEffect(() => {
    if (!file) {
      setUrl(null);
      return;
    }
    const created = URL.createObjectURL(file);
    setUrl(created);
    // Not optional: each un-revoked URL pins its File in memory for the life of
    // the document, and this composer churns them on every add and remove.
    return () => URL.revokeObjectURL(created);
  }, [file]);
  return url;
}

function RemoveButton({
  name,
  onRemove,
  disabled,
}: {
  name: string;
  onRemove: () => void;
  disabled: boolean;
}) {
  return (
    <Tooltip title={`Remove ${name}`}>
      <IconButton
        className="attachment-preview-remove"
        size="small"
        aria-label={`Remove ${name}`}
        onClick={onRemove}
        disabled={disabled}
        sx={{
          position: "absolute",
          top: 0,
          right: 0,
          transform: "translate(35%, -35%)",
          p: 0.125,
          opacity: 0,
          pointerEvents: "none",
          transition: "opacity 120ms",
          bgcolor: "background.paper",
          border: "1px solid",
          borderColor: "divider",
          "&:hover": { bgcolor: "action.hover" },
          // Keyboard users never hover, so focus has to reveal it too — which is
          // why this is opacity-toggled rather than mounted on hover: an
          // unmounted control cannot be tabbed to.
          "&:focus-visible": { opacity: 1, pointerEvents: "auto" },
        }}
      >
        <X size={10} />
      </IconButton>
    </Tooltip>
  );
}

export function AttachmentPreview({
  file,
  onRemove,
  disabled = false,
}: {
  file: File;
  onRemove: () => void;
  disabled?: boolean | undefined;
}) {
  const isImage = isImageAttachment(file.name);
  const url = useObjectUrl(isImage ? file : null);

  const shell = {
    position: "relative" as const,
    flexShrink: 0,
    height: SIZE,
    borderRadius: 1,
    border: "1px solid",
    borderColor: "divider",
    bgcolor: "background.paper",
    overflow: "visible" as const,
    "&:hover .attachment-preview-remove, &:focus-within .attachment-preview-remove": {
      opacity: 1,
      pointerEvents: "auto",
    },
  };

  if (isImage) {
    return (
      <Box data-testid="attachment-preview-image" sx={{ ...shell, width: SIZE }}>
        <Tooltip title={file.name}>
          <Box
            component="img"
            src={url ?? undefined}
            // The name survives here even though it is not drawn: a thumbnail
            // with no accessible name is invisible to a screen reader.
            alt={file.name}
            sx={{
              width: "100%",
              height: "100%",
              objectFit: "cover",
              borderRadius: 1,
              display: "block",
            }}
          />
        </Tooltip>
        <RemoveButton name={file.name} onRemove={onRemove} disabled={disabled} />
      </Box>
    );
  }

  return (
    <Box
      data-testid="attachment-preview-named"
      sx={{ ...shell, display: "flex", alignItems: "center", gap: 0.5, px: 0.75, maxWidth: 150 }}
    >
      <FileIcon size={11} style={{ flexShrink: 0, opacity: 0.7 }} />
      <Typography
        variant="caption"
        title={file.name}
        sx={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}
      >
        {file.name}
      </Typography>
      <RemoveButton name={file.name} onRemove={onRemove} disabled={disabled} />
    </Box>
  );
}

/**
 * The attachment row inside the composer. Scrolls rather than wraps: the
 * composer must not grow taller as files are added, or the send control walks
 * down the page.
 */
export function AttachmentPreviewStrip({
  files,
  onRemove,
  disabled = false,
}: {
  files: File[];
  onRemove: (name: string) => void;
  disabled?: boolean | undefined;
}) {
  if (files.length === 0) return null;
  return (
    <Stack
      direction="row"
      spacing={0.75}
      data-testid="attachment-strip"
      sx={{
        mb: 0.75,
        overflowX: "auto",
        // Room for the remove button, which is translated OUTSIDE each tile's
        // top-right corner. `overflow-x: auto` makes this a scroll container,
        // and a scroll container clips on BOTH axes — setting one axis to a
        // non-visible value computes the other to `auto` rather than leaving it
        // `visible`. So the button was cut off against this row's top edge. The
        // padding has to EXCEED the button's translated overhang, not merely be
        // non-zero.
        pt: 0.75,
        pr: 0.75,
        // The scrollbar in a 30px-tall row eats half the tile; the row is short
        // enough to drag-scroll or shift-wheel.
        scrollbarWidth: "none",
        "&::-webkit-scrollbar": { display: "none" },
      }}
    >
      {files.map((file) => (
        <AttachmentPreview
          key={file.name}
          file={file}
          disabled={disabled}
          onRemove={() => onRemove(file.name)}
        />
      ))}
    </Stack>
  );
}
