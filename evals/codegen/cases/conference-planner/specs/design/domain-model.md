# Conference Planner — Domain Model

Events own rooms, sessions and talk proposals. Registrations (seats and waitlist) are owned by the registration service and reference events and sessions by id only.

```mermaid
erDiagram
    EVENT ||--o{ ROOM : has
    EVENT ||--o{ SESSION : schedules
    EVENT ||--o{ PROPOSAL : receives
    ROOM ||--o{ SESSION : hosts
    PROPOSAL ||--o{ SCORE : "is scored by"
    PROPOSAL |o--o| SESSION : "becomes"
    EVENT ||--o{ REGISTRATION : "attended via"
    SESSION ||--o{ REGISTRATION : "seats / waitlist"

    EVENT {
        string id PK
        string name
        string venue
        date startDate
        date endDate
    }
    ROOM {
        string id PK
        string eventId FK
        string name
        int capacity
    }
    SESSION {
        string id PK
        string eventId FK
        string roomId FK
        string title
        string speakerId
        datetime startTime
        datetime endTime
        int capacity
    }
    PROPOSAL {
        string id PK
        string eventId FK
        string speakerId
        string title
        string abstract
        string status "submitted, accepted, rejected"
        string sessionId FK
    }
    SCORE {
        string id PK
        string proposalId FK
        string reviewerId
        int value "1 to 5"
    }
    REGISTRATION {
        string id PK
        string userId
        string eventId FK
        string sessionId FK "null for event registration"
        string status "confirmed, waitlisted, cancelled"
        datetime joinedAt "orders the waitlist"
    }
```

- Session capacity is set by the events API and held by the registration service, which counts seats and orders the waitlist by `joinedAt`.
- A reviewer has at most one SCORE per proposal; the proposal's score is the average.
