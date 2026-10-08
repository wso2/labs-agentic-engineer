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

// Fixture: the app shell's Sign out leads to a screen the manifest does not
// list. The user menu is closed, so only a theme that keeps its entries in the
// markup lets the render check see it.

import { AppShell, Button, Heading, Screen, defineApp } from "@wso2/prototype-kit";

function Home() {
  return (
    <AppShell id="shell" user={{ name: "Dana Lee" }} nav={[{ id: "nav.home", label: "Home", to: "screen.home" }]} account="screen.home" settings="screen.home" signOut="screen.goodbye">
      <Heading id="heading.home" text="Home" />
    </AppShell>
  );
}

function Admin() {
  return (
    <Screen>
      <Heading id="heading.admin" text="Administration" />
      <Button id="btn.home" label="Back home" to="screen.home" />
    </Screen>
  );
}

export default defineApp({ screens: { "screen.home": Home, "screen.admin": Admin } });
