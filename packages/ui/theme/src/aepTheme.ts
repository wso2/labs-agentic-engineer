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

import { createOxygenTheme } from "@wso2/oxygen-ui";
import { channel, tone } from "./tones.js";

// AEP's one theme, for the console and the prototypes it shows: the
// guided-shell design's colours laid over Oxygen's base. Type stays Oxygen's
// bundled Inter; only colours follow the design. See tones.ts for why each
// colour is spelled out in full.

interface Scheme {
  primary: [main: string, contrastText: string];
  background: string;
  paper: string;
  text: string;
  textMuted: string;
  divider: string;
  success: string;
  error: string;
  warning: string;
  info: string;
  /** Contrast text on the status colours. */
  onStatus: string;
}

function palette(s: Scheme) {
  return {
    primary: tone(...s.primary),
    success: tone(s.success, s.onStatus),
    error: tone(s.error, s.onStatus),
    warning: tone(s.warning, s.onStatus),
    info: tone(s.info, s.onStatus),
    background: {
      default: s.background,
      paper: s.paper,
      defaultChannel: channel(s.background),
      paperChannel: channel(s.paper),
    },
    text: {
      primary: s.text,
      secondary: s.textMuted,
      primaryChannel: channel(s.text),
      secondaryChannel: channel(s.textMuted),
    },
    divider: s.divider,
    dividerChannel: channel(s.divider),
  };
}

const light = palette({
  primary: ["#E2611B", "#FFFFFF"],
  background: "#F4F5F7",
  paper: "#FFFFFF",
  text: "#1A1D22",
  textMuted: "#656C78",
  divider: "#E1E4E9",
  success: "#1F7A4D",
  error: "#B3261E",
  warning: "#B26A00",
  info: "#2F6FB5",
  onStatus: "#FFFFFF",
});

const dark = palette({
  primary: ["#F07A3A", "#1A0E06"],
  background: "#0F1216",
  paper: "#171B21",
  text: "#E7EAEF",
  textMuted: "#9AA3B0",
  divider: "#262C35",
  success: "#4CC38A",
  error: "#F2716A",
  warning: "#E0A030",
  info: "#7FB2EA",
  onStatus: "#0F1216",
});

// Shell surfaces the stock palette has no slot for: the dark activity rail
// (and its hover wash), the chat column and its user bubbles, and the scrim
// and shadow of a card drawn over the overview. They are
// CSS variables switched by Oxygen's colour-scheme attribute, used as
// `bgcolor: "var(--aep-shell-rail)"`.
const shellLight = {
  "--aep-shell-rail": "#1B1F26",
  "--aep-shell-rail-text": "#C8CDD5",
  "--aep-shell-rail-active": "#FFFFFF",
  "--aep-shell-rail-hover": "rgba(255, 255, 255, 0.08)",
  "--aep-shell-chat": "#FAFBFC",
  "--aep-shell-user-bubble": "#EEF1F5",
  "--aep-shell-scrim": "rgba(20, 24, 30, 0.30)",
  "--aep-shell-card-shadow":
    "0 18px 48px rgba(20, 24, 30, 0.18), 0 2px 6px rgba(20, 24, 30, 0.08)",
};

const shellDark = {
  "--aep-shell-rail": "#0B0D11",
  "--aep-shell-rail-text": "#A9B1BD",
  "--aep-shell-rail-active": "#FFFFFF",
  "--aep-shell-rail-hover": "rgba(255, 255, 255, 0.08)",
  "--aep-shell-chat": "#12161B",
  "--aep-shell-user-bubble": "#222932",
  "--aep-shell-scrim": "rgba(0, 0, 0, 0.55)",
  "--aep-shell-card-shadow": "0 18px 48px rgba(0, 0, 0, 0.55), 0 2px 6px rgba(0, 0, 0, 0.4)",
};

export const aepTheme = createOxygenTheme({
  colorSchemes: {
    light: { palette: light },
    dark: { palette: dark },
  },
  components: {
    // Sentence case, as the prototype writes every action ("Start build",
    // "Finance posts every claim to one Xero organisation."); Oxygen's base
    // uppercases button labels.
    MuiButton: {
      styleOverrides: { root: { textTransform: "none" } },
    },
    MuiCssBaseline: {
      styleOverrides: {
        ":root": shellLight,
        "html[data-color-scheme='dark']": shellDark,
      },
    },
  },
});
