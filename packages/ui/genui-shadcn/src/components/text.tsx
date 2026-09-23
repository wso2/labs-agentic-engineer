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

// shadcn ships typography as classes, not components; these are the ones its
// Typography page gives for h2 (without its underline), h3, h4, and muted text.
export function GenUiHeading({
  props,
}: GenUiRenderProps<GenUiPropsOf<"Heading">>) {
  if (props.level === "page") {
    return <h2 className="scroll-m-20 text-3xl font-semibold tracking-tight">{props.text}</h2>;
  }
  return props.level === "subsection" ? (
    <h4 className="scroll-m-20 text-xl font-semibold tracking-tight">{props.text}</h4>
  ) : (
    <h3 className="scroll-m-20 text-2xl font-semibold tracking-tight">{props.text}</h3>
  );
}

export function GenUiText({ props }: GenUiRenderProps<GenUiPropsOf<"Text">>) {
  return (
    <p className={props.tone === "muted" ? "text-sm text-muted-foreground" : "text-sm"}>
      {props.text}
    </p>
  );
}
