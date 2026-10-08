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

import { useCallback, type ReactNode } from "react";
import { useNavigate, useParams } from "@tanstack/react-router";
import { Typography } from "@wso2/oxygen-ui";
import { CardFrame } from "../../shell/components/CardFrame";
import { cardTitle, pageOfCard, pageTitle, type ProjectCard, type ProjectPage } from "../../shell/scope";
import { WorkspaceTabs } from "./WorkspaceTabs";

/** A card's place on the track, where it has one (Spec, Design and Prototype show theirs as tabs). */
const STEP: Partial<Record<ProjectCard, number>> = { build: 3 };

/** Where each Page is: a card closes back to the one it is over. */
const PAGE_PATH = {
  overview: "/projects/$projectName",
  builds: "/projects/$projectName/builds",
  validations: "/projects/$projectName/validations",
  deploy: "/projects/$projectName/deploy",
  issues: "/projects/$projectName/issues",
} as const satisfies Record<ProjectPage, string>;

/**
 * A card drawn over the project Page that lists it (scope.ts `pageOfCard`):
 * the page stays rendered under a scrim, and closing (X, Escape, or a click
 * on the scrim) goes back to it. Each card is a route of its own, so this is
 * what the card routes render; the frame itself is the shell's `CardFrame`.
 *
 * Spec, Design and Prototype are one workspace, so their header is the
 * Spec · Design · Prototype tabs rather than a title; any other card shows its title, or the `title`
 * it is given when its own name says more ("Configure Staging"). A card's own actions (the design card's Address
 * comments) sit in the header beside the close button, and wrap under the
 * tabs at phone width. A `fill` body is laid out by its children (the spec
 * card's rail and document scroll on their own); otherwise the body is one
 * padded scroll.
 */
export function CardOverlay({
  card,
  title = cardTitle(card),
  fill = false,
  actions,
  children,
}: {
  card: ProjectCard;
  title?: string;
  fill?: boolean;
  actions?: ReactNode;
  children: ReactNode;
}) {
  const { projectName } = useParams({ from: "/projects/$projectName" });
  const navigate = useNavigate();

  const page = pageOfCard(card);
  const close = useCallback(
    () => void navigate({ to: PAGE_PATH[page], params: { projectName } }),
    [navigate, page, projectName],
  );
  const tabbed = card === "spec" || card === "design" || card === "prototype";
  const step = STEP[card];

  return (
    <CardFrame
      name={tabbed ? cardTitle(card) : title}
      heading={
        tabbed ? (
          <WorkspaceTabs active={card} />
        ) : (
          <>
            {step !== undefined && (
              <Typography variant="caption" color="text.secondary" sx={{ fontFamily: "monospace" }}>
                {step} / 4
              </Typography>
            )}
            <Typography component="h2" sx={{ fontSize: "1rem", fontWeight: 600, flex: 1 }}>
              {title}
            </Typography>
          </>
        )
      }
      closeHint={page === "overview" ? "Back to project overview" : `Back to ${pageTitle(page)}`}
      onClose={close}
      actions={actions}
      fill={fill}
      openKey={card}
    >
      {children}
    </CardFrame>
  );
}
