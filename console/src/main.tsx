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

import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import { AcrylicOrangeTheme, CssBaseline, OxygenUIThemeProvider } from '@wso2/oxygen-ui';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import AppAuthProvider from './auth/AuthProvider';
import { App } from './App';

// One process-wide QueryClient. Defaults match the design's polling
// model: tab-visibility gating via refetchIntervalInBackground:false on
// each polling hook; refetchOnWindowFocus auto-refreshes when the user
// returns to the tab; staleTime:0 so polling cadence drives freshness.
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: true,
      refetchIntervalInBackground: false,
      staleTime: 0,
      retry: 1,
    },
  },
});

createRoot(document.getElementById('app')!).render(
  <StrictMode>
    <AppAuthProvider>
      <OxygenUIThemeProvider theme={AcrylicOrangeTheme}>
        <CssBaseline />
        <QueryClientProvider client={queryClient}>
          <BrowserRouter>
            <App />
          </BrowserRouter>
        </QueryClientProvider>
      </OxygenUIThemeProvider>
    </AppAuthProvider>
  </StrictMode>
);
