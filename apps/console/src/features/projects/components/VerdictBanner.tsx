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

import { Box, Button, Typography, alpha } from "@wso2/oxygen-ui";
import { ArrowRight } from "@wso2/oxygen-ui-icons-react";
import { createLink } from "@tanstack/react-router";
import {
  verdictSentence,
  type ValidationCounts,
} from "../../validation/lib/verdict";
import { validationView } from "../lib/pipeline";

const LinkButton = createLink(Button);

/**
 * The development environment's validation evidence: what the last attempt
 * found, and the way to the report.
 *
 * EVERY state takes its sentence from the SHARED copy the Validation page's tile
 * reads, word for word. Writing any of them here is what let the two come apart:
 * this banner announced a lifecycle state as a verdict ("This deployment's verdict:
 * awaiting fix.") and, on a settled failure, led with the count that PASSED — so a
 * reader moving between the two surfaces met a different voice and a different
 * headline number for one outcome.
 */
export function VerdictBanner({
  projectName,
  validation,
  verdict,
  repairing,
  counts,
}: {
  projectName: string;
  /** deploy.validation — the loop's position. */
  validation: string;
  /** The run's stored verdict, which `awaiting-fix` folds away. */
  verdict: string;
  /** The attempt in flight repairs that verdict rather than re-asking it. */
  repairing: boolean;
  counts?: ValidationCounts;
}) {
  const view = validationView(validation);
  if (!view) return null;
  // WHICH verdict the sentence is about differs by state, and only that. A settled
  // deploy.validation IS the verdict, mirrored — which is also the only place the
  // three states outside COUNTABLE (`inconclusive`, `unreported`, `skipped`) can be
  // read from here, since their run row is never fetched. The two lifecycle values
  // are the ones that fold a verdict away, so those take the run's.
  const inFlight = validation === "running" || validation === "awaiting-fix";
  const sentence =
    verdictSentence(inFlight ? verdict : validation, counts, validation, repairing) ||
    // `skipped`, and any value from a newer server. The shared copy deliberately has
    // no sentence for skipped, so naming the verdict is what is left to say.
    `This deployment's verdict: ${view.label}.`;
  return (
    <Box
      sx={(theme) => {
        const main =
          view.tone === "ghost" || view.tone === "neutral"
            ? theme.palette.text.secondary
            : theme.palette[view.tone].main;
        return {
          border: `1px solid ${alpha(main, 0.35)}`,
          bgcolor: alpha(main, 0.06),
          borderRadius: 2,
          px: 1.75,
          py: 1.25,
          display: "flex",
          alignItems: "center",
          gap: 1.25,
        };
      }}
    >
      <Typography variant="body2" color="text.secondary" sx={{ flexGrow: 1 }}>
        {sentence}
      </Typography>
      <LinkButton
        to="/projects/$projectName/validation"
        params={{ projectName }}
        size="small"
        color="inherit"
        endIcon={<ArrowRight size={14} aria-hidden />}
        // Text, not outlined — it sits inside the banner's own border. Which
        // costs it MUI's 5px text padding, half what the outlined navigation
        // buttons get, so px is said explicitly.
        sx={{ flexShrink: 0, fontWeight: 500, px: 1.25 }}
      >
        View validations
      </LinkButton>
    </Box>
  );
}
