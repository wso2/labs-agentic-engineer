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

import { StrictMode, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  Box,
  ClassicTheme,
  CssBaseline,
  OxygenUIThemeProvider,
  Stack,
  Tab,
  Tabs,
  Typography,
} from "@wso2/oxygen-ui";
import { genUiSystemPrompt } from "@aep/ui-genui";
import { ChatPage } from "./pages/ChatPage.js";
import { ComponentsPage } from "./pages/ComponentsPage.js";
import { ViewsPage } from "./pages/ViewsPage.js";

const promptChars = genUiSystemPrompt().length;

type Page = "components" | "views" | "chat";

// The page lives in ?page= rather than the hash, which the components page
// uses to jump between components.
function initialPage(): Page {
  const page = new URLSearchParams(window.location.search).get("page");
  return page === "views" || page === "chat" ? page : "components";
}

function Demo() {
  const [page, setPage] = useState<Page>(initialPage);
  const choose = (next: Page) => {
    setPage(next);
    const url = new URL(window.location.href);
    url.searchParams.set("page", next);
    url.hash = "";
    window.history.replaceState(null, "", url);
  };

  return (
    <Stack spacing={3} sx={{ p: 3 }}>
      <Stack spacing={1}>
        <Typography variant="h5" component="h1">
          GenUI catalog
        </Typography>
        <Typography variant="body2" color="text.secondary">
          A model writes JSON that names only these components and actions; the
          host renders it with Oxygen UI. The system prompt that teaches a model
          this catalog is {promptChars.toLocaleString()} characters (≈
          {Math.round(promptChars / 4).toLocaleString()} tokens).
        </Typography>
      </Stack>
      <Box>
        <Tabs value={page} onChange={(_, next: Page) => choose(next)}>
          <Tab value="components" label="Components" />
          <Tab value="views" label="Composed views" />
          <Tab value="chat" label="Chat" />
        </Tabs>
      </Box>
      {page === "components" ? <ComponentsPage /> : page === "views" ? <ViewsPage /> : <ChatPage />}
    </Stack>
  );
}

const container = document.getElementById("root");
if (container) {
  createRoot(container).render(
    <StrictMode>
      {/* Classic: the theme the Oxygen UI Storybook shows. */}
      <OxygenUIThemeProvider theme={ClassicTheme}>
        <CssBaseline />
        <Demo />
      </OxygenUIThemeProvider>
    </StrictMode>,
  );
}
