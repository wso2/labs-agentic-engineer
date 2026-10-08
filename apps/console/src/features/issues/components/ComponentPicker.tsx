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
import { Alert, Box, Button, FormControlLabel, Radio, RadioGroup, Skeleton, Typography } from "@wso2/oxygen-ui";
import { useDesignDependencies } from "../../deploy/api/deploy";
import { HandOffRefusedError, type useHandToCodingAgent } from "../api/handOff";

// The Issue card's picker for handing the issue to the coding agent: which of
// the design's components the issue is about. A design with one component
// still asks, with it chosen, so nothing is handed over before the person
// says so. A refusal (no deployed version) shows here in the server's words.

type HandOff = ReturnType<typeof useHandToCodingAgent>;

export function ComponentPicker({
  projectName,
  handOff,
  onCancel,
}: {
  projectName: string;
  handOff: HandOff;
  onCancel: () => void;
}) {
  const design = useDesignDependencies(projectName);
  const names = design.data?.map((c) => c.componentName) ?? [];
  const [choice, setChoice] = useState<string | null>(null);
  const chosen = choice ?? (names.length === 1 ? (names[0] ?? null) : null);
  const error = handOff.error;

  return (
    <Box
      component="section"
      aria-labelledby="hand-off-question"
      sx={{ display: "flex", flexDirection: "column", gap: 1, p: 1.5, border: 1, borderColor: "divider", borderRadius: 2 }}
    >
      <Typography id="hand-off-question" component="h3" sx={{ fontSize: "0.8125rem", fontWeight: 600 }}>
        Which component is it about?
      </Typography>
      {design.isPending ? (
        <Skeleton width="40%" />
      ) : design.isError ? (
        <Typography variant="body2" color="text.secondary">
          The design&apos;s components could not be read.
        </Typography>
      ) : names.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          The design has no components yet, so there is nothing to hand it to.
        </Typography>
      ) : (
        <RadioGroup aria-labelledby="hand-off-question" value={chosen ?? ""} onChange={(e) => setChoice(e.target.value)}>
          {names.map((name) => (
            <FormControlLabel key={name} value={name} control={<Radio size="small" />} label={name} disabled={handOff.isPending} />
          ))}
        </RadioGroup>
      )}
      {error && <Alert severity={error instanceof HandOffRefusedError ? "warning" : "error"}>{error.message}</Alert>}
      <Box sx={{ display: "flex", gap: 1 }}>
        {names.length > 0 && (
          <Button
            size="small"
            variant="contained"
            disabled={!chosen || handOff.isPending}
            onClick={() => chosen && handOff.mutate(chosen)}
          >
            Hand it over
          </Button>
        )}
        <Button size="small" variant="text" disabled={handOff.isPending} onClick={onCancel}>
          Cancel
        </Button>
      </Box>
    </Box>
  );
}
