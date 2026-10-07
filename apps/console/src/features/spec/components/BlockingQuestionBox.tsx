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

import { useId, useState, type FormEvent } from "react";
import { Box, Button, TextField, Typography } from "@wso2/oxygen-ui";
import type * as Y from "yjs";
import type { SpecFeature } from "../api/specModel";
import { answerBlockingQuestion } from "../collab/specEdits";
import type { BlockingQuestion } from "../model/questions";
import { soft } from "./Tag";

/**
 * A question a feature's interview waits on, on the feature's page, as its
 * file's Open Questions has it: the answers the agent offers, and room for
 * the user's own. Answering is an edit to the file (collab/specEdits.ts): the
 * question leaves Open Questions and the answer lands in Decisions, so the
 * box, the chip and Next up go with it.
 */
export function BlockingQuestionBox({
  doc,
  feature,
  blocking,
}: {
  doc: Y.Doc;
  feature: Pick<SpecFeature, "name" | "path">;
  blocking: BlockingQuestion;
}) {
  const headingId = useId();
  const [own, setOwn] = useState("");
  const answer = (text: string) => answerBlockingQuestion(doc, feature.path, blocking.question, text);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    if (own.trim()) answer(own);
  };

  return (
    <Box
      data-anchor="blocking"
      component="section"
      aria-labelledby={headingId}
      sx={{
        mt: 2.25,
        maxWidth: "72ch",
        border: 1,
        borderColor: "warning.main",
        bgcolor: soft("warning"),
        borderRadius: 2.5,
        px: 1.75,
        py: 1.5,
        display: "flex",
        flexDirection: "column",
        gap: 1,
      }}
    >
      <Typography sx={{ fontSize: "0.75rem", fontWeight: 600, letterSpacing: "0.02em", color: "warning.main" }}>
        BLOCKING QUESTION
      </Typography>
      <Typography id={headingId} component="h2" sx={{ fontWeight: 600, fontSize: "0.875rem" }}>
        {blocking.question}
      </Typography>
      <Typography variant="body2" color="text.secondary">
        {feature.name}'s interview waits for your answer. It does not stop a build of the other features.
      </Typography>
      {blocking.options.length > 0 && (
        <Box sx={{ display: "flex", flexDirection: "column", alignItems: "flex-start", gap: 0.75 }}>
          {blocking.options.map((option) => (
            <Button
              key={option}
              size="small"
              variant="outlined"
              color="warning"
              onClick={() => answer(option)}
              sx={{ textAlign: "start", justifyContent: "flex-start", bgcolor: "background.paper" }}
            >
              {option}
            </Button>
          ))}
        </Box>
      )}
      <Box component="form" onSubmit={onSubmit} sx={{ display: "flex", flexWrap: "wrap", gap: 0.75, alignItems: "flex-start" }}>
        <TextField
          size="small"
          value={own}
          onChange={(e) => setOwn(e.target.value)}
          placeholder={blocking.options.length > 0 ? "Or answer in your own words" : "Answer in your own words"}
          slotProps={{ htmlInput: { "aria-label": "Your answer" } }}
          sx={{ flex: "1 1 220px", bgcolor: "background.paper" }}
        />
        <Button type="submit" size="small" variant="contained" disabled={!own.trim()}>
          Answer
        </Button>
      </Box>
    </Box>
  );
}
