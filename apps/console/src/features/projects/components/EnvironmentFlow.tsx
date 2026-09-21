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

import { Fragment } from "react";
import { Box, Skeleton, Stack } from "@wso2/oxygen-ui";
import { ArrowRight } from "@wso2/oxygen-ui-icons-react";
import type { EnvironmentRow } from "../lib/deploymentLedger";
import { findEnvironment, labelOf, type EnvironmentInfo } from "../lib/environments";
import {
  EnvironmentFlowCard,
  type EnvironmentFlowDetail,
  type FlowTarget,
} from "./EnvironmentCards";
import type { ConnectionRow } from "../lib/promotion";
import type { PromotionSource } from "../lib/deploymentFlow";

// The Deployments board as the PIPELINE it actually is: one full-detail card
// per environment, left to right in the platform's promotion order, with an
// arrow between them. However many environments the pipeline has — one, two,
// six — the row is the same shape, because nothing here counts them.
//
// The order is the order `environmentRows` handed over. This file never
// sorts, never reads `position` to re-derive it, and never assumes which
// environment is which: every fact a card states comes off its own
// `EnvironmentInfo` and its own row.

/** A fixed card width, so the cards are the same shape and the row scrolls
 *  rather than squeezing six environments into a viewport. Wide enough that a
 *  component's name, its kind and its status sit on one line without wrapping,
 *  and that a promote button and its reason caption share a row rather than
 *  stacking — the row is scrollable, so width costs nothing but a scroll. */
const CARD_WIDTH = 600;

/**
 * The flow while the pipeline is not known. It states NOTHING — not "no
 * environments", not "nothing deployed": the environments list is a network
 * read, and a shimmer is the only honest thing to draw before it lands or
 * when it failed. The page above owns the error alert and the retry.
 */
export function EnvironmentFlowSkeleton() {
  return (
    <Box data-testid="environment-flow-skeleton" aria-label="Loading the deployment pipeline">
      <Skeleton variant="rounded" height={320} />
    </Box>
  );
}

export interface EnvironmentFlowProps extends EnvironmentFlowDetail {
  projectName: string;
  /** The pipeline's environments, in promotion order, exactly as served. */
  environments: EnvironmentInfo[];
  /** One row per environment, in the same order (`environmentRows`). */
  rows: EnvironmentRow[];
  /** Open an environment's page — the whole card is this control. */
  onTryOut: (environment: string) => void;
  onPromote: () => void;
  onConfigureConnection: (row: ConnectionRow) => void;
  onConfigurePromoteTarget: (row: ConnectionRow) => void;
}

/** The promotion target as its own row describes it — `null` on the last
 *  environment, and on a `promotesTo` the served list does not name (which is
 *  a pipeline the console cannot follow, not an empty environment). */
function flowTarget(
  environments: EnvironmentInfo[],
  rows: EnvironmentRow[],
  promotesTo: string | undefined,
): FlowTarget | null {
  if (!promotesTo) return null;
  const env = findEnvironment(environments, promotesTo);
  const row = rows.find((r) => r.environment === promotesTo);
  if (!row) return null;
  return {
    label: labelOf(env, promotesTo),
    // A binding is not a running version: a failed, converging or undeployed
    // target is populated too (see `productionLiveSentence`).
    state: !row.cards.some((c) => c.deployment)
      ? "empty"
      : row.status.tone === "success"
        ? "running"
        : "populated",
    statusLabel: row.status.label,
  };
}

/** The environment that promotes INTO `name`, and the version it runs — read
 *  off the served list and the rows, never off position: the source is the
 *  environment whose own `promotesTo` names this one. Null when nothing does
 *  (the pipeline's entry), which is what keeps an entry card from claiming a
 *  promotion would fill it. */
function promotionSource(
  environments: EnvironmentInfo[],
  rows: EnvironmentRow[],
  name: string,
): PromotionSource | null {
  const from = environments.find((e) => e.promotesTo === name);
  if (!from) return null;
  const row = rows.find((r) => r.environment === from.name);
  return { label: labelOf(from, from.name), version: row?.version ?? "" };
}

export function EnvironmentFlow({
  projectName,
  environments,
  rows,
  onTryOut,
  onPromote,
  onConfigureConnection,
  onConfigurePromoteTarget,
  ...detail
}: EnvironmentFlowProps) {
  // No rows is not "no environments" — it is equally the shape of a read that
  // has not landed or did not come back. The flow refuses to say which, and
  // draws the same shimmer the page draws while it waits.
  if (rows.length === 0) return <EnvironmentFlowSkeleton />;

  return (
    <Stack
      data-testid="environment-flow"
      direction="row"
      spacing={0}
      sx={{
        // stretch: every card takes the tallest card's height, so the pinned
        // trailing steps land on one line across the pipeline.
        alignItems: "stretch",
        overflowX: "auto",
        // The row is exactly as tall as its tallest card, so nothing here has
        // anywhere to scroll to vertically — said explicitly because an
        // `overflowX: auto` alone would compute the Y axis to `auto` too, and
        // NOTHING in this flow scrolls inside itself: a card is as tall as
        // its content, and a pipeline taller than the viewport scrolls the
        // PAGE.
        overflowY: "hidden",
        // Room for the cards' shadow and the scrollbar.
        pb: 1.5,
      }}
    >
      {rows.map((row, index) => {
        const env = findEnvironment(environments, row.environment);
        // A row whose environment the list does not describe has no steps to
        // derive — `stepsFor` is the platform's answer, not a guess.
        if (!env) return null;
        // The target is the environment `promotesTo` NAMES — not the next row
        // along. They agree on a linear pipeline, but this file's whole
        // contract is never assuming which environment is which, and a
        // positional neighbour would let the card name one environment and
        // report another's occupancy.
        const target = flowTarget(environments, rows, env.promotesTo);
        return (
          <Fragment key={row.environment}>
            {index > 0 && (
              <Stack
                aria-hidden
                sx={{ alignSelf: "center", justifyContent: "center", px: 1, flexShrink: 0 }}
              >
                <Box component={ArrowRight} size={20} sx={{ color: "text.disabled" }} />
              </Stack>
            )}
            <Box sx={{ display: "flex", flex: "0 0 auto", width: CARD_WIDTH, maxWidth: "100%" }}>
              <EnvironmentFlowCard
                projectName={projectName}
                env={env}
                row={row}
                // The ENTRY environment is `position === 0` — the same test
                // `deploymentLedger` uses to decide which row the deploy
                // aggregate answers for. Two definitions of "entry" would let
                // a card read the aggregate's version under a heading that
                // says a different environment.
                entry={env.position === 0}
                target={target}
                source={promotionSource(environments, rows, row.environment)}
                detail={detail}
                onOpen={onTryOut}
                onPromote={onPromote}
                onConfigureConnection={onConfigureConnection}
                onConfigurePromoteTarget={onConfigurePromoteTarget}
              />
            </Box>
          </Fragment>
        );
      })}
    </Stack>
  );
}
