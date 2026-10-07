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

import { Alert, Box, Button, Typography } from "@wso2/oxygen-ui";
import { useSettleDependency, type DesignDependency } from "../api/designModel";

/**
 * A dependency that blocks one feature's build, opened from the top of the
 * artifact list: what the feature needs, the question that settles it, and
 * its answers. Everything else designs and builds on without it.
 */
export function DependencyView({
  projectName,
  dependency,
  featureName,
}: {
  projectName: string;
  dependency: DesignDependency;
  featureName: string;
}) {
  const settle = useSettleDependency(projectName);
  return (
    <Box sx={{ display: "flex", flexDirection: "column", gap: 1.5, maxWidth: "64ch" }}>
      <Typography sx={{ fontSize: "0.6875rem", letterSpacing: "0.08em", textTransform: "uppercase", fontWeight: 600, color: "warning.main" }}>
        Blocking a build
      </Typography>
      <Typography component="h1" sx={{ fontSize: "1.375rem", fontWeight: 600, letterSpacing: "-0.01em" }}>
        {featureName} waits on {dependency.needs}
      </Typography>
      {dependency.answer ? (
        <Alert severity="success">
          Settled: {dependency.answer}. {featureName} no longer waits on {dependency.needs}.
        </Alert>
      ) : (
        <>
          <Typography variant="body2">{dependency.why}</Typography>
          <Typography sx={{ fontWeight: 600, fontSize: "0.9375rem" }}>{dependency.question}</Typography>
          <Box sx={{ display: "flex", gap: 1, flexWrap: "wrap" }}>
            {dependency.options.map((option, i) => (
              <Button
                key={option}
                variant={i === 0 ? "contained" : "outlined"}
                disabled={settle.isPending}
                onClick={() => settle.mutate({ featureId: dependency.featureId, answer: option })}
              >
                {option}
              </Button>
            ))}
          </Box>
          {dependency.options.length === 0 && (
            <Typography variant="body2" color="text.secondary">
              Settle it in the chat: tell the agent what to use, and it updates the design.
            </Typography>
          )}
          {settle.isError && <Alert severity="error">{settle.error.message}</Alert>}
        </>
      )}
    </Box>
  );
}
