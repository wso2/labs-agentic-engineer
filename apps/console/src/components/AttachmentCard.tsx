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

import { Box, IconButton, Stack, Tooltip, Typography } from "@wso2/oxygen-ui";
import { X } from "@wso2/oxygen-ui-icons-react";
import { attachmentTypeLabel } from "../lib/attachments";

/**
 * One attached file, as a card inside the CREATE VIEW's composer (#383).
 *
 * Sole consumer: `PromptComposer`. The chat composer deliberately does NOT use
 * this — see `AttachmentPreview`, which is a 30px row instead. The two diverge
 * because their composers do: the create view's composer IS the page, so a
 * 132x108 tile is affordable and the documents are the point of the screen,
 * while the chat's is a strip at the bottom of a side panel and its attachments
 * are an aside to a sentence.
 *
 * The name is the whole card — no size, because an oversized file never becomes
 * a card (it becomes a rejection notice), so the only thing left worth saying is
 * which kind of document this is.
 */
const CARD_WIDTH = 132;
const CARD_HEIGHT = 108;

export function AttachmentCard({
  name,
  onRemove,
  disabled,
}: {
  name: string;
  /** Omit to render a read-only card (no remove control, none in the tab order). */
  onRemove?: () => void;
  /** Composer is locked (a turn is running): the control stays visible but inert. */
  disabled?: boolean | undefined;
}) {
  return (
    <Box
      sx={{
        position: "relative",
        flexShrink: 0,
        width: CARD_WIDTH,
        height: CARD_HEIGHT,
        p: 1.25,
        display: "flex",
        flexDirection: "column",
        justifyContent: "space-between",
        borderRadius: 1.5,
        border: "1px solid",
        borderColor: "divider",
        bgcolor: "background.paper",
        "&:hover .attachment-card-remove, &:focus-within .attachment-card-remove": {
          opacity: 1,
          pointerEvents: "auto",
        },
      }}
    >
      <Typography
        variant="body2"
        title={name}
        sx={{
          overflow: "hidden",
          // Four lines, then ellipsis: a hashed export name fills the card and
          // a short one leaves the badge where the eye expects it.
          display: "-webkit-box",
          WebkitBoxOrient: "vertical",
          WebkitLineClamp: 4,
          wordBreak: "break-word",
          lineHeight: 1.35,
        }}
      >
        {name}
      </Typography>
      <Box
        sx={{
          alignSelf: "flex-start",
          px: 0.75,
          py: 0.25,
          borderRadius: 0.75,
          border: "1px solid",
          borderColor: "divider",
        }}
      >
        <Typography variant="caption" color="text.secondary">
          {attachmentTypeLabel(name)}
        </Typography>
      </Box>
      {onRemove && (
        // The remove control sits OUTSIDE the card's corner and is revealed on
        // hover. It is opacity-toggled rather than mounted on hover so it stays
        // in the tab order: `:focus-within` brings it back for keyboard users,
        // who get no hover.
        <Tooltip title={`Remove ${name}`}>
          <IconButton
            className="attachment-card-remove"
            size="small"
            aria-label={`Remove ${name}`}
            onClick={onRemove}
            disabled={disabled === true}
            sx={{
              position: "absolute",
              top: 0,
              left: 0,
              transform: "translate(-40%, -40%)",
              opacity: 0,
              pointerEvents: "none",
              transition: "opacity 120ms",
              bgcolor: "background.paper",
              border: "1px solid",
              borderColor: "divider",
              "&:hover": { bgcolor: "action.hover" },
              "&:focus-visible": { opacity: 1, pointerEvents: "auto" },
            }}
          >
            <X size={12} />
          </IconButton>
        </Tooltip>
      )}
    </Box>
  );
}

/**
 * The card strip inside a composer. Scrolls rather than wraps: the composer must
 * not grow taller as files are added, or the send control walks down the page.
 */
export function AttachmentCardStrip({
  names,
  onRemove,
  disabled,
}: {
  names: string[];
  onRemove?: ((name: string) => void) | undefined;
  disabled?: boolean | undefined;
}) {
  if (names.length === 0) return null;
  return (
    <Stack
      direction="row"
      spacing={1}
      sx={{
        mb: 1.5,
        overflowX: "auto",
        // Room for the remove button, which is translated OUTSIDE each card's
        // top-left corner. `overflow-x: auto` makes this a scroll container, and
        // a scroll container clips on BOTH axes — setting one axis to a
        // non-visible value computes the other to `auto` rather than leaving it
        // `visible`. So the button was cut off against this box's top and left
        // edges. The padding has to EXCEED the button's translated overhang
        // (~10px at size "small"), not merely be non-zero.
        pt: 1.5,
        pl: 1.5,
      }}
    >
      {names.map((name) => (
        <AttachmentCard
          key={name}
          name={name}
          disabled={disabled === true}
          {...(onRemove ? { onRemove: () => onRemove(name) } : {})}
        />
      ))}
    </Stack>
  );
}
