# Worked example — risk register webapp wireframes

A complete `wireframes.dsl` for a three-screen desktop flow. Note the
rhythm: every screen opens with the same `navbar` and `sidebar` (consistent
chrome); content sits right of the sidebar (`x ≥ 280`) and below the navbar
(`y ≥ 80`); the screen's primary action is the bottom-most `button` (the
compiler attaches the `→(N)` flow marker to it).

```
// Risk register — three screens, desktop 1280x800

screen RiskDashboard
  navbar "RiskHub | Dashboard | Risks | Reports | Profile"
  sidebar "Overview | My Risks | All Registers | Audits | Settings"
  heading "Risk Overview" 280,80
  card "Open risks: 24" 280,130 300x120
  card "Overdue actions: 6" 600,130 300x120
  card "High severity: 3" 920,130 300x120
  heading "Recent activity" 280,280
  table "Risk | Owner | Severity | Updated" 280,320 940x320
  button "New risk" 1100,80 140x40

screen NewRisk
  navbar "RiskHub | Dashboard | Risks | Reports | Profile"
  sidebar "Overview | My Risks | All Registers | Audits | Settings"
  heading "New Risk" 280,80
  input "title" 280,130 640x36
  input "description" 280,182 640x72
  input "register (dropdown)" 280,270 300x36
  input "owner (dropdown)" 620,270 300x36
  input "impact" 280,322 300x36
  input "likelihood" 620,322 300x36
  button "Cancel" 280,400 140x40
  button "Create risk" 440,400 160x40

screen RiskDetail
  navbar "RiskHub | Dashboard | Risks | Reports | Profile"
  sidebar "Overview | My Risks | All Registers | Audits | Settings"
  heading "Risk: Unpatched edge servers" 280,80
  text "Severity: High — Owner: Platform team" 280,120
  card "Remediation progress" 280,160 640x140
  table "Action | Assignee | Due | Status" 280,330 940x240
  button "Update status" 280,610 160x40

flow
  RiskDashboard -> NewRisk
  NewRisk -> RiskDashboard
  RiskDashboard -> RiskDetail
  RiskDetail -> RiskDashboard
```

Checklist before finishing a wireframe file:

- Every screen from the requirements has a `screen` block; no extras.
- Chrome (`navbar`, `sidebar`) is identical across screens of the same app.
- Content stays inside the screen, right of the sidebar, below the navbar;
  nothing overlaps.
- Every screen is reachable in `flow`, and `flow` names exactly match
  `screen` names.
- Labels are content-bearing ("Open risks: 24", "register (dropdown)"), not
  placeholders — the wireframe is a communication artifact.
