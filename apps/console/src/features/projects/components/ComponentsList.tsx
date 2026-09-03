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
import { Card, Chip, Tooltip } from "@wso2/oxygen-ui";
import { Box as BoxIcon, Boxes } from "@wso2/oxygen-ui-icons-react";
import { EmptyState } from "../../../components/EmptyState";
import type { components } from "../../../generated/aep-api";
import { useComponentEndpointUrl } from "../api/queries";
import { ComponentOpenApiDialog } from "./ComponentOpenApiDialog";
import { OverviewRow } from "./OverviewRow";

type Component = components["schemas"]["Component"];

// The component type is OpenChoreo's own ComponentType name, end-to-end.
const isWebApp = (c: Component) => c.type === "web-application";

const typeLabel = (c: Component) => (isWebApp(c) ? "Web app" : "Service");

/** The row itself, so both kinds render identically apart from where they go. */
function ComponentRow({
  component: c,
  last,
  onClick,
  href,
}: {
  component: Component;
  last: boolean;
  onClick?: (() => void) | undefined;
  href?: string | undefined;
}) {
  return (
    <OverviewRow
      icon={<BoxIcon size={18} />}
      title={c.displayName ?? c.name}
      trailing={
        <Chip
          size="small"
          variant="outlined"
          label={typeLabel(c)}
          sx={{ height: 22, flexShrink: 0, fontSize: "0.75rem" }}
        />
      }
      caption={c.description ?? undefined}
      last={last}
      onClick={onClick}
      href={href}
    />
  );
}

/**
 * A web app's row, which opens the RUNNING APP rather than a contract.
 *
 * A web app has no OpenAPI document, so this row used to go nowhere at all —
 * the one component on the page a reader most wants to click was the one that
 * did nothing. It has a public URL instead, and that URL is the answer to
 * "what did the platform actually build me".
 *
 * The URL comes from the component's DEPLOYMENTS, not from the component:
 * `Component.endpointUrl` is on the contract but the backend never fills it
 * (noted drift, #196), while the dev binding's resolved URL rides on
 * list-deployments. Its own component so the read is one hook per web app,
 * rather than a hook called inside a map.
 *
 * Until it deploys there is no URL, and the row then stays inert — a link
 * offered before the app is up is a link to a 404.
 */
function WebAppRow({
  projectName,
  component,
  last,
}: {
  projectName: string;
  component: Component;
  last: boolean;
}) {
  const url = useComponentEndpointUrl(projectName, component.name).data;
  if (!url) return <ComponentRow component={component} last={last} />;
  return (
    <Tooltip title="Open the app" placement="left">
      <div>
        <ComponentRow component={component} last={last} href={url} />
      </div>
    </Tooltip>
  );
}

/**
 * The project's components.
 *
 * These were one bordered card per component, with an avatar carrying the first
 * letter of a name printed right beside it. They are now `OverviewRow`s, which
 * is the build page's task row: a reader moving between the two pages should
 * not have to work out what a row is twice.
 *
 * The Dependencies list below uses the same row for the same reason. One column
 * running two densities was what made this page feel loose.
 *
 * Deliberately state-free. A component's build state used to be rolled up from
 * its tasks, but an issue no longer names a component — issue bodies are prose
 * the platform writes and never reads back — so the roll-up had no input left.
 * What is running lives on the deployments board, which reads the cluster.
 */
export function ComponentsList({
  projectName,
  items,
}: {
  projectName: string;
  items: Component[];
}) {
  const [contractComponent, setContractComponent] = useState<string | null>(
    null,
  );

  if (items.length === 0) {
    return (
      <EmptyState
        bordered
        icon={<Boxes size={28} />}
        title="No components yet"
        description="Components are the services and apps your design is made of, and they appear as agents build them."
      />
    );
  }

  return (
    <>
      <Card variant="outlined">
        {items.map((c, i) =>
          isWebApp(c) ? (
            <WebAppRow
              key={c.name}
              projectName={projectName}
              component={c}
              last={i === items.length - 1}
            />
          ) : (
            <Tooltip key={c.name} title="View API contract" placement="left">
              <div>
                <ComponentRow
                  component={c}
                  last={i === items.length - 1}
                  onClick={() => setContractComponent(c.name)}
                />
              </div>
            </Tooltip>
          ),
        )}
      </Card>
      <ComponentOpenApiDialog
        projectName={projectName}
        componentName={contractComponent}
        onClose={() => setContractComponent(null)}
      />
    </>
  );
}
