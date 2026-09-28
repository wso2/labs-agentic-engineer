Feature: Personal greeting

  @story-1
  Rule: A visitor who submits a name receives a personal greeting

    Scenario: Submitting a name produces a greeting
      Given the greeting page is open
      When Alex submits the name "Alex"
      Then a greeting addressed to "Alex" is shown

  @story-2
  Rule: The greeting shows the submitted name exactly as entered

    Scenario: Capitalisation is preserved
      Given the greeting page is open
      When Alex submits the name "aLEX"
      Then the greeting shows the name exactly as "aLEX"

  @story-3
  Rule: An empty or whitespace-only name is refused, with the page saying why

    @negative
    Scenario: An empty name is refused
      Given the greeting page is open
      When Alex submits an empty name
      Then no greeting is shown
      And the page states why the submission was refused
