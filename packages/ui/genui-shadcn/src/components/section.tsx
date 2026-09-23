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
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "#shadcn/components/ui/accordion";
import { Card } from "#shadcn/components/ui/card";
import { StateBadge } from "./status.js";

const ITEM = "section";

// A one-item Accordion on a Card: shadcn's collapsible titled panel.
export function GenUiSection({
  props,
  children,
}: GenUiRenderProps<GenUiPropsOf<"Section">>) {
  return (
    <Card className="px-4">
      <Accordion
        type="single"
        collapsible
        {...(props.collapsed ? {} : { defaultValue: ITEM })}
      >
        <AccordionItem value={ITEM}>
          <AccordionTrigger>
            <span className="flex flex-wrap items-center gap-2">
              <span>{props.title}</span>
              {props.summary ? (
                <span className="text-xs font-normal text-muted-foreground">
                  {props.summary}
                </span>
              ) : null}
              {props.badge ? (
                <StateBadge
                  state={{ label: props.badge.label, tone: props.badge.tone ?? "neutral" }}
                />
              ) : null}
            </span>
          </AccordionTrigger>
          <AccordionContent>
            <div className="flex flex-col gap-4">{children}</div>
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </Card>
  );
}
