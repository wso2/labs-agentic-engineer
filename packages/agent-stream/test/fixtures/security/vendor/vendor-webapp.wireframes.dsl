screen Orders "Orders my company is party to"
  navbar "Vendor Portal"
  sidebar "Orders | Invoices -> Invoices | Payments -> Payments"
  table "Order | Supplier | Status" -> ConfirmOrders
    row "PO-1004 | Acme | Awaiting"
  button "New order" primary -> PlaceOrder

screen PlaceOrder "Raise a purchase order"
  navbar "Vendor Portal"
  sidebar "Orders -> Orders | Invoices -> Invoices | Payments -> Payments"
  input "Supplier"
  button "Raise" primary -> Orders

screen ConfirmOrders "Orders waiting on my confirmation"
  navbar "Vendor Portal"
  sidebar "Orders -> Orders | Invoices -> Invoices | Payments -> Payments"
  table "Order | Buyer | Raised"
    row "PO-1004 | Globex | today"

screen Invoices "Invoices against these orders"
  navbar "Vendor Portal"
  sidebar "Orders -> Orders | Invoices | Payments -> Payments"
  table "Invoice | Amount | Status"
    row "INV-77 | 1200.00 | Submitted"

screen Payments "What has been paid out"
  navbar "Vendor Portal"
  sidebar "Orders -> Orders | Invoices -> Invoices | Payments"
  table "Payment | Invoice | Released"
    row "PAY-12 | INV-77 | today"
