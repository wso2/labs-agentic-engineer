Feature: Catalogue management

  @story-8
  Rule: Only a librarian may add, edit and remove titles

    Scenario: Adding a title
      Given Lena the librarian is signed in
      When she adds a title named uniquely for this run by "Frank Herbert"
      Then a member searching for that name finds the title

    Scenario: Editing a title's author
      Given Lena the librarian has added a title by "F. Herbert"
      When she changes its author to "Frank Herbert"
      Then the title shows the author "Frank Herbert"

    Scenario: Removing a title
      Given Lena the librarian has added a title with no loans
      When she removes it
      Then a member searching for that name finds nothing

    @negative
    Scenario: A member cannot add a title
      Given Mia the member is signed in
      When she tries to add a title
      Then the catalogue has no new title

  @story-9
  Rule: Only a librarian may add, edit and withdraw copies

    Scenario: Adding a copy makes it available
      Given Lena the librarian has added a title with no copies
      When she adds a copy with barcode unique to this run
      Then the title shows 1 copy available

    Scenario: Withdrawing a copy
      Given Lena the librarian has added a title with one available copy
      When she withdraws that copy
      Then the title shows 0 copies available

    @negative
    Scenario: A member cannot withdraw a copy
      Given a title has one available copy
      When Mia the member tries to withdraw it
      Then the title still shows 1 copy available
