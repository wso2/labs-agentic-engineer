# Team Lunch Ordering — Domain model

The entities behind lunch-api: rounds opened by teammates and the items they
add before cutoff.

```mermaid
erDiagram
    ROUND ||--o{ ITEM : contains
    ROUND {
        string id PK
        string restaurant
        datetime cutoffAt
        string notes "optional"
        string openedBy
        datetime openedAt
        string status "open | closed"
        datetime closedAt
        string closedReason "cutoff | manual"
    }
    ITEM {
        string id PK
        string roundId FK
        string addedBy
        string description
        int quantity
        decimal price "per unit"
        datetime createdAt
        datetime updatedAt
    }
```

Exactly one `open` ROUND may exist at a time. The consolidated order is
derived from a closed ROUND's ITEMs (grouped totals, per-person breakdown,
grand total) — computed on read, never stored.
