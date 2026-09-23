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

import { useMemo, useState } from "react";
import {
  Alert,
  Box,
  MenuItem,
  Paper,
  Stack,
  TextField,
  Typography,
} from "@wso2/oxygen-ui";
import { validateGenUiSpec, type GenUiDispatchOutcome } from "@aep/ui-genui";
import { exampleSpecs } from "@aep/ui-genui/examples";
import { GenUiView } from "@aep/ui-genui-oxygen";
import { handlers } from "../handlers.js";

const exampleNames = Object.keys(exampleSpecs);

function parse(text: string) {
  try {
    return validateGenUiSpec(JSON.parse(text));
  } catch (error) {
    return { ok: false as const, issues: [`Not JSON: ${(error as Error).message}`] };
  }
}

/** Whole views: a full spec, editable, rendered as one UI. */
export function ViewsPage() {
  const [example, setExample] = useState(exampleNames[0] ?? "");
  const [text, setText] = useState(() =>
    JSON.stringify(exampleSpecs[example], null, 2),
  );
  const [log, setLog] = useState<GenUiDispatchOutcome[]>([]);
  const result = useMemo(() => parse(text), [text]);

  const pickExample = (name: string) => {
    setExample(name);
    setText(JSON.stringify(exampleSpecs[name], null, 2));
    setLog([]);
  };

  return (
    <Box
      sx={{
        display: "grid",
        gap: 3,
        gridTemplateColumns: { md: "minmax(320px, 1fr) 2fr" },
      }}
    >
      <Stack spacing={2}>
        <Typography variant="body2" color="text.secondary">
          Pick an example or paste model output. It is validated, then rendered
          as one UI.
        </Typography>
        <TextField
          select
          label="Example"
          size="small"
          value={example}
          onChange={(event) => pickExample(event.target.value)}
        >
          {exampleNames.map((name) => (
            <MenuItem key={name} value={name}>
              {name}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          label="Spec (JSON)"
          multiline
          minRows={20}
          value={text}
          onChange={(event) => setText(event.target.value)}
          slotProps={{ htmlInput: { style: { fontFamily: "monospace", fontSize: 12 } } }}
        />
      </Stack>
      <Stack spacing={2}>
        {result.ok ? (
          <Paper variant="outlined">
            <Box sx={{ p: 2 }}>
              <GenUiView
                spec={result.spec}
                handlers={handlers}
                onActionOutcome={(outcome) => setLog((prev) => [outcome, ...prev])}
              />
            </Box>
          </Paper>
        ) : (
          <Alert severity="error">
            {result.issues.map((issue) => (
              <div key={issue}>{issue}</div>
            ))}
          </Alert>
        )}
        <Typography variant="subtitle2">Action log</Typography>
        {log.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            Press a button in the rendered UI.
          </Typography>
        ) : (
          log.map((outcome, index) => (
            <Typography key={index} variant="body2" sx={{ fontFamily: "monospace" }}>
              {outcome.status} · {outcome.action}
            </Typography>
          ))
        )}
      </Stack>
    </Box>
  );
}

