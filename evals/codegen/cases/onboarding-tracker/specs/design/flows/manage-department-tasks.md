# Work a department's task queue

An IT Staff member (Facilities Staff follows the identical path on their own
department) works through their department's tasks across all new hires,
noticing overdue items directly in the app, and the HR Coordinator watches
overall progress on the dashboard.

```mermaid
sequenceDiagram
    actor ITStaff as IT Staff
    actor HRCoordinator as HR Coordinator
    participant onboarding-webapp
    participant onboarding-api

    ITStaff->>onboarding-webapp: open IT task queue
    onboarding-webapp->>onboarding-api: list IT tasks
    onboarding-api-->>onboarding-webapp: tasks (overdue flagged)
    ITStaff->>onboarding-webapp: mark task in-progress/complete
    onboarding-webapp->>onboarding-api: update task status
    onboarding-api-->>onboarding-webapp: updated

    HRCoordinator->>onboarding-webapp: open dashboard
    onboarding-webapp->>onboarding-api: list onboarding records with task status
    onboarding-api-->>onboarding-webapp: records (per-department completion, overdue flags)
    alt all tasks complete
        HRCoordinator->>onboarding-webapp: mark onboarding complete
        onboarding-webapp->>onboarding-api: complete onboarding record
        onboarding-api-->>onboarding-webapp: completed
    end
```
