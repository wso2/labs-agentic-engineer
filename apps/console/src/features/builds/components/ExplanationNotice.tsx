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

import { useState } from "react";
import { createLink } from "@tanstack/react-router";
import { Alert, AlertTitle, Box, Button, Typography } from "@wso2/oxygen-ui";
import { ArrowRight } from "@wso2/oxygen-ui-icons-react";
import { detailsText, type Explanation, type FailureDetails } from "../model/explain";

const LinkButton = createLink(Button);

function Details({ details }: { details: FailureDetails }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(detailsText(details));
      setCopied(true);
    } catch {
      // The clipboard is a convenience; the text is on screen regardless.
    }
  };
  return (
    <Box sx={{ mt: 1, display: "flex", flexDirection: "column", alignItems: "flex-start", gap: 0.5 }}>
      <Typography component="pre" variant="caption" sx={{ m: 0, fontFamily: "monospace", whiteSpace: "pre-wrap", overflowWrap: "anywhere" }}>
        {detailsText(details)}
      </Typography>
      <Button size="small" color="inherit" onClick={() => void copy()}>
        {copied ? "Copied" : "Copy details"}
      </Button>
    </Box>
  );
}

/**
 * Why the build is stuck or failed, above its tasks, with the way out: a link
 * where the platform knows where the fix lives (a dependency's values, the
 * design, the Deploy page), and a failure's details for a bug report.
 */
export function ExplanationNotice({ projectName, explanation }: { projectName: string; explanation: Explanation }) {
  const [open, setOpen] = useState(false);
  const { next, details } = explanation;
  return (
    <Alert severity={explanation.tone} role="status">
      <AlertTitle>{explanation.title}</AlertTitle>
      <Typography variant="body2">{explanation.body}</Typography>
      <Box sx={{ display: "flex", gap: 1, mt: 1, flexWrap: "wrap" }}>
        {next &&
          (next.to === "/projects/$projectName/deploy/$env/configure" ? (
            <LinkButton
              to={next.to}
              params={{ projectName, env: next.env }}
              size="small"
              variant="outlined"
              color="inherit"
              endIcon={<ArrowRight size={14} aria-hidden />}
            >
              {next.label}
            </LinkButton>
          ) : (
            <LinkButton
              to={next.to}
              params={{ projectName }}
              size="small"
              variant="outlined"
              color="inherit"
              endIcon={<ArrowRight size={14} aria-hidden />}
            >
              {next.label}
            </LinkButton>
          ))}
        {details && (
          <Button size="small" color="inherit" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
            {open ? "Hide details" : "Show details"}
          </Button>
        )}
      </Box>
      {open && details && <Details details={details} />}
    </Alert>
  );
}
