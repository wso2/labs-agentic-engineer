# Create a new hire's onboarding record

An HR Coordinator starts a new hire's onboarding; the tracker pre-populates
the checklist from standard department templates so IT and Facilities see
their tasks immediately.

```mermaid
sequenceDiagram
    actor HRCoordinator as HR Coordinator
    participant onboarding-webapp
    participant onboarding-api

    HRCoordinator->>onboarding-webapp: start new onboarding (name, start date)
    onboarding-webapp->>onboarding-api: create onboarding record
    onboarding-api->>onboarding-api: seed tasks from department templates
    onboarding-api-->>onboarding-webapp: record created (with seeded tasks)
    onboarding-webapp-->>HRCoordinator: onboarding detail, tasks by department
    HRCoordinator->>onboarding-webapp: add/edit/remove a task (exception)
    onboarding-webapp->>onboarding-api: update task list
    onboarding-api-->>onboarding-webapp: updated
```
