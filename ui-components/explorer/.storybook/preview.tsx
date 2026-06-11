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

import { useLayoutEffect } from 'react';
import type { Preview } from '@storybook/react-vite';
import { AcrylicOrangeTheme, CssBaseline, OxygenUIThemeProvider } from '@wso2/oxygen-ui';
import { useColorScheme } from '@mui/material/styles';

/**
 * Flip MUI's color scheme synchronously before the browser paints. Using
 * useLayoutEffect (not useEffect) plus setting the html attribute directly
 * avoids the one-frame flash that happens when setMode runs post-paint.
 */
function ThemeModeApplier({ mode }: { mode: 'light' | 'dark' }) {
  const { setMode } = useColorScheme();
  useLayoutEffect(() => {
    const html = document.documentElement;
    html.setAttribute('data-mui-color-scheme', mode);
    html.setAttribute('data-color-scheme', mode);
    html.style.colorScheme = mode;
    setMode(mode);
  }, [mode, setMode]);
  return null;
}

const preview: Preview = {
  globalTypes: {
    theme: {
      name: 'Theme',
      description: 'Light / dark mode',
      defaultValue: 'light',
      toolbar: {
        icon: 'circlehollow',
        items: [
          { value: 'light', title: 'Light', icon: 'sun' },
          { value: 'dark', title: 'Dark', icon: 'moon' },
        ],
        dynamicTitle: true,
      },
    },
  },
  decorators: [
    (Story, context) => {
      const mode = (context.globals.theme ?? 'light') as 'light' | 'dark';
      // Apply the attribute synchronously during render so the very first
      // paint already reads the correct CSS variables. setAttribute is
      // idempotent, so repeating it on every render is safe.
      if (typeof document !== 'undefined') {
        const html = document.documentElement;
        html.setAttribute('data-mui-color-scheme', mode);
        html.setAttribute('data-color-scheme', mode);
        html.style.colorScheme = mode;
      }
      return (
        <OxygenUIThemeProvider theme={AcrylicOrangeTheme} defaultMode={mode}>
          <CssBaseline />
          <ThemeModeApplier mode={mode} />
          <div
            style={{
              padding: 16,
              background:
                'var(--oxygen-palette-background-default, var(--mui-palette-background-default, #fff))',
              color:
                'var(--oxygen-palette-text-primary, var(--mui-palette-text-primary, #1a1a1a))',
              minHeight: '100vh',
            }}
          >
            <Story />
          </div>
        </OxygenUIThemeProvider>
      );
    },
  ],
  parameters: {
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    a11y: { test: 'todo' },
  },
};

export default preview;
