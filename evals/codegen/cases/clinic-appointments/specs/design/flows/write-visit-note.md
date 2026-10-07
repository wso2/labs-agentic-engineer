# Doctor day and visit note

A doctor reviews their day and records a visit note that no other role can read.

```mermaid
sequenceDiagram
    actor Doctor
    participant clinic-webapp
    participant clinic-api

    Doctor->>clinic-webapp: open day schedule
    clinic-webapp->>clinic-api: get my schedule for date
    clinic-api-->>clinic-webapp: appointments with status
    Doctor->>clinic-webapp: write visit note
    clinic-webapp->>clinic-api: save note for appointment
    alt not the doctor's appointment
        clinic-api-->>clinic-webapp: not found
    else
        clinic-api-->>clinic-webapp: note saved
    end
```
