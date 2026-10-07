Feature: Appointment booking

  @story-3
  Rule: A patient sees only available slots when browsing

    Scenario: Booked slots are not offered
      Given Dr. Rivera has two slots on a future date and one is booked by another patient
      When Pat the patient browses that doctor and date
      Then exactly one slot is listed

  @story-4
  Rule: A slot can be booked by at most one patient

    Scenario: Booking an available slot
      Given Dr. Rivera has an available slot at "09:30" on a future date
      When Pat the patient books that slot
      Then Pat has a booked appointment with Dr. Rivera at "09:30"

    @negative
    Scenario: A second patient cannot book the same slot
      Given Pat the patient has booked Dr. Rivera's slot at "09:30" on a future date
      When Sam the patient tries to book that same slot
      Then Sam has no appointment for that slot
      And the slot has exactly one appointment

  @story-5
  Rule: A patient may reschedule only their own appointment, and not within 2 hours of its start

    Scenario: Rescheduling to another available slot
      Given Pat the patient has an appointment next week at "09:30"
      And Dr. Rivera has another available slot next week at "11:00"
      When Pat reschedules to the "11:00" slot
      Then Pat's appointment is at "11:00"
      And the "09:30" slot is available again

    @negative
    Scenario: Rescheduling within 2 hours is refused
      Given Pat the patient has an appointment starting in 1 hour
      When Pat tries to reschedule it to another available slot
      Then Pat's appointment is unchanged

  @story-6
  Rule: A patient may cancel only their own appointment, and not within 2 hours of its start

    Scenario: Cancelling frees the slot
      Given Pat the patient has an appointment next week at "09:30"
      When Pat cancels it
      Then the appointment is cancelled
      And the "09:30" slot is available to other patients

    @negative
    Scenario: Cancelling within 2 hours is refused
      Given Pat the patient has an appointment starting in 1 hour
      When Pat tries to cancel it
      Then the appointment is still booked

    @negative
    Scenario: A patient cannot cancel another patient's appointment
      Given Sam the patient has an appointment next week at "09:30"
      When Pat the patient tries to cancel Sam's appointment
      Then Sam's appointment is still booked

  @story-7
  Rule: A patient sees only their own appointments, upcoming and past

    Scenario: Listing own appointments
      Given Pat the patient has one upcoming and one past appointment
      And Sam the patient has one upcoming appointment
      When Pat opens her appointments
      Then exactly 2 appointments are listed
      And all of them are Pat's
