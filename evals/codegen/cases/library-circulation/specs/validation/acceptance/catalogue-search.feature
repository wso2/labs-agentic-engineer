Feature: Catalogue search

  @story-1
  Rule: A member can find titles by title, author or subject

    Scenario: Searching by author
      Given the catalogue holds "Dune" by "Frank Herbert" and "Emma" by "Jane Austen"
      When Mia the member searches for "Herbert"
      Then the results include "Dune" and do not include "Emma"

    Scenario: Searching by subject
      Given the catalogue holds "Dune" with subject "Science fiction" and "Emma" with subject "Romance"
      When Mia the member searches for "Romance"
      Then the results include "Emma" and do not include "Dune"

    @negative
    Scenario: A search matching nothing
      Given the catalogue holds "Dune" by "Frank Herbert"
      When Mia the member searches for "zzzxqv"
      Then the results are empty

  @story-2
  Rule: A member sees whether a title has available copies

    Scenario: A title with a free copy
      Given "Dune" has two copies and one is on loan
      When Mia the member opens "Dune"
      Then it shows 1 copy available

    Scenario: A title with every copy on loan
      Given "Dune" has one copy and it is on loan
      When Mia the member opens "Dune"
      Then it shows 0 copies available
