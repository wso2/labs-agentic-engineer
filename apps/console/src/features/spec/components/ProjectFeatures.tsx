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

import { Box, Button, Skeleton } from "@wso2/oxygen-ui";
import { EmptyState } from "../../../components/EmptyState";
import { useSpecWorkspace } from "../useSpecWorkspace";
import { FeatureRows } from "./FeatureRows";

/** The overview's feature list: the same rows as the product page; a row opens the spec card on that feature. */
export function ProjectFeatures({ projectName }: { projectName: string }) {
  const { model, workspace } = useSpecWorkspace(projectName);
  if (model.isError) {
    return (
      <EmptyState
        bordered
        compact
        description="Couldn't load the features."
        action={
          <Button size="small" variant="outlined" onClick={() => void model.refetch()}>
            Try again
          </Button>
        }
      />
    );
  }
  if (!workspace) {
    return (
      <Box aria-busy>
        <Skeleton variant="rounded" height={132} />
      </Box>
    );
  }
  if (workspace.features.length === 0) {
    return <EmptyState bordered compact description="The agent proposes features once it has read your brief." />;
  }
  return <FeatureRows projectName={projectName} features={workspace.features} />;
}
