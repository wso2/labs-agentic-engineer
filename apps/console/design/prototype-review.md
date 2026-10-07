# Prototype review

A clickable prototype of each designed web application, tried and annotated in
the browser. The agent makes it (`/prototype`), the person reviews it, and
Annotate feedback goes back to the chat as a typed batch. Why it is shaped this
way: [ADR-0001](decisions/ADR-0001-prototype-review-is-a-full-screen-overlay.md).
Code: `features/prototype/`.

## Where it shows

- **Prototype tab** (`PrototypeWorkspace`, route `projects/$p/prototype`): one
  centered entry per web application (a contract artifact whose `design.json`
  type is `web-application`) with status None, Ready, Invalid (the reason in a
  tooltip) or Revising, and one action: Review once a prototype renders, else
  Make prototype (Try again when invalid). No action while the first one is
  being made. Revising an existing prototype goes through Annotate in review;
  remaking it from a changed design is the Design tab's Make prototype. The tab
  shows a dot while a prototype is unreviewed.
- **Design actions:** Make prototype (`MakePrototypeButton`), hidden until the
  design has a web application. One app sends `/prototype <c>`, several send a
  bare `/prototype`. Disabled unless the chat can send.
- **Chat:** an "Open prototype" note after a `/prototype` exchange that wrote
  a prototype which is valid now (one component opens its review, several
  open the tab). It is derived (`model/note.ts`) from the log, which the
  history carries, and the room, so a reload and teammates see it too.

## Files

A prototype is `specs/design/components/<c>/prototype.{json,tsx}` in the room,
read with `useRoomFiles`. Status comes from the kit's `parseManifestJson`
(`model/prototypes.ts`). The agent's writes are gated by the kit's rules and the
render check; Go re-checks on save (see ADR-0042).

## Review

`PrototypeReview`: toolbar (Screen, Flow, Role, State, Reset data,
Preview/Annotate), the kit `PrototypeWindow` (browser chrome, read-only `prototype://<screen>` address bar; styled by the console with `--proto-window-*` Oxygen variables) around the `PrototypeFrame`, and in Annotate the
`FeedbackPanel`. A live revision replaces the manifest in place
(`MANIFEST_REPLACED`). Escape clears the selection, then closes. With focus in
the prototype, only the frame's `proto:escape` counts (sent when the prototype
left the key unused); the dialog ignores an Escape whose target is the frame.

- **Preview** acts: navigation, forms, mock data. **Annotate** only selects;
  selected elements are pinned and a request is typed against them (max 4000
  characters, 50 requests per batch, the kit CLI's limits).
- **Queue** (`model/feedback.ts`): per component, kept across close and reopen.
  It carries the hash of the revision of its first request. Requests,
  limits, pins and the hash are the kit's (`@wso2/prototype-kit/feedback`);
  the hash is plain JavaScript, so it works over plain HTTP.
- **Send all:** refused with the reason while the chat is not idle (queue
  kept). Otherwise `chatStore.send("/prototype <c>", {kind: "prototype",
  feedback})`; on success the queue clears, the overlay closes and the design
  data is read again. The batch is journaled with the turn and comes back in
  the history (`ConversationMessage.prototypeFeedback`); the chat row shows
  `feedbackSummary` of it (`model/summary.ts`: screen, role and state named
  from the manifest, element ids, text), not the wire text.
- **Reviewed dot:** shown while a valid prototype's current revision is
  unreviewed in this browser (not while a turn is revising it).
  `model/reviewed.ts` stores `{component: hash}` in localStorage
  (`aep:prototype-reviewed:<project>`); every access is guarded.

## Wire

`turnBody` puts `prototypeFeedback` (`PrototypeFeedbackInput`, from the
generated API types) on the JSON body with `collab: true`; the instruction is
the bare `/prototype` or `/prototype <c>` with the same component. The mock
(`mocks/fixtures/prototype.ts`) writes the manifest then the source, answers
feedback by number, and refuses a bad batch as Go does.

## Theme

`@wso2/prototype-theme-oxygen`, drawn with the console's own theme
(`@aep/ui-theme`, the `aepTheme` `main.tsx` applies), so the prototype and the
console cannot drift. Until the frame's app first draws, the review passes
`PrototypeFrame` a loading state ("Starting the prototype…") that covers the
frame, so a click while the runtime starts is not lost; a frame that never
draws shows the kit's "didn't start" error instead. The review passes the
console's resolved scheme (`useColorScheme`: the mode, or the system's under
System), so the prototype is light or dark with the console. Its frame has no
storage (opaque origin), so a script that touches `localStorage` in every
frame (for example a Playwright init script) raises there; limit it to the top
frame.
