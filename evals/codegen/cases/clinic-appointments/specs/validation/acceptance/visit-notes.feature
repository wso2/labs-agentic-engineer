Feature: Doctor's day and visit notes

  @story-10
  Rule: A doctor sees only their own appointments for the day

    Scenario: Viewing the day
      Given Dr. Rivera has 2 appointments today and Dr. Chen has 1 appointment today
      When Dr. Rivera opens her day
      Then exactly 2 appointments are listed
      And all of them are Dr. Rivera's

  @story-11
  Rule: A doctor writes visit notes on their own appointments

    Scenario: Writing a note
      Given Dr. Rivera has an appointment today with Pat the patient
      When Dr. Rivera saves the note "Sore throat, rest advised" on it
      Then Dr. Rivera reads the note "Sore throat, rest advised" on that appointment

    @negative
    Scenario: A doctor cannot write a note on another doctor's appointment
      Given Dr. Chen has an appointment today with Sam the patient
      When Dr. Rivera tries to save a note on it
      Then that appointment has no note

  @story-12
  Rule: Only doctors can read visit notes

    @negative
    Scenario: A receptionist cannot read a note
      Given Dr. Rivera has saved a note on Pat's appointment
      When Rita the receptionist tries to read that note
      Then the note text is not shown to Rita

    @negative
    Scenario: A patient cannot read their own visit note
      Given Dr. Rivera has saved a note on Pat's appointment
      When Pat the patient tries to read that note
      Then the note text is not shown to Pat
