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

import Markdown, { defaultUrlTransform } from "react-markdown";
import { Box } from "@wso2/oxygen-ui";

// Console-wide markdown renderer for agent-authored content (skill bodies,
// alert diagnoses, and — later — agent chat messages). Styling is
// theme-token only so it holds up in light and dark; see design-system.md.
/** The scheme a chat message uses to link a spec document: `aep://spec/<repo path>`. */
export const SPEC_LINK_PREFIX = "aep://spec/";

/** The document a spec link names, or null for any other href. */
export function specLinkPath(href: string | undefined): string | null {
  if (!href || !href.startsWith(SPEC_LINK_PREFIX)) return null;
  const path = href.slice(SPEC_LINK_PREFIX.length);
  return path.startsWith("specs/") ? path : null;
}

export function MarkdownView({
  children,
  onSpecLink,
}: {
  children: string;
  /**
   * A link into the spec (`aep://spec/<path>`) was clicked — the design
   * turn's closing list links each open dependency's definition this way
   * (ADR-0028). Without a handler the link renders as plain text, since a
   * browser cannot follow the scheme.
   */
  onSpecLink?: ((path: string) => void) | undefined;
}) {
  return (
    <Box
      sx={{
        color: "text.primary",
        fontSize: "0.875rem",
        lineHeight: 1.6,
        wordBreak: "break-word",
        "& h1, & h2, & h3, & h4": {
          mt: 2,
          mb: 1,
          fontWeight: 600,
          lineHeight: 1.3,
        },
        "& h1": { fontSize: "1.2rem" },
        "& h2": { fontSize: "1.05rem" },
        "& h3, & h4": { fontSize: "0.95rem" },
        "& p": { my: 1 },
        "& ul, & ol": { pl: 3, my: 1 },
        "& li": { mb: 0.5 },
        "& a": { color: "primary.main" },
        "& hr": { border: 0, borderTop: 1, borderColor: "divider", my: 2 },
        "& blockquote": {
          borderLeft: 3,
          borderColor: "divider",
          pl: 2,
          ml: 0,
          color: "text.secondary",
        },
        "& code": {
          fontFamily: "monospace",
          fontSize: "0.85em",
          px: 0.5,
          py: 0.25,
          borderRadius: 0.5,
          bgcolor: "action.hover",
        },
        "& pre": {
          p: 1.5,
          my: 1,
          borderRadius: 1,
          overflowX: "auto",
          bgcolor: "action.hover",
          "& code": { bgcolor: "transparent", p: 0 },
        },
        "& table": {
          borderCollapse: "collapse",
          my: 1,
          "& td, & th": {
            border: 1,
            borderColor: "divider",
            px: 1,
            py: 0.5,
            textAlign: "left",
          },
        },
        "& img": { maxWidth: "100%" },
      }}
    >
      <Markdown
        // The default transform drops unknown schemes; the spec scheme rides
        // through so the renderer below can turn it into a click.
        urlTransform={(url) => (url.startsWith(SPEC_LINK_PREFIX) ? url : defaultUrlTransform(url))}
        components={{
          a: ({ href, children: linkChildren }) => {
            const path = specLinkPath(href);
            if (path === null) return <a href={href}>{linkChildren}</a>;
            if (!onSpecLink) return <>{linkChildren}</>;
            return (
              <a
                href={href}
                onClick={(e) => {
                  e.preventDefault();
                  onSpecLink(path);
                }}
              >
                {linkChildren}
              </a>
            );
          },
        }}
      >
        {children}
      </Markdown>
    </Box>
  );
}
