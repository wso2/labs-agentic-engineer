// Team Lunch Ordering — webapp wireframes (single flat team, no separate admin role)

screen OpenRound "Any signed-in teammate starts today's lunch round when none is open"
  navbar "Lunch | Today | History"
  row
    heading "Start Today's Lunch Round"
    right
    text "Signed in as J. Alvarez"
  text "No round is open right now — be the one to start it."
  input "Restaurant — e.g. Taco Fiesta"
  input "Cutoff time — e.g. 12:30 PM today"
  textarea "Notes (optional) — e.g. pickup at 12:45, pay opener via Venmo"
  row
    right
    button "Cancel"
    button "Open round" primary -> OpenRoundView

screen OpenRoundView "Teammates browse the open round and add their own items before cutoff"
  navbar "Lunch | Today | History"
  row
    heading "Taco Fiesta"
    badge "Open" success
  text "Cutoff: 12:30 PM (in 42 minutes) — opened by J. Alvarez"
  row
    right
    button "Close round now" -> ConsolidatedOrder
  split 60/40
    left
      heading "Items so far"
      table "Teammate | Item | Qty | Price"
        row "J. Alvarez | 2x Carnitas taco | 2 | $3.50"
        row "M. Chen | Burrito bowl | 1 | $9.00"
        row "S. Patel | Quesadilla | 1 | $7.50"
      row
        right
        button "Edit my item"
        button "Remove my item"
    right
      card "Add your item"
        input "Description — e.g. Carnitas taco"
        row
          input "Quantity — e.g. 2"
          input "Price — e.g. 3.50"
        button "Add item" primary
      text "You can only edit or remove items you added, before cutoff."

screen ConsolidatedOrder "The opener (and any teammate) view the closed round's consolidated order"
  navbar "Lunch | Today | History"
  breadcrumb "History / Sep 14 — Taco Fiesta"
  row
    heading "Taco Fiesta — Sep 14"
    badge "Closed" info
  text "Closed at 12:30 PM (cutoff) — opened by J. Alvarez"
  split 60/40
    left
      heading "Items, grouped"
      table "Item | Total qty"
        row "Carnitas taco | 4"
        row "Burrito bowl | 2"
        row "Quesadilla | 1"
      card "Grand total | $58.50 | across 3 teammates"
    right
      heading "Per-person breakdown"
      table "Teammate | Items | Owes"
        row "J. Alvarez | 2x Carnitas taco | $7.00"
        row "M. Chen | Burrito bowl | $9.00"
        row "S. Patel | Quesadilla | $7.50"

screen History "Everyone browses past closed rounds"
  navbar "Lunch | Today | History"
  heading "Past Rounds"
  table "Date | Restaurant | Opened by | Grand total" -> ConsolidatedOrder
    row "Sep 14 | Taco Fiesta | J. Alvarez | $58.50"
    row "Sep 13 | Pizza Palace | M. Chen | $71.20"
    row "Sep 12 | Sushi Go | S. Patel | $64.00"
