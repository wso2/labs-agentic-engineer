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

import type { ReactNode } from "react";
import { Box, ButtonBase, Stack, Typography } from "@wso2/oxygen-ui";
import { ArrowUpRight, ChevronRight } from "@wso2/oxygen-ui-icons-react";

/**
 * One row of a project's inventory, built to the build page's task-row
 * geometry (`BuildTaskList.tsx`).
 *
 * The two pages list different things — versions of work there, the parts of
 * the app here — but a reader moving between them should not have to re-learn
 * what a row is. The shape is the build row's: a rounded tile, the title and a
 * chip on the first line, a note on the second, a 1px divider under every row
 * but the last.
 *
 * The VERTICAL measurements are tighter than the build row's, on purpose. A
 * task row is 82px and earns it — it carries a state colour, an issue link, a
 * timestamp and sometimes a button. These rows carry a name, a category and a
 * sentence, and at 82px that reads as three items floating in a panel. So the
 * padding is 10px rather than 15, the second line sits 2px under the first
 * rather than 7, and the tile is 32px. Same shape, less air.
 *
 * One deliberate deviation. The build row puts its category chip on the second
 * line beside the note; copied literally, that wraps here, because this list
 * lives in a ~270px column rather than the full page width, and a wrapped note
 * makes a 94px row next to an 80px one. So the chip rides the title line,
 * right-aligned, and the note gets the second line to itself, clamped to one.
 * Same measurements, same rhythm, no ragged heights — and the chips form a
 * column of their own rather than trailing off after each name.
 *
 * The leading tile is untinted here on purpose. On the build page its colour is
 * the task's STATE, and these rows have no state to report — a component's is
 * on the deployments board, a dependency's in the catalog drawer. A tile that
 * borrowed the colour without the meaning would be the page's loudest lie.
 */
export function OverviewRow({
  icon,
  title,
  trailing,
  meta,
  caption,
  onClick,
  href,
  last = false,
}: {
  /** Goes in the leading tile. Sized 18px by the caller, as on the build page. */
  icon: ReactNode;
  title: string;
  /** Marks at the row's right edge, vertically centred against both lines. */
  trailing?: ReactNode;
  /** Marks that lead the second line, where the build row puts its component
   *  chip. Keeping the category down here is what makes the two rows the same
   *  height as well as the same shape. */
  meta?: ReactNode;
  /** The second line. Rows without one are still full height, so a list of
   *  them keeps its rhythm rather than shuffling between two row sizes. */
  caption?: string | undefined;
  /** Omit for a row that does not go anywhere — it then renders no chevron. */
  onClick?: (() => void) | undefined;
  /**
   * An EXTERNAL destination, opened in a new tab. Pass this instead of
   * `onClick` — the row then renders as a real anchor, so the browser's own
   * ways of following a link (middle click, ⌘-click, "copy link address") all
   * work, which they never do on a button that calls `window.open`.
   *
   * Its affordance is the outward arrow rather than the chevron: a chevron
   * promises the next panel of this console, and this one leaves it.
   */
  href?: string | undefined;
  last?: boolean;
}) {
  const body = (
    <Stack
      direction="row"
      spacing={1.75}
      sx={{ alignItems: "center", px: 2.25, py: 1.25, width: "100%", minWidth: 0 }}
    >
      <Box
        aria-hidden
        sx={{
          width: 32,
          height: 32,
          borderRadius: 1.25,
          flexShrink: 0,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          bgcolor: "action.hover",
          color: "text.secondary",
        }}
      >
        {icon}
      </Box>

      <Box sx={{ flex: 1, minWidth: 0 }}>
        <Stack direction="row" spacing={1.25} sx={{ alignItems: "center", minWidth: 0 }}>
          <Typography
            sx={{
              fontSize: "0.90625rem",
              fontWeight: 500,
              whiteSpace: "nowrap",
              overflow: "hidden",
              textOverflow: "ellipsis",
            }}
            title={title}
          >
            {title}
          </Typography>
        </Stack>
        <Stack
          direction="row"
          spacing={1.125}
          sx={{ alignItems: "center", mt: 0.25, minWidth: 0, minHeight: 18 }}
        >
          {meta}
          {caption && (
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{
                flex: 1,
                minWidth: 0,
                lineHeight: 1.55,
                display: "-webkit-box",
                WebkitLineClamp: 1,
                WebkitBoxOrient: "vertical",
                overflow: "hidden",
              }}
            >
              {caption}
            </Typography>
          )}
        </Stack>
      </Box>

      {/* The chip hangs off the ROW, not the title line: right-aligned so the
          chips form their own column, and centred against both lines rather
          than sitting up beside the first one. */}
      {trailing && <Box sx={{ flexShrink: 0 }}>{trailing}</Box>}

      {/* Always laid out, hidden when the row goes nowhere. Skipping it for a
          static row shifted that row's chip 32px right of every other one, so
          the chips stopped forming a column the moment one row was not a link. */}
      <Box
        aria-hidden
        className="row-chevron"
        sx={{
          display: "flex",
          flexShrink: 0,
          color: "text.disabled",
          visibility: onClick || href ? "visible" : "hidden",
          opacity: 0.5,
          transition: (theme) =>
            theme.transitions.create("opacity", {
              duration: theme.transitions.duration.shortest,
            }),
        }}
      >
        {href ? <ArrowUpRight size={18} /> : <ChevronRight size={18} />}
      </Box>
    </Stack>
  );

  const interactiveSx = {
    display: "block",
    width: "100%",
    textAlign: "left",
    "&:hover": { bgcolor: "action.hover" },
    "&:focus-visible": {
      outline: 2,
      outlineColor: "primary.main",
      outlineOffset: -2,
    },
    "&:hover .row-chevron, &:focus-visible .row-chevron": { opacity: 1 },
  } as const;

  return (
    <Box
      sx={{
        borderBottom: last ? 0 : 1,
        borderColor: "divider",
      }}
    >
      {href ? (
        <ButtonBase
          component="a"
          href={href}
          target="_blank"
          rel="noreferrer"
          sx={interactiveSx}
        >
          {body}
        </ButtonBase>
      ) : onClick ? (
        <ButtonBase onClick={onClick} sx={interactiveSx}>
          {body}
        </ButtonBase>
      ) : (
        body
      )}
    </Box>
  );
}
