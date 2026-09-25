# Scenario: this comment is not a scenario
Feature: Shapes that fool a naive scanner

  Rule: A docstring's contents are data, not Gherkin

    Scenario: Storing a note verbatim
      Given the note reads
        """
        Scenario: not a scenario
        Rule: not a rule
        """
      Then it is stored unchanged

  Rule: The grammar's synonyms are the same construct

    # Scenario: nor is this one
    Example: An Example is a Scenario by another name
      Given the parser has read this file
      Then it counts this as one scenario

  Rule: An outline is counted once, not once per row

    Scenario Outline: Adding an item of any quantity
      Given the list is empty
      When Priya adds "<item>" with quantity "<qty>"
      Then the list shows "<item>"

      Examples:
        | item | qty |
        | Milk | 1   |
        | Eggs | 2   |
