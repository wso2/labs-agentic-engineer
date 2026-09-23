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

import { StrictMode, useMemo, useState, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import {
  AcrylicOrangeTheme,
  Alert,
  Box,
  ClassicTheme,
  CssBaseline,
  MenuItem,
  OxygenUIThemeProvider,
  Paper,
  Stack,
  TextField,
  ThemeSwitcher,
  Typography,
  useColorScheme,
} from "@wso2/oxygen-ui";
import {
  genUiSystemPrompt,
  validateGenUiSpec,
  type GenUiActionHandlers,
  type GenUiDispatchOutcome,
  type GenUiSpec,
} from "@aep/ui-genui";
import { exampleSpecs } from "@aep/ui-genui/examples";
import { GenUiView as OxygenGenUiView } from "@aep/ui-genui-oxygen";
import { GenUiView as ShadcnGenUiView } from "@aep/ui-genui-shadcn";
import "@aep/ui-genui-shadcn/styles.css";

const exampleNames = Object.keys(exampleSpecs);
const promptChars = genUiSystemPrompt().length;

// Every catalog action just logs here; the outcome log shows what reached it.
const handlers: GenUiActionHandlers = {
  openTask: (params) => console.info("openTask", params),
  approveDependency: (params) => console.info("approveDependency", params),
  rejectDependency: (params) => console.info("rejectDependency", params),
  openDeployments: (params) => console.info("openDeployments", params),
  editExternalResource: (params) => console.info("editExternalResource", params),
};

interface LoggedOutcome {
  designSystem: string;
  outcome: GenUiDispatchOutcome;
}

function parse(text: string) {
  try {
    return validateGenUiSpec(JSON.parse(text));
  } catch (error) {
    return { ok: false as const, issues: [`Not JSON: ${(error as Error).message}`] };
  }
}

function Panel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Stack spacing={1}>
      <Typography variant="subtitle2">{title}</Typography>
      <Paper variant="outlined">
        <Box sx={{ p: 2 }}>{children}</Box>
      </Paper>
    </Stack>
  );
}

// shadcn switches to dark with a `dark` class; follow Oxygen's colour scheme
// so both panels are in the same mode.
function ShadcnColorScheme({ children }: { children: ReactNode }) {
  const { mode, systemMode } = useColorScheme();
  const dark = (mode === "system" ? systemMode : mode) === "dark";
  return <div className={dark ? "dark" : undefined}>{children}</div>;
}

function Demo() {
  const [example, setExample] = useState(exampleNames[0] ?? "");
  const [text, setText] = useState(() =>
    JSON.stringify(exampleSpecs[example], null, 2),
  );
  const [log, setLog] = useState<LoggedOutcome[]>([]);
  const result = useMemo(() => parse(text), [text]);

  const pickExample = (name: string) => {
    setExample(name);
    setText(JSON.stringify(exampleSpecs[name], null, 2));
    setLog([]);
  };
  const logFrom = (designSystem: string) => (outcome: GenUiDispatchOutcome) =>
    setLog((prev) => [{ designSystem, outcome }, ...prev]);

  const render = (spec: GenUiSpec) => (
    <Box
      sx={{
        display: "grid",
        gap: 3,
        gridTemplateColumns: { lg: "1fr 1fr" },
        alignItems: "start",
      }}
    >
      <Panel title="Oxygen UI">
        <OxygenGenUiView
          spec={spec}
          handlers={handlers}
          onActionOutcome={logFrom("Oxygen UI")}
        />
      </Panel>
      <Panel title="shadcn/ui">
        <ShadcnColorScheme>
          <ShadcnGenUiView
            spec={spec}
            handlers={handlers}
            onActionOutcome={logFrom("shadcn/ui")}
          />
        </ShadcnColorScheme>
      </Panel>
    </Box>
  );

  return (
    <Box
      sx={{
        p: 3,
        display: "grid",
        gap: 3,
        gridTemplateColumns: { md: "minmax(320px, 1fr) 3fr" },
      }}
    >
      <Stack spacing={2}>
        <Stack direction="row" spacing={2}>
          <Typography variant="h5" component="h1">
            GenUI design systems
          </Typography>
          <ThemeSwitcher />
        </Stack>
        <Typography variant="body2" color="text.secondary">
          One spec, every design system. System prompt for this catalog:{" "}
          {promptChars.toLocaleString()} characters (≈
          {Math.round(promptChars / 4).toLocaleString()} tokens).
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
          render(result.spec)
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
            Press a button in either rendered UI.
          </Typography>
        ) : (
          log.map(({ designSystem, outcome }, index) => (
            <Typography key={index} variant="body2" sx={{ fontFamily: "monospace" }}>
              {designSystem} · {outcome.status} · {outcome.action}
            </Typography>
          ))
        )}
      </Stack>
    </Box>
  );
}

// Classic is what the Oxygen UI Storybook shows; Acrylic Orange is what the
// console uses.
const themes = [
  { key: "classic", label: "Classic (Oxygen Storybook)", theme: ClassicTheme },
  { key: "acrylic-orange", label: "Acrylic Orange (console)", theme: AcrylicOrangeTheme },
];

const container = document.getElementById("root");
if (container) {
  createRoot(container).render(
    <StrictMode>
      <OxygenUIThemeProvider themes={themes} initialTheme="classic">
        <CssBaseline />
        <Demo />
      </OxygenUIThemeProvider>
    </StrictMode>,
  );
}
