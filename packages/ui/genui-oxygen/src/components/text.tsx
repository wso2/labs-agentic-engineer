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

import { PageTitle, Typography } from "@wso2/oxygen-ui";
import type { GenUiPropsOf, GenUiRenderProps } from "@aep/ui-genui";

export function GenUiHeading({
  props,
}: GenUiRenderProps<GenUiPropsOf<"Heading">>) {
  if (props.level === "page") {
    return (
      <PageTitle>
        <PageTitle.Header>{props.text}</PageTitle.Header>
      </PageTitle>
    );
  }
  return props.level === "subsection" ? (
    <Typography variant="subtitle1" component="h4">
      {props.text}
    </Typography>
  ) : (
    <Typography variant="h6" component="h3">
      {props.text}
    </Typography>
  );
}

export function GenUiText({ props }: GenUiRenderProps<GenUiPropsOf<"Text">>) {
  return (
    <Typography
      variant="body2"
      color={props.tone === "muted" ? "text.secondary" : "text.primary"}
    >
      {props.text}
    </Typography>
  );
}
