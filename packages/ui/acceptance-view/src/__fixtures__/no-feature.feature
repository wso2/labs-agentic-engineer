# A file that declares no Feature:. Both readers must agree it carries nothing —
# the parser answers null and the checker answers an empty feature name.

  Rule: A rule with no feature above it
    Scenario: And a scenario under that
      Given nothing anchors them
      Then neither reader may invent a feature
