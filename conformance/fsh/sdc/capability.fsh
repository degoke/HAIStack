Instance: sdc-r4
InstanceOf: CapabilityStatement
Usage: #definition
Title: "HAIStack SDC R4"
Description: "Capability statement for HAIStack Structured Data Capture on FHIR R4."
* url = "http://hl7.org/fhir/uv/sdc/CapabilityStatement/sdc-r4"
* version = "3.0.0"
* name = "SDCR4"
* status = #active
* date = "2026-01-01"
* publisher = "HAIStack"
* kind = #capability
* fhirVersion = #4.0.1
* format = #json
* rest.mode = #server
* rest.resource[0].type = #Questionnaire
* rest.resource[0].interaction[0].code = #read
* rest.resource[0].operation[0].name = "populate"
* rest.resource[0].operation[0].definition = "http://hl7.org/fhir/uv/sdc/OperationDefinition/Questionnaire-populate"
