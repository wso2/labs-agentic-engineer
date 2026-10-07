# Domain model

Doctors publish Slots; an Appointment occupies exactly one Slot; a VisitNote belongs to one Appointment. People are identified by the sign-in subject, so there are no user tables.

```mermaid
erDiagram
    SLOT ||--o| APPOINTMENT : "booked by"
    APPOINTMENT ||--o| VISIT_NOTE : "has"

    SLOT {
        string id PK
        string doctorId
        string doctorName
        datetime startTime
        datetime endTime
        string status "AVAILABLE or BOOKED"
    }
    APPOINTMENT {
        string id PK
        string slotId FK
        string doctorId
        string doctorName
        string patientId
        string patientName
        string bookedBy "subject who booked"
        datetime startTime
        datetime endTime
        string status "BOOKED, CHECKED_IN or CANCELLED"
        datetime checkedInAt
    }
    VISIT_NOTE {
        string id PK
        string appointmentId FK
        string doctorId
        string text
        datetime updatedAt
    }
```

A slot is BOOKED by at most one active appointment; cancelling frees the slot. Rescheduling moves the appointment to another AVAILABLE slot and frees the old one. Cancel and reschedule are refused within 2 hours of the appointment start. Visit notes are exposed only through doctor-scoped operations.
