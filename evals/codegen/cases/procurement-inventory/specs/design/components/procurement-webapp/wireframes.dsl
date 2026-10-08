screen Catalogue "Requester: browse items available to request"
  navbar "Procurement"
  sidebar "Catalogue -> Catalogue | My requests -> MyRequests | Sign out"
  heading "Catalogue"
  row
    search "Search items"
    right
    button "New request" primary -> NewRequest
  table "SKU | Item | Unit | Unit price" -> NewRequest
    row "PEN-01 | Ballpoint pens | box | 4.50"
    row "PAP-A4 | A4 paper | ream | 6.20"

screen NewRequest "Requester: raise a purchase request"
  navbar "Procurement"
  sidebar "Catalogue -> Catalogue | My requests -> MyRequests | Sign out"
  heading "New purchase request"
  row
    select "Item"
    input "Quantity"
    button "Add line"
  table "Item | Quantity | Unit price | Line value"
    row "A4 paper | 10 | 6.20 | 62.00"
  card "Total value | 62.00 | sum of lines"
  row
    right
    button "Cancel"
    button "Submit request" primary -> MyRequests

screen MyRequests "Requester: status of my purchase requests"
  navbar "Procurement"
  sidebar "Catalogue -> Catalogue | My requests -> MyRequests | Sign out"
  heading "My requests"
  tabs "All | Pending | Approved | Rejected | Received"
  table "Request | Total | Status | Raised" -> MyRequestDetail
    row "PR-1001 | 62.00 | Pending | today"
    row "PR-0998 | 310.00 | Approved | yesterday"

screen MyRequestDetail "Requester: one of my requests"
  navbar "Procurement"
  sidebar "Catalogue -> Catalogue | My requests -> MyRequests | Sign out"
  breadcrumb "My requests / PR-0998"
  badge "Approved" success
  table "Item | Ordered | Received"
    row "A4 paper | 50 | 20"
  text "Decision: approved by an approver"

screen ApprovalQueue "Approver: pending requests to review"
  navbar "Procurement"
  sidebar "Approvals -> ApprovalQueue | Sign out"
  heading "Pending requests"
  card "Approval limit | 5000.00 | same for all approvers"
  table "Request | Requester | Total | Raised" -> ReviewRequest
    row "PR-1001 | requester | 62.00 | today"
    row "PR-1002 | requester | 6200.00 | today"

screen ReviewRequest "Approver: decide a request"
  navbar "Procurement"
  sidebar "Approvals -> ApprovalQueue | Sign out"
  breadcrumb "Pending requests / PR-1002"
  table "Item | Quantity | Unit price | Line value"
    row "A4 paper | 1000 | 6.20 | 6200.00"
  card "Total value | 6200.00 | above the approval limit of 5000"
  badge "Exceeds approval limit" warning
  textarea "Reason (for rejection)"
  row
    right
    button "Reject" danger -> ApprovalQueue
    button "Approve" primary -> ApprovalQueue

screen ReceivingQueue "Warehouse Clerk: approved orders awaiting goods"
  navbar "Procurement"
  sidebar "Receiving -> ReceivingQueue | Stock levels -> StockLevels | Low stock -> LowStock | Manage catalogue -> ManageCatalogue | Sign out"
  heading "Approved orders"
  table "Order | Total | Status" -> ReceiveGoods
    row "PR-0998 | 310.00 | Partially received"
    row "PR-1003 | 120.00 | Approved"

screen ReceiveGoods "Warehouse Clerk: receive goods against an order"
  navbar "Procurement"
  sidebar "Receiving -> ReceivingQueue | Stock levels -> StockLevels | Low stock -> LowStock | Manage catalogue -> ManageCatalogue | Sign out"
  breadcrumb "Approved orders / PR-0998"
  table "Item | Ordered | Received so far | Outstanding"
    row "A4 paper | 50 | 20 | 30"
  input "Quantity received now"
  row
    right
    button "Cancel"
    button "Confirm receipt" primary -> ReceivingQueue

screen StockLevels "Warehouse Clerk: on-hand stock for every item"
  navbar "Procurement"
  sidebar "Receiving -> ReceivingQueue | Stock levels -> StockLevels | Low stock -> LowStock | Manage catalogue -> ManageCatalogue | Sign out"
  heading "Stock levels"
  search "Search items"
  table "Item | On hand | Reorder level" -> ItemLedger
    row "A4 paper | 20 | 15"
    row "Ballpoint pens | 3 | 10"

screen ItemLedger "Warehouse Clerk: ledger of one item"
  navbar "Procurement"
  sidebar "Receiving -> ReceivingQueue | Stock levels -> StockLevels | Low stock -> LowStock | Manage catalogue -> ManageCatalogue | Sign out"
  breadcrumb "Stock levels / A4 paper"
  table "When | Change | Balance after | Reason | Receipt"
    row "today | +20 | 20 | goods receipt | RC-77"

screen LowStock "Warehouse Clerk: items below reorder level"
  navbar "Procurement"
  sidebar "Receiving -> ReceivingQueue | Stock levels -> StockLevels | Low stock -> LowStock | Manage catalogue -> ManageCatalogue | Sign out"
  heading "Low stock"
  table "Item | On hand | Reorder level" -> ItemLedger
    row "Ballpoint pens | 3 | 10"

screen ManageCatalogue "Warehouse Clerk: add and edit catalogue items"
  navbar "Procurement"
  sidebar "Receiving -> ReceivingQueue | Stock levels -> StockLevels | Low stock -> LowStock | Manage catalogue -> ManageCatalogue | Sign out"
  heading "Manage catalogue"
  row
    search "Search items"
    right
    button "Add item" primary -> ItemForm
  table "SKU | Item | Unit | Unit price | Reorder level" -> ItemForm
    row "PEN-01 | Ballpoint pens | box | 4.50 | 10"
    row "PAP-A4 | A4 paper | ream | 6.20 | 15"

screen ItemForm "Warehouse Clerk: add a new item or edit an existing one"
  navbar "Procurement"
  sidebar "Receiving -> ReceivingQueue | Stock levels -> StockLevels | Low stock -> LowStock | Manage catalogue -> ManageCatalogue | Sign out"
  breadcrumb "Manage catalogue / Item"
  heading "Item details"
  input "Name"
  input "Unit"
  input "Unit price"
  input "Reorder level"
  text "A new item starts with 0 in stock" muted
  row
    right
    button "Cancel"
    button "Save item" primary -> ManageCatalogue

flow "Browse and request"
  role "Requester"
  description "A requester browses the catalogue, raises a request and follows its status"
  Catalogue
  NewRequest
  MyRequests
  MyRequestDetail

flow "Approval queue"
  role "Approver"
  description "An approver reviews pending requests and decides them within the approval limit"
  ApprovalQueue
  ReviewRequest

flow "Receive goods"
  role "Warehouse Clerk"
  description "A clerk receives goods against approved orders and checks stock"
  ReceivingQueue
  ReceiveGoods
  StockLevels
  ItemLedger
  LowStock

flow "Manage catalogue"
  role "Warehouse Clerk"
  description "A clerk adds new items and edits existing ones"
  ManageCatalogue
  ItemForm
