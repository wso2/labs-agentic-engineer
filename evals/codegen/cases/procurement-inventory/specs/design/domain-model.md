# Domain model

Items, stock levels and the stock ledger belong to the inventory service; purchase requests, receipts and approval limits belong to the procurement API.

```mermaid
erDiagram
    ITEM {
        string id PK
        string sku
        string name
        string unit
        number unitPrice
        int onHand
        int reorderLevel
    }
    LEDGER_ENTRY {
        string id PK
        string itemId FK
        int quantityChange
        int balanceAfter
        string reason
        string reference
        string createdBy
        datetime createdAt
    }
    PURCHASE_REQUEST {
        string id PK
        string requesterId
        string status
        number totalValue
        string decidedBy
        string decisionReason
        datetime createdAt
    }
    REQUEST_LINE {
        string id PK
        string requestId FK
        string itemId
        int quantity
        number unitPrice
        int receivedQuantity
    }
    GOODS_RECEIPT {
        string id PK
        string requestId FK
        string receivedBy
        datetime receivedAt
    }
    ITEM ||--o{ LEDGER_ENTRY : "records changes"
    PURCHASE_REQUEST ||--|{ REQUEST_LINE : contains
    PURCHASE_REQUEST ||--o{ GOODS_RECEIPT : "received by"
    ITEM ||--o{ REQUEST_LINE : "requested as (by id)"
```

- Request status: pending, approved, rejected, partially-received, received. *assumed*
- A receipt writes one ledger entry per received line in the inventory service, with the receipt id as the reference.
- The approval limit is one configured value, 5000, for all approvers; it is service configuration of the procurement API, not stored per approver.
- A new item starts with 0 on hand and no ledger entry; clerks add and edit items (name, unit, unit price, reorder level); the SKU is assigned by the inventory service.
