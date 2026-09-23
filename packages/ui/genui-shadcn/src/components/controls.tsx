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
import { Button } from "#shadcn/components/ui/button";

type Variant = NonNullable<GenUiPropsOf<"Button">["variant"]>;

const BUTTON_VARIANT: Record<Variant, "default" | "outline" | "destructive"> = {
  primary: "default",
  secondary: "outline",
  danger: "destructive",
};

export function GenUiButton({
  props,
  emit,
}: GenUiRenderProps<GenUiPropsOf<"Button">>) {
  // The div keeps the button at its own width inside a column stack.
  return (
    <div>
      <Button
        variant={BUTTON_VARIANT[props.variant ?? "primary"]}
        disabled={props.disabled ?? false}
        onClick={() => emit("press")}
      >
        {props.label}
      </Button>
    </div>
  );
}
