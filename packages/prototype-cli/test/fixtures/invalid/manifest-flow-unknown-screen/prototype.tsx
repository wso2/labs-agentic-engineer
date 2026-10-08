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

// Fixture: the smallest prototype every check passes; the invalid source fixtures each change one thing in it.

import { Button, Heading, Screen, Text, defineApp, useDisplayState, useRole } from "@wso2/prototype-kit";

function Home() {
  const role = useRole();
  const state = useDisplayState();
  return (
    <Screen>
      <Heading id="heading.home" text="Home" />
      <Text id="text.state" text={state === "state.empty" ? "Nothing here yet" : "Welcome back"} />
      {role === "admin" ? <Button id="btn.admin" label="Administer" to="screen.admin" /> : null}
    </Screen>
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
