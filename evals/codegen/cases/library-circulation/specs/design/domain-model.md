# Library Circulation — Domain Model

Titles and copies belong to the catalogue service; loans, holds and fees belong to the circulation API. Members are identified by the signed-in subject, not stored as a profile.

```mermaid
erDiagram
    TITLE ||--o{ COPY : has
    TITLE ||--o{ HOLD : "queued on"
    COPY ||--o{ LOAN : "lent as"
    LOAN ||--o| FEE : "may incur"
    MEMBER ||--o{ LOAN : borrows
    MEMBER ||--o{ HOLD : places
    MEMBER ||--o{ FEE : owes

    TITLE {
        string id PK
        string name
        string author
        string subject
        string isbn
    }
    COPY {
        string id PK
        string titleId FK
        string barcode
        string status "available | on-loan | on-hold | withdrawn"
    }
    LOAN {
        string id PK
        string copyId FK
        string titleId FK
        string memberId FK
        date checkedOutAt
        date dueDate
        date returnedAt
    }
    HOLD {
        string id PK
        string titleId FK
        string memberId FK
        datetime placedAt
        string status "waiting | ready | fulfilled | cancelled"
    }
    FEE {
        string id PK
        string loanId FK
        string memberId FK
        decimal amount
        string status "unpaid | paid"
    }
    MEMBER {
        string id PK "identity subject"
    }
```

Title and Copy live in the catalogue service; Loan, Hold and Fee in the circulation API. A loan's due date is checkout plus 14 days; a late return creates a Fee of 0.25 per day overdue (both assumed in the PRD). Loans store `copyId`/`titleId` as references into the catalogue.
