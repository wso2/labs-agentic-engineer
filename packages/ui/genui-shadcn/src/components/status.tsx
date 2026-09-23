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

import type {
  GenUiPropsOf,
  GenUiRenderProps,
  GenUiStateLabel,
  GenUiTone,
} from "@aep/ui-genui";
import { Alert, AlertDescription, AlertTitle } from "#shadcn/components/ui/alert";
import { Badge } from "#shadcn/components/ui/badge";
import { Progress } from "#shadcn/components/ui/progress";

type BadgeVariant = "default" | "secondary" | "destructive" | "outline";

// shadcn's Badge has no success or warning colour, so those tones map to its
// nearest stock variants rather than to custom colours.
const BADGE_VARIANT: Record<GenUiTone, BadgeVariant> = {
  neutral: "secondary",
  info: "outline",
  success: "default",
  warning: "outline",
  error: "destructive",
};

export function StateBadge({ state }: { state: GenUiStateLabel }) {
  return <Badge variant={BADGE_VARIANT[state.tone]}>{state.label}</Badge>;
}

export function GenUiStatusChip({
  props,
}: GenUiRenderProps<GenUiPropsOf<"StatusChip">>) {
  // The div keeps the badge at its own width inside a column stack.
  return (
    <div>
      <Badge variant={BADGE_VARIANT[props.tone ?? "neutral"]}>{props.label}</Badge>
    </div>
  );
}

export function GenUiAlert({ props }: GenUiRenderProps<GenUiPropsOf<"Alert">>) {
  return (
    <Alert variant={props.tone === "error" ? "destructive" : "default"}>
      {props.title ? <AlertTitle>{props.title}</AlertTitle> : null}
      <AlertDescription>{props.message}</AlertDescription>
    </Alert>
  );
}

export function GenUiProgress({
  props,
}: GenUiRenderProps<GenUiPropsOf<"Progress">>) {
  return (
    <div className="flex flex-col gap-2">
      {props.label ? (
        <p className="text-sm text-muted-foreground">{props.label}</p>
      ) : null}
      <Progress value={props.value} aria-label={props.label ?? "Progress"} />
    </div>
  );
}
