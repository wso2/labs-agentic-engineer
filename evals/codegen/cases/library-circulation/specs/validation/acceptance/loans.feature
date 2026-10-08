Feature: Loans

  @story-3
  Rule: A member may borrow an available copy, within limits

    Scenario: Borrowing an available copy
      Given a title has one available copy
      When Mia the member borrows the title
      Then Mia has one active loan for it with a due date 14 days from today
      And the title shows 0 copies available

    @negative
    Scenario: No copy is available
      Given a title has one copy and it is on loan to someone else
      When Mia the member tries to borrow the title
      Then Mia has no loan for it

    @negative
    Scenario: A member at the loan limit cannot borrow
      Given Mia the member has 5 active loans
      When she tries to borrow another available title
      Then Mia still has exactly 5 active loans

    @negative
    Scenario: A member with unpaid fees cannot borrow
      Given Mia the member has an unpaid fee of 1.00
      When she tries to borrow an available title
      Then Mia has no loan for that title

  @story-6
  Rule: A member sees only their own loans, with due dates

    Scenario: Viewing current loans
      Given Mia the member has borrowed "The Hobbit"
      When she opens her loans
      Then "The Hobbit" is listed with its due date

    @negative
    Scenario: Another member's loans are not shown
      Given Dan the member has borrowed "Dune"
      When Mia the member opens her loans
      Then "Dune" is not listed

  @story-10
  Rule: Only a librarian may check a copy out to a member

    Scenario: Checking a copy out
      Given Mia is a member and a copy is available
      When Lena the librarian checks the copy out to Mia
      Then Mia has an active loan for it due 14 days from today

    @negative
    Scenario: A member cannot check out to another member
      Given Dan is a member and a copy is available
      When Mia the member tries to check the copy out to Dan
      Then Dan has no loan for it

  @story-11
  Rule: Only a librarian may check a copy in

    Scenario: Returning a copy with no holds
      Given Mia has an active loan for a copy and nobody holds the title
      When Lena the librarian checks the copy in
      Then the loan is closed and the copy is available

    Scenario: Returning a copy that has a hold waiting
      Given Mia has an active loan and Dan has a waiting hold on the title
      When Lena the librarian checks the copy in
      Then Dan's hold is ready and the copy is not offered to anyone else

    @negative
    Scenario: A member cannot check a copy in
      Given Mia has an active loan for a copy
      When Mia tries to check the copy in
      Then the loan is still active
