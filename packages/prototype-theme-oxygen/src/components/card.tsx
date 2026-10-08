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

/** The outlined card most components sit in, with an optional section title, as the console draws its panels. */

import { Card, CardContent, Typography } from "@wso2/oxygen-ui";
import type { ReactNode } from "react";

export function TitledCard({ title, children }: { title?: string | undefined; children?: ReactNode }) {
  return (
    <Card variant="outlined" component="section">
      {title && (
        <Typography variant="subtitle1" component="h3" sx={{ fontWeight: 600, px: 2, pt: 2 }}>
          {title}
        </Typography>
      )}
      <CardContent sx={{ "&:last-child": { pb: 2 } }}>{children}</CardContent>
    </Card>
  );
}
