screen Catalogue "Members search titles and see availability"
  navbar "Library"
  sidebar "Catalogue -> Catalogue | My loans -> MyLoans | My holds -> MyHolds | My fees -> MyFees | Sign out"
  heading "Catalogue"
  row
    search "Search title, author or subject"
    button "Search"
  table "Title | Author | Subject | Available" -> TitleDetail
    row "The Hobbit | J.R.R. Tolkien | Fantasy | 2"
    row "Dune | Frank Herbert | Science fiction | 0"

screen TitleDetail "A title with its availability, to borrow or hold"
  navbar "Library"
  sidebar "Catalogue -> Catalogue | My loans -> MyLoans | My holds -> MyHolds | My fees -> MyFees | Sign out"
  breadcrumb "Catalogue / Dune"
  heading "Dune"
  text "Frank Herbert · Science fiction"
  badge "0 copies available" warning
  row
    button "Place hold" -> MyHolds
    right
    button "Borrow" primary -> MyLoans

screen MyLoans "A member's current loans and due dates"
  navbar "Library"
  sidebar "Catalogue -> Catalogue | My loans -> MyLoans | My holds -> MyHolds | My fees -> MyFees | Sign out"
  heading "My loans"
  table "Title | Checked out | Due | Status"
    row "The Hobbit | 2 Mar | 16 Mar | On loan"
    row "Emma | 20 Feb | 6 Mar | Overdue"

screen MyHolds "A member's holds and their queue position"
  navbar "Library"
  sidebar "Catalogue -> Catalogue | My loans -> MyLoans | My holds -> MyHolds | My fees -> MyFees | Sign out"
  heading "My holds"
  table "Title | Position | Status"
    row "Dune | 2 | Waiting"
  button "Cancel selected hold"

screen MyFees "A member's fees"
  navbar "Library"
  sidebar "Catalogue -> Catalogue | My loans -> MyLoans | My holds -> MyHolds | My fees -> MyFees | Sign out"
  heading "My fees"
  card "Unpaid total | 1.00 | pay at the desk"
  table "Loan | Amount | Status"
    row "Emma | 1.00 | Unpaid"

screen ManageTitles "Librarians manage the titles in the catalogue"
  navbar "Library"
  sidebar "Titles -> ManageTitles | Check out -> Checkout | Check in -> Checkin | Holds queue -> HoldsQueue | Fees -> FeesDesk | Sign out"
  row
    heading "Titles"
    right
    button "Add title" primary -> EditTitle
  search "Search titles"
  table "Title | Author | Copies" -> TitleCopies
    row "The Hobbit | J.R.R. Tolkien | 3"
    row "Dune | Frank Herbert | 2"

screen EditTitle "Add or edit a title"
  navbar "Library"
  sidebar "Titles -> ManageTitles | Check out -> Checkout | Check in -> Checkin | Holds queue -> HoldsQueue | Fees -> FeesDesk | Sign out"
  heading "Title"
  input "Name"
  input "Author"
  input "Subject"
  input "ISBN"
  row
    button "Remove title" danger
    right
    button "Cancel" -> ManageTitles
    button "Save" primary -> ManageTitles

screen TitleCopies "Copies of one title"
  navbar "Library"
  sidebar "Titles -> ManageTitles | Check out -> Checkout | Check in -> Checkin | Holds queue -> HoldsQueue | Fees -> FeesDesk | Sign out"
  breadcrumb "Titles / Dune"
  row
    heading "Copies"
    right
    button "Add copy" primary -> EditCopy
  table "Barcode | Status" -> EditCopy
    row "DUN-001 | Available"
    row "DUN-002 | On loan"

screen EditCopy "Add or edit a copy, or withdraw it"
  navbar "Library"
  sidebar "Titles -> ManageTitles | Check out -> Checkout | Check in -> Checkin | Holds queue -> HoldsQueue | Fees -> FeesDesk | Sign out"
  heading "Copy"
  input "Barcode"
  row
    button "Withdraw copy" danger
    right
    button "Cancel" -> TitleCopies
    button "Save" primary -> TitleCopies

screen Checkout "Check a copy out to a member"
  navbar "Library"
  sidebar "Titles -> ManageTitles | Check out -> Checkout | Check in -> Checkin | Holds queue -> HoldsQueue | Fees -> FeesDesk | Sign out"
  heading "Check out"
  input "Member id"
  input "Copy barcode"
  button "Check out"
  text "Due date is 14 days from today" muted

screen Checkin "Check a copy in"
  navbar "Library"
  sidebar "Titles -> ManageTitles | Check out -> Checkout | Check in -> Checkin | Holds queue -> HoldsQueue | Fees -> FeesDesk | Sign out"
  heading "Check in"
  input "Copy barcode"
  button "Check in"
  card "Late fee | 1.00 | shown when the return is overdue"

screen HoldsQueue "Librarians work the holds queue per title"
  navbar "Library"
  sidebar "Titles -> ManageTitles | Check out -> Checkout | Check in -> Checkin | Holds queue -> HoldsQueue | Fees -> FeesDesk | Sign out"
  heading "Holds queue"
  search "Filter by title"
  table "Title | Member | Placed | Status"
    row "Dune | m-1042 | 1 Mar | Ready"
    row "Dune | m-2210 | 2 Mar | Waiting"
  button "Cancel hold" danger

screen FeesDesk "Librarians see fees and mark them paid"
  navbar "Library"
  sidebar "Titles -> ManageTitles | Check out -> Checkout | Check in -> Checkin | Holds queue -> HoldsQueue | Fees -> FeesDesk | Sign out"
  heading "Fees"
  search "Filter by member"
  table "Member | Loan | Amount | Status"
    row "m-1042 | Emma | 1.00 | Unpaid"
  button "Mark paid"

flow "Find and borrow"
  role "Member"
  description "A member searches, opens a title and borrows it"
  Catalogue
  TitleDetail
  MyLoans

flow "Holds and fees"
  role "Member"
  description "A member reviews holds and fees"
  MyHolds
  MyFees

flow "Manage catalogue"
  role "Librarian"
  description "A librarian maintains titles and copies"
  ManageTitles
  EditTitle
  TitleCopies
  EditCopy

flow "Circulation desk"
  role "Librarian"
  description "A librarian checks items out and in and settles fees"
  Checkout
  Checkin
  FeesDesk

flow "Work holds"
  role "Librarian"
  description "A librarian works the holds queue"
  HoldsQueue
