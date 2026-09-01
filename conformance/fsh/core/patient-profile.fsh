Profile: HAIPatient
Parent: Patient
Id: hai-patient
Title: "HAIStack Patient"
Description: """Patient profile for Health AI Stack core. At least one identifier
with both system and value is required so edge and offline deployments can
correlate records without relying on a central MPI."""
* ^url = "http://haistack.example.org/fhir/StructureDefinition/hai-patient"
* ^version = "1.0.0"
* ^status = #active
* ^date = "2026-01-01"
* ^publisher = "HAIStack"
* ^fhirVersion = #4.0.1
* identifier 1..* MS
* identifier.system 1..1 MS
* identifier.value 1..1 MS
