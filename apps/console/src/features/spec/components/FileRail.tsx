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

import type { ReactNode } from "react";
import { createLink, useNavigate } from "@tanstack/react-router";
import { Box, ButtonBase, NativeSelect, Typography } from "@wso2/oxygen-ui";
import type { SourceDocument } from "../api/specModel";
import { PRODUCT_KEY, PRODUCT_WIDE_KEY } from "../model/files";
import { railState, type FeatureView } from "../model/workspace";
import { PHONE } from "../../shell/layout";
import { soft } from "./Tag";

const FileLink = createLink(ButtonBase);

function Eyebrow({ children }: { children: ReactNode }) {
  return (
    <Typography
      component="h3"
      sx={{
        px: 1.25,
        pt: 1.25,
        pb: 0.5,
        fontSize: "0.6875rem",
        letterSpacing: "0.08em",
        textTransform: "uppercase",
        fontWeight: 600,
        color: "text.secondary",
      }}
    >
      {children}
    </Typography>
  );
}

function RailItem({
  projectName,
  fileKey,
  current,
  sub = false,
  children,
}: {
  projectName: string;
  fileKey: string;
  current: string;
  sub?: boolean;
  children: ReactNode;
}) {
  const selected = fileKey === current;
  return (
    <FileLink
      to="/projects/$projectName/spec"
      params={{ projectName }}
      search={fileKey === PRODUCT_KEY ? {} : { file: fileKey }}
      aria-current={selected ? "page" : undefined}
      sx={{
        width: "100%",
        display: "flex",
        alignItems: "center",
        justifyContent: "flex-start",
        gap: 1,
        py: 0.75,
        pr: 1.25,
        pl: sub ? 2 : 1.25,
        borderRadius: 1.5,
        fontSize: "0.8125rem",
        textAlign: "start",
        color: selected ? "primary.main" : "text.primary",
        fontWeight: selected ? 600 : 400,
        bgcolor: selected ? soft("primary") : "transparent",
        "&:hover": { bgcolor: selected ? soft("primary") : "action.hover" },
      }}
    >
      {children}
    </FileLink>
  );
}

const nameSx = { minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" } as const;

function Name({ children }: { children: ReactNode }) {
  return (
    <Box component="span" className="rail-name" sx={nameSx}>
      {children}
    </Box>
  );
}

function FeatureItem({
  feature,
  projectName,
  current,
}: {
  feature: FeatureView;
  projectName: string;
  current: string;
}) {
  const state = railState(feature);
  return (
    <RailItem projectName={projectName} fileKey={feature.id} current={current} sub>
      <Box component="span" sx={{ fontFamily: "monospace", fontSize: "0.72rem", width: 22, flexShrink: 0, color: current === feature.id ? "primary.main" : "text.secondary" }}>
        {feature.id}
      </Box>
      <Name>{feature.name}</Name>
      <Box
        component="span"
        sx={{ ml: "auto", fontSize: "0.6875rem", fontWeight: 400, whiteSpace: "nowrap", color: state.tone ? `${state.tone}.main` : "text.secondary" }}
      >
        {state.label}
      </Box>
    </RailItem>
  );
}

/** The spec's files: the product page, each feature, product-wide, and the documents the user attached. */
export function FileRail({
  projectName,
  features,
  documents,
  current,
}: {
  projectName: string;
  features: FeatureView[];
  documents: SourceDocument[];
  current: string;
}) {
  return (
    <Box
      component="nav"
      aria-label="Spec files"
      sx={{
        width: 250,
        flexShrink: 0,
        borderRight: 1,
        borderColor: "divider",
        overflowY: "auto",
        px: 1,
        pt: 1.75,
        pb: 10,
        display: "flex",
        flexDirection: "column",
        gap: 0.125,
        [PHONE]: { display: "none" },
      }}
    >
      <Eyebrow>Requirements</Eyebrow>
      <RailItem projectName={projectName} fileKey={PRODUCT_KEY} current={current}>
        <Name>Product</Name>
      </RailItem>
      {features.length > 0 && (
        <Typography sx={{ fontSize: "0.75rem", color: "text.secondary", px: 1.25, pt: 0.75, pb: 0.25 }}>Features</Typography>
      )}
      {features.map((f) => (
        <FeatureItem key={f.id} feature={f} projectName={projectName} current={current} />
      ))}
      <RailItem projectName={projectName} fileKey={PRODUCT_WIDE_KEY} current={current}>
        <Name>Product-wide</Name>
      </RailItem>
      {documents.length > 0 && <Eyebrow>Documents</Eyebrow>}
      {documents.map((d) => (
        <RailItem key={d.id} projectName={projectName} fileKey={d.id} current={current}>
          <Box component="span" sx={nameSx}>{d.title}</Box>
        </RailItem>
      ))}
    </Box>
  );
}

/** At phone width the rail gives way to this: the same files, in a picker above the document. */
export function FilePicker({
  projectName,
  features,
  documents,
  current,
}: {
  projectName: string;
  features: FeatureView[];
  documents: SourceDocument[];
  current: string;
}) {
  const navigate = useNavigate();
  const options = [
    { key: PRODUCT_KEY, label: "Product" },
    ...features.map((f) => ({ key: f.id, label: `${f.id} ${f.name}` })),
    { key: PRODUCT_WIDE_KEY, label: "Product-wide" },
    ...documents.map((d) => ({ key: d.id, label: d.title })),
  ];
  return (
    <NativeSelect
      value={current}
      onChange={(e) =>
        void navigate({
          to: "/projects/$projectName/spec",
          params: { projectName },
          search: e.target.value === PRODUCT_KEY ? {} : { file: e.target.value },
        })
      }
      inputProps={{ "aria-label": "Open file" }}
      sx={{ display: "none", mb: 2.5, width: "100%", [PHONE]: { display: "inline-flex" } }}
    >
      {options.map((o) => (
        <option key={o.key} value={o.key}>
          {o.label}
        </option>
      ))}
    </NativeSelect>
  );
}
