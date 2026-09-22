library PatientLogic version '1.0.0'
using FHIR version '4.0.1'
include FHIRHelpers version '4.0.1' called FHIRHelpers

context Patient

define "Patient Given Name":
  First(Patient.name.given)

define "Is Adult":
  AgeInYears() >= 18
