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
import {
  Avatar,
  Box,
  Divider,
  IconButton,
  ListItemIcon,
  ListItemText,
  ListSubheader,
  Menu,
  MenuItem,
  Tooltip,
  Typography,
  UserMenu,
  useColorScheme,
} from "@wso2/oxygen-ui";
import { Check, LogOut, Monitor, Moon, Sun } from "@wso2/oxygen-ui-icons-react";
import { useSession } from "../../../auth/SessionContext";

type Mode = "light" | "dark" | "system";

const MODES: { mode: Mode; label: string; icon: typeof Sun }[] = [
  { mode: "light", label: "Light", icon: Sun },
  { mode: "dark", label: "Dark", icon: Moon },
  { mode: "system", label: "System", icon: Monitor },
];

/**
 * The avatar at the foot of the rail and its menu: who is signed in and in
 * which org, the colour scheme (Oxygen's own mode, persisted by it), and sign
 * out through the session.
 *
 * Oxygen's `UserMenu` is not used whole: it anchors below and to the left of
 * its trigger (a header's top-right corner), which from the rail's bottom-left
 * opens off screen. Its header section is reused as is.
 */
export function RailUserMenu() {
  const { user, orgHandle, signOut } = useSession();
  const { mode, setMode } = useColorScheme();
  const [anchor, setAnchor] = useState<HTMLElement | null>(null);
  const open = anchor !== null;
  const close = () => setAnchor(null);

  return (
    <>
      <Tooltip title="Account" placement="right">
        <IconButton
          aria-label={`Account: ${user.name}`}
          aria-haspopup="menu"
          aria-expanded={open || undefined}
          aria-controls={open ? "rail-user-menu" : undefined}
          onClick={(e) => setAnchor(e.currentTarget)}
          sx={{ mt: 0.75, p: 0.5 }}
        >
          <Avatar
            sx={{
              width: 28,
              height: 28,
              fontSize: "0.75rem",
              fontWeight: 600,
              bgcolor: "info.main",
              color: "info.contrastText",
            }}
          >
            {user.name.charAt(0).toUpperCase()}
          </Avatar>
        </IconButton>
      </Tooltip>
      <Menu
        id="rail-user-menu"
        anchorEl={anchor}
        open={open}
        onClose={close}
        anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
        transformOrigin={{ vertical: "bottom", horizontal: "left" }}
        slotProps={{ paper: { sx: { minWidth: 260, ml: 1 } } }}
      >
        <UserMenu.Header name={user.name} email={user.email} {...(user.role ? { role: user.role } : {})} />
        <Box sx={{ px: 2, py: 1.25 }}>
          <Typography variant="caption" color="text.secondary" component="div">
            Organization
          </Typography>
          <Typography variant="body2" sx={{ fontWeight: 500 }}>
            {orgHandle ?? "Default organization"}
          </Typography>
        </Box>
        <Divider />
        <ListSubheader sx={{ lineHeight: 2.5, bgcolor: "transparent" }}>Theme</ListSubheader>
        {MODES.map(({ mode: value, label, icon: Icon }) => (
          <MenuItem
            key={value}
            role="menuitemradio"
            aria-checked={mode === value}
            selected={mode === value}
            // Stays open: the reader sees the scheme change and can try another.
            onClick={() => setMode(value)}
          >
            <ListItemIcon>
              <Icon size={18} />
            </ListItemIcon>
            <ListItemText primary={label} />
            {mode === value && <Check size={16} />}
          </MenuItem>
        ))}
        <Divider />
        <MenuItem
          onClick={() => {
            close();
            signOut();
          }}
        >
          <ListItemIcon>
            <LogOut size={18} />
          </ListItemIcon>
          <ListItemText primary="Sign out" />
        </MenuItem>
      </Menu>
    </>
  );
}
