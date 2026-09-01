Instance: Patient-identifier-custom
InstanceOf: SearchParameter
Usage: #definition
Title: "Patient identifier-custom"
Description: "Token search on Patient.identifier for HAIStack core."
* url = "http://haistack.example.org/SearchParameter/Patient-identifier-custom"
* version = "1.0.0"
* name = "identifier-custom"
* status = #draft
* date = "2026-01-01"
* publisher = "HAIStack"
* description = "Search Patient by identifier."
* code = #identifier-custom
* base = #Patient
* type = #token
* expression = "Patient.identifier"
* xpathUsage = #normal
