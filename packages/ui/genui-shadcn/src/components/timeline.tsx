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

import type { GenUiPropsOf, GenUiRenderProps } from "@aep/ui-genui";
import { cn } from "cn";
import { Card, CardContent } from "#shadcn/components/ui/card";

// shadcn has no timeline or Gantt component; this one is composed from its
// Card and theme tokens (bars use the primary colour, faded while waiting).
export function GenUiAgentTimeline({
  props,
}: GenUiRenderProps<GenUiPropsOf<"AgentTimeline">>) {
  return (
    <Card>
      <CardContent className="flex flex-col gap-3">
        <div className="flex justify-between text-xs text-muted-foreground">
          <span>{props.total}</span>
          <span>solid · working &nbsp; faded · waiting on another agent</span>
        </div>
        {props.lanes.map((lane, index) => (
          <div
            key={`${index}-${lane.name}`}
            className="grid grid-cols-[minmax(0,16rem)_1fr_4.5rem] items-center gap-4"
          >
            <span
              className="truncate text-sm"
              title={lane.name}
              style={{ paddingLeft: `${(lane.depth ?? 0) * 0.75}rem` }}
            >
              {lane.name}
            </span>
            <div className="relative h-2">
              <div
                className={cn(
                  "absolute inset-y-0 rounded-full bg-primary",
                  lane.state === "waiting" && "opacity-40",
                )}
                style={{
                  left: `${lane.start}%`,
                  width: `${Math.max(lane.end - lane.start, 1)}%`,
                }}
              />
            </div>
            <span className="text-right text-xs text-muted-foreground tabular-nums">
              {lane.duration}
            </span>
          </div>
        ))}
      </CardContent>
    </Card>
  );
}
