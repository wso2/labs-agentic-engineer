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

/**
 * DependencyView — the renderer for a dependency's definition file,
 * `specs/design/dependencies/<name>/dependency.json`, the way DesignView
 * renders a component's design.json: what the dependency is, where it
 * stands, and the one thing the user can do about it.
 *
 * The FILE is the source (live doc ahead of the commit, else the committed
 * copy); the read model adds what a file cannot know — status, flags, who
 * uses it. Three things move a dependency forward, and all three live here
 * rather than in chat: **Select a provider** runs the guided flow
 * (`/resolve-dependency <name>`), whose first card asks which provider with
 * the definition's suggestions as options; **Resolve** runs the same flow
 * once a provider is chosen; **Provide interface** opens the modal that
 * lands a document beside the definition; and **Accept** records the user's
 * permission to build against an interface the agent wrote — the fallback
 * for one nobody authorized from the flow's own card. Both writes land
 * in git outside the room, so the view reports them (`onCommitted`) for the
 * owner to bring the room's copy up to date. Question cards the flow asks
 * render on the spec view around this pane, so the user never leaves it.
 */

import type React from "react";
import { useMemo, useState } from "react";
import { Alert, Box, Button, Chip, Stack, Typography } from "@wso2/oxygen-ui";
import { FileText, Plug, TriangleAlert } from "@wso2/oxygen-ui-icons-react";
import { dependencyFilePath } from "../api/designTree";
import { useAcceptDependencyAssumption } from "../api/queries";
import { parseDependencyDefinition } from "../lib/dependencyDefinition";
import type { DependencyState } from "../lib/dependencyStates";
import { ProvideInterfaceDialog } from "./ProvideInterfaceDialog";

const STYLE_LABEL: Record<string, string> = {
  "rest-api": "REST API",
  graphql: "GraphQL",
  sdk: "SDK",
};

const mono = { fontFamily: "monospace", fontSize: "0.875rem" } as const;

function SectionHeading({ children, action }: { children: string; action?: React.ReactNode }) {
  return (
    <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", mt: 3, mb: 1 }}>
      <Typography
        variant="overline"
        color="text.secondary"
        sx={{ display: "block", fontWeight: 700, letterSpacing: "0.08em" }}
      >
        {children}
      </Typography>
      {action}
    </Box>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <Box sx={{ display: "flex", gap: 2, alignItems: "baseline" }}>
      <Typography variant="body2" color="text.secondary" sx={{ minWidth: 96, flexShrink: 0 }}>
        {label}
      </Typography>
      <Typography component="span" sx={mono}>
        {value}
      </Typography>
    </Box>
  );
}

function FileLink({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <Button
      variant="text"
      size="small"
      startIcon={<FileText size={14} />}
      sx={{ alignSelf: "flex-start" }}
      onClick={onClick}
    >
      {label}
    </Button>
  );
}

export function DependencyView({
  projectName,
  name,
  definition,
  state,
  onOpenFile,
  onResolve,
  onReconsider,
  onCommitted,
}: {
  projectName: string;
  /** The directory's name — what components reference. */
  name: string;
  /** The dependency.json text. */
  definition: string;
  /** The folded read model, when it knows the name. */
  state: DependencyState | undefined;
  onOpenFile: (path: string) => void;
  /** Runs the guided flow for this dependency — its first card asks which provider. */
  onResolve: (name: string) => void;
  /** Opens a conversation about an already-resolved choice. */
  onReconsider: (name: string) => void;
  /** A write landed in the dependency's directory outside the room. */
  onCommitted?: ((name: string) => void) | undefined;
}) {
  const parsed = useMemo(() => parseDependencyDefinition(definition), [definition]);
  const accept = useAcceptDependencyAssumption(projectName);
  const [providing, setProviding] = useState(false);

  if (!parsed.ok) {
    return (
      <Box sx={{ p: 3 }}>
        <Alert severity="error">Couldn&apos;t parse this dependency&apos;s definition: {parsed.message}</Alert>
      </Box>
    );
  }
  const file = parsed.definition;
  const edge = state?.dependency;
  const resolved = edge?.status === "resolved";
  const awaitingAcceptance = edge?.reason === "needs-acceptance";
  const contractFile = file.contract ?? "";
  const sdkFile = file.sdk ?? "";
  const hasInterface = contractFile !== "";
  // The user chooses the service; until they have, the definition asks (the
  // Service card) and nothing about an interface applies.
  const chosen = Boolean(file.provider) || file.source === "org";
  // A document settles the interface, not the choice; a registered org
  // dependency's interface is the org record's; GraphQL schemas are not
  // fetched by the platform yet.
  const canProvide = chosen && file.source !== "org" && file.style !== "graphql";

  return (
    <Box sx={{ height: "100%", overflow: "auto", p: 3 }}>
      <Box sx={{ maxWidth: 960, mx: "auto" }}>
        {/* Header bar — kind and state sit above the name as an eyebrow row,
            the way a component's type and version do. */}
        <Box sx={{ display: "flex", alignItems: "center", gap: 1, mb: 1, flexWrap: "wrap" }}>
          <Chip size="small" icon={<Plug size={14} />} label="External dependency" />
          {state?.flags.map((f) => (
            <Chip key={f} size="small" variant="outlined" label={f} />
          ))}
          {state?.blocking && (
            <Chip
              size="small"
              color="warning"
              variant="outlined"
              icon={<TriangleAlert size={12} />}
              label={state.todo}
            />
          )}
          {resolved && !state?.blocking && (
            <Chip size="small" color="success" variant="outlined" label="Resolved" />
          )}
        </Box>
        <Stack direction="row" alignItems="center" justifyContent="space-between" spacing={2}>
          <Typography variant="h4" sx={{ fontWeight: 700, lineHeight: 1.2 }}>
            {name}
          </Typography>
          {resolved ? (
            // A reconsider is a conversation about a consumer's choice; with
            // no consumer there is nobody to reconsider for.
            state && state.usedBy.length > 0 && (
              <Button variant="outlined" onClick={() => onReconsider(name)}>
                Reconsider
              </Button>
            )
          ) : (
            // While no provider is chosen the Provider section's button is the
            // way in, so the header does not repeat it.
            chosen && (
              <Button variant="contained" onClick={() => onResolve(name)}>
                Resolve
              </Button>
            )
          )}
        </Stack>

        {/* Facts — labeled, so the provider reads as the provider and never as
            the name said twice. */}
        <Box sx={{ mt: 2, display: "flex", flexDirection: "column", gap: 0.5 }}>
          {file.style && <Fact label="Style" value={STYLE_LABEL[file.style] ?? file.style} />}
          {file.source === "org" && <Fact label="Source" value="Organization registry" />}
          {edge?.package && <Fact label="Package" value={edge.package} />}
        </Box>

        {file.description && (
          <>
            <SectionHeading>Description</SectionHeading>
            <Typography variant="body1" color="text.secondary">
              {file.description}
            </Typography>
          </>
        )}

        <SectionHeading>Used by</SectionHeading>
        {state && state.usedBy.length > 0 ? (
          <Stack direction="row" spacing={0.5} flexWrap="wrap">
            {state.usedBy.map((c) => (
              <Chip key={c} size="small" variant="outlined" label={c} />
            ))}
          </Stack>
        ) : (
          <Typography variant="body2" color="text.secondary">
            No component references this dependency yet.
          </Typography>
        )}

        {/* The dependency is the service the product needs; the provider is who
            supplies it. Chosen: the name, and the Interface below. Not chosen:
            one button into the resolve flow, whose first card asks which —
            the agent's suggestions as its options (ADR-0028). */}
        <SectionHeading
          action={
            !chosen && (
              <Button variant="contained" size="small" onClick={() => onResolve(name)}>
                Select a provider
              </Button>
            )
          }
        >
          Provider
        </SectionHeading>
        {chosen ? (
          <Typography variant="body1">{file.source === "org" ? "Registered by the organization" : file.provider}</Typography>
        ) : (
          <Typography variant="body2" color="text.secondary">
            None chosen yet. Select one and the agent sets it up: its interface, then the configuration keys.
          </Typography>
        )}
        {chosen && (
          <>
        <SectionHeading
          action={
            canProvide && (
              <Button size="small" variant="outlined" onClick={() => setProviding(true)}>
                {hasInterface ? "Replace interface" : "Provide interface"}
              </Button>
            )
          }
        >
          Interface
        </SectionHeading>
        {awaitingAcceptance && (
          <Box sx={{ border: 1, borderColor: "warning.main", borderRadius: 1, p: 2, mb: 2 }}>
            <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
              The agent wrote this interface from research
            </Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
              No published document was found. Accepting lets the build code against the
              agent&apos;s interface as written; it stays marked assumed everywhere until a real
              document replaces it.
            </Typography>
            <Stack direction="row" spacing={1}>
              <Button
                variant="contained"
                loading={accept.isPending}
                onClick={() => accept.mutate({ depName: name }, { onSuccess: () => onCommitted?.(name) })}
              >
                Accept the assumption
              </Button>
              {hasInterface && (
                <Button variant="text" onClick={() => onOpenFile(dependencyFilePath(name, contractFile))}>
                  Read it first
                </Button>
              )}
            </Stack>
            {accept.isError && (
              <Typography variant="body2" color="error" sx={{ mt: 1 }}>
                {accept.error instanceof Error ? accept.error.message : "The assumption was not recorded."}
              </Typography>
            )}
          </Box>
        )}
        {edge?.contractDerived && (
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            Derived from the provider&apos;s documentation — every operation cites its page; no published document exists to check it against.
          </Typography>
        )}
        {file.assumed && (
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            Assumed interface, accepted by {file.assumed.by} on {file.assumed.at}
            {file.assumed.note ? ` — ${file.assumed.note}` : ""}.
          </Typography>
        )}
        {hasInterface ? (
          <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
            <FileLink label={contractFile} onClick={() => onOpenFile(dependencyFilePath(name, contractFile))} />
            {file.provenance?.sourceUrl && <Fact label="Source" value={file.provenance.sourceUrl} />}
            {file.provenance?.sliced && <Fact label="Kept" value="the operations the design uses" />}
            {file.provenance?.fetchedAt && <Fact label="Read on" value={file.provenance.fetchedAt} />}
          </Box>
        ) : file.style === "sdk" && sdkFile ? (
          <Typography variant="body2" color="text.secondary">
            SDK only — no API document beside the manifest.
          </Typography>
        ) : (
            <Typography variant="body2" color="text.secondary">
              No interface on file yet.
            </Typography>
        )}
        {sdkFile && (
          <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5, mt: 1 }}>
            <FileLink label={sdkFile} onClick={() => onOpenFile(dependencyFilePath(name, sdkFile))} />
          </Box>
        )}
          </>
        )}
        {file.config && file.config.length > 0 && (
          <>
            <SectionHeading>Configuration</SectionHeading>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
              The settings every consumer reads. Values are supplied on the build, not here.
            </Typography>
            <Stack spacing={0.75}>
              {file.config.map((k) => (
                <Box key={k.key} sx={{ display: "flex", gap: 1, alignItems: "baseline", flexWrap: "wrap" }}>
                  <Chip size="small" label={k.key} color={k.secret ? "warning" : "default"} />
                  {k.description && (
                    <Typography variant="body2" color="text.secondary">
                      {k.description}
                    </Typography>
                  )}
                </Box>
              ))}
            </Stack>
          </>
        )}
      </Box>
      {canProvide && (
        <ProvideInterfaceDialog
          projectName={projectName}
          name={name}
          replacing={hasInterface}
          open={providing}
          onClose={() => setProviding(false)}
          onCommitted={() => onCommitted?.(name)}
        />
      )}
    </Box>
  );
}
