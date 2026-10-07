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

import type { ReactNode } from "react";
import { Outlet, useChildMatches } from "@tanstack/react-router";
import { cardOfRoute } from "../scope";
import { BasePage } from "./BasePage";

/**
 * A project Page and the Cards it lists: what a Page's layout route renders.
 * The page is the base layer and stays mounted while one of its Cards (a
 * child route) is open over it, covered by the card's scrim. A child route
 * that draws no card (the page's own address) leaves it uncovered.
 */
export function PageWithCards({ children }: { children: ReactNode }) {
  const cardOpen = useChildMatches().some((m) => cardOfRoute(m.routeId) !== null);
  return (
    <>
      <BasePage covered={cardOpen}>{children}</BasePage>
      <Outlet />
    </>
  );
}
