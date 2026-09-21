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
 * uses it. Everything the file says about the thing itself lives in its
 * `resource` block, which is the SAME shape whether the project defined the
 * resource or copied it from the organization's registry (`resource.ref`
 * set). So this view reads one block either way, and says where it came from
 * rather than rendering two different pages.
 *
 * Three things move a dependency forward, and all three live here rather than
 * in chat: **Select a provider** runs the guided flow
 * (`/resolve-dependency <name>`), whose first card asks which provider with
 * the definition's suggestions as options; **Resolve** runs the same flow
 * once a provider is chosen; **Provide interface** opens the modal that
 * lands a document beside the definition; and **Accept** records the user's
 * permission to build against an interface the agent wrote — the fallback
 * for one nobody authorized from the flow's own card. Both writes land
 * in git outside the room, so the view reports them (`onCommitted`) for the
 * owner to bring the room's copy up to date. Question cards the flow asks
 * render on the spec view around this pane, so the user never leaves it.
 *
 * While a turn holds the room every one of those buttons goes inert, with
 * the reason as its tooltip — the PRD's lenses do the same, from the same
 * gate (`busyReason`). A resolve fired then would be refused as a second
 * send; an upload or acceptance would land in the directory the agent may
 * be writing at that moment. Reading is never gated: the file links stay
 * live, since reading while the agent works is what the page is for.
 */

import type React from "react";
import { useMemo, useState } from "react";
import { Alert, Box, Button, Chip, Stack, Tooltip, Typography, type ButtonProps } from "@wso2/oxygen-ui";
import { FileText, Plug, TriangleAlert } from "@wso2/oxygen-ui-icons-react";
import { dependencyFilePath } from "../api/designTree";
import { useAcceptDependencyAssumption } from "../api/queries";
import { consumedAsLabel, parseDependencyDefinition } from "../lib/dependencyDefinition";
import type { DependencyState } from "../lib/dependencyStates";
import { ProvideInterfaceDialog } from "./ProvideInterfaceDialog";

/** What a copied field is, said once and quietly, wherever one is shown. */
const FROM_THE_ORGANIZATION = "from the organization";

const mono = { fontFamily: "monospace", fontSize: "0.875rem" } as const;

function SectionHeading({ children, action }: { children: React.ReactNode; action?: React.ReactNode }) {
  return (
    <Box sx={{ display: "flex", alignItems: "center", justifyContent: "space-between", mt: 3, mb: 1 }}>
      <Typography
        variant="overline"
        color="text.secondary"
        sx={{ display: "flex", alignItems: "center", gap: 1, fontWeight: 700, letterSpacing: "0.08em" }}
      >
        {children}
      </Typography>
      {action}
    </Box>
  );
}

/** The quiet mark on a field the platform copied from the registry record. */
function CopiedChip() {
  return <Chip size="small" variant="outlined" label={FROM_THE_ORGANIZATION} />;
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

/**
 * A button that changes the dependency — fires a turn or writes its
 * directory — so it goes inert while a turn holds the room, and says why.
 * The span carries the tooltip: a disabled button swallows pointer events.
 * With no reason it is the button as it was.
 */
function GatedButton({ busyReason, ...props }: ButtonProps & { busyReason: string }) {
  if (busyReason === "") return <Button {...props} />;
  return (
    <Tooltip title={busyReason}>
      <span>
        <Button {...props} disabled />
      </span>
    </Tooltip>
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

/** A date to read, from an RFC 3339 instant; anything else is shown as written. */
function onDay(at: string): string {
  return /^\d{4}-\d{2}-\d{2}/.test(at) ? at.slice(0, 10) : at;
}

/**
 * Where the contract document beside the definition came from, in the user's
 * words. `origin` replaced the markers the agent used to leave inside the
 * document itself, so this is the only place that says it.
 */
function originLabel(
  origin: string | undefined,
  accepted: { by: string; at: string } | undefined,
): string {
  switch (origin) {
    case "registry":
      return "copied from the organization's registry";
    case "provider":
      return "the provider's published document";
    case "derived":
      return "derived from the provider's documentation";
    case "assumed":
      return accepted
        ? `written by the agent, accepted by ${accepted.by} on ${onDay(accepted.at)}`
        : "written by the agent, not yet accepted";
    default:
      return "";
  }
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
  busyReason,
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
  /**
   * Why a change to the dependency would be refused right now — a turn holds
   * the room — or `""` when the buttons are live. The lenses' gate, verbatim,
   * so the tooltip reads the same everywhere on the spec view.
   */
  busyReason: string;
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
  const resource = file.resource;
  const edge = state?.dependency;
  const resolved = edge?.status === "resolved";
  const awaitingAcceptance = edge?.reason === "needs-acceptance";
  // A copy of a Registered External resource: the block was taken from the org
  // registry record, so its keys, instructions and document are the
  // organization's rather than this project's.
  const copied = Boolean(resource.ref);
  const contract = resource.contract;
  const contractFile = contract?.path ?? "";
  const hasInterface = contractFile !== "";
  // An `sdk` contract IS the manifest, so it needs no second link; a manifest
  // beside some other contract still gets one.
  const sdkFile = edge?.sdk && edge.sdk !== contractFile ? edge.sdk : "";
  const contractType = contract?.type ?? edge?.contractType;
  const contractOrigin = contract?.origin ?? edge?.contractOrigin;
  // The mark belongs to the DOCUMENT, not the resource: after Replace
  // interface the copy keeps its `ref` while the interface becomes the
  // project's own, and calling that one "from the organization" would
  // contradict the origin shown right under it. A file written before the
  // origin vocabulary records none, and a copy's document was the
  // organization's — that is the reading it had, and it keeps it.
  const contractFromRegistry =
    contractOrigin === "registry" || (copied && contractOrigin === undefined);
  const origin = originLabel(contractOrigin, contract?.accepted);
  // This project's copy first; a registry record carries its own.
  const provenance = file.provenance ?? resource.provenance;
  const provenanceSource = provenance?.sourceUrl ?? provenance?.registry;
  const consumedAs = consumedAsLabel(contractType);
  // The user chooses the service; until they have, the definition asks (the
  // Service card) and nothing about an interface applies. A copy has the
  // organization's choice already — once the platform has completed it. A
  // STUB (a ref and nothing else) is the agent's request for that copy: the
  // platform fills the block at save, so until the room reads the commit the
  // file names no provider and no interface, and the view says so instead of
  // dressing the name up as a provider.
  const stub = copied && !resource.provider;
  const chosen = Boolean(resource.provider);
  // A document settles the interface, not the choice. A copy may be given one
  // too — the organization's document is a starting point the project is
  // allowed to replace with its own. GraphQL schemas are not fetched by the
  // platform yet.
  const canProvide = chosen && contractType !== "graphql";
  // The registry record this copy names is gone (renamed, or never registered).
  const refMissing = Boolean(edge?.resourceRef) && edge?.reason === "needs-input";
  // The organization's document moved on since this copy was taken. The FILE
  // cannot know it — the platform compares the copy's hash with the registry's
  // and flags it on the read model. The view reports it; re-copying is not an
  // action the definition offers.
  const stale = (edge?.flags ?? []).includes("stale");

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
              <GatedButton busyReason={busyReason} variant="outlined" onClick={() => onReconsider(name)}>
                Reconsider
              </GatedButton>
            )
          ) : (
            // While no provider is chosen the Provider section's button is the
            // way in, so the header does not repeat it.
            chosen &&
            !refMissing && (
              <GatedButton busyReason={busyReason} variant="contained" onClick={() => onResolve(name)}>
                Resolve
              </GatedButton>
            )
          )}
        </Stack>

        {/* Facts — labeled, so the provider reads as the provider and never as
            the name said twice. Style is gone from the file: how the component
            talks to the system is the contract's type said plainly. */}
        <Box sx={{ mt: 2, display: "flex", flexDirection: "column", gap: 0.5 }}>
          {consumedAs && <Fact label="Consumed as" value={consumedAs} />}
          {copied && <Fact label="Source" value="Organization registry" />}
          {edge?.package && <Fact label="Package" value={edge.package} />}
        </Box>

        {refMissing && (
          <Alert
            severity="warning"
            sx={{ mt: 2 }}
            action={
              <GatedButton busyReason={busyReason} size="small" onClick={() => onResolve(name)}>
                Select a provider
              </GatedButton>
            }
          >
            The organization has no registered resource with this name
          </Alert>
        )}
        {stale && (
          <Alert severity="warning" sx={{ mt: 2 }}>
            The organization&apos;s document changed since this copy was made
          </Alert>
        )}

        {resource.description && (
          <>
            <SectionHeading>Description</SectionHeading>
            <Typography variant="body1" color="text.secondary">
              {resource.description}
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
            the agent's suggestions as its options (ADR-0028). A copy names the
            organization's provider, never the organization. */}
        <SectionHeading
          action={
            !chosen &&
            !stub && (
              <GatedButton
                busyReason={busyReason}
                variant="contained"
                size="small"
                onClick={() => onResolve(name)}
              >
                Select a provider
              </GatedButton>
            )
          }
        >
          Provider
        </SectionHeading>
        {chosen ? (
          <Typography variant="body1">{resource.provider}</Typography>
        ) : stub && !refMissing ? (
          <Typography variant="body2" color="text.secondary">
            The organization&apos;s record is being copied here; it appears once the design turn commits.
          </Typography>
        ) : stub ? (
          <Typography variant="body2" color="text.secondary">
            None. The organization has no registered resource with this name — select a provider above.
          </Typography>
        ) : (
          <Typography variant="body2" color="text.secondary">
            None chosen yet. Select one and the agent sets it up: its interface, then the configuration keys.
          </Typography>
        )}

        {resource.consumptionInstructions && (
          <>
            <SectionHeading action={copied ? <CopiedChip /> : undefined}>
              How the organization uses it
            </SectionHeading>
            <Typography variant="body1" color="text.secondary" sx={{ whiteSpace: "pre-wrap" }}>
              {resource.consumptionInstructions}
            </Typography>
          </>
        )}

        {chosen && (
          <>
        <SectionHeading
          action={
            <Stack direction="row" spacing={1} alignItems="center">
              {contractFromRegistry && <CopiedChip />}
              {canProvide && (
                <GatedButton
                  busyReason={busyReason}
                  size="small"
                  variant="outlined"
                  onClick={() => setProviding(true)}
                >
                  {hasInterface ? "Replace interface" : "Provide interface"}
                </GatedButton>
              )}
            </Stack>
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
              <GatedButton
                busyReason={busyReason}
                variant="contained"
                loading={accept.isPending}
                onClick={() => accept.mutate({ depName: name }, { onSuccess: () => onCommitted?.(name) })}
              >
                Accept the assumption
              </GatedButton>
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
        {hasInterface ? (
          // A contract is a WHOLE document now — there is no "kept: the
          // operations the design uses" any more.
          <Box sx={{ display: "flex", flexDirection: "column", gap: 0.5 }}>
            <FileLink label={contractFile} onClick={() => onOpenFile(dependencyFilePath(name, contractFile))} />
            {origin && <Fact label="Origin" value={origin} />}
            {provenanceSource && <Fact label="Source" value={provenanceSource} />}
            {provenance?.readOn && <Fact label="Read on" value={provenance.readOn} />}
          </Box>
        ) : sdkFile ? (
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
        {resource.config && resource.config.length > 0 && (
          <>
            <SectionHeading action={copied ? <CopiedChip /> : undefined}>Configuration</SectionHeading>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
              {copied
                ? "The settings every consumer reads. The organization holds their values."
                : "The settings every consumer reads. Values are supplied on the build, not here."}
            </Typography>
            <Stack spacing={0.75}>
              {resource.config.map((k) => (
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
