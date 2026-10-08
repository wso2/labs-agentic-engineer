# Domain model

Tickets are raised by employees, worked by support agents and watched by team leads. Each ticket has a comment thread and an SLA due time the API computes from its priority.

```mermaid
erDiagram
    USER_PROFILE ||--o{ TICKET : raises
    USER_PROFILE ||--o{ TICKET : "is assigned"
    USER_PROFILE ||--o{ COMMENT : writes
    TICKET ||--o{ COMMENT : has

    USER_PROFILE {
        string id PK "IDP subject"
        string displayName
    }
    TICKET {
        string id PK
        string title
        string description
        string status "open, triaged, in_progress, resolved, reopened"
        string priority "low, medium, high, critical"
        string requesterId FK
        string assigneeId FK "nullable"
        datetime slaDueAt "computed by the API from priority"
        boolean slaBreached "computed: unresolved past slaDueAt"
        datetime createdAt
        datetime updatedAt
        datetime resolvedAt "nullable"
    }
    COMMENT {
        string id PK
        string ticketId FK
        string authorId FK
        string body
        datetime createdAt
    }
```

- Status moves open → triaged → in_progress → resolved; resolved → reopened; a reopened ticket can be triaged again or started. *assumed*
- `slaDueAt` is creation time plus the priority's target, recomputed from creation time when priority changes. *assumed*
- `slaBreached` is derived, never stored; resolved tickets are not breached unless resolved after the due time. *assumed*
- USER_PROFILE is recorded from the signed-in identity on first use so agents can be listed for assignment.
