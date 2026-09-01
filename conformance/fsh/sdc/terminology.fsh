ValueSet: SDCQuestionnaireItemType
Id: sdc-questionnaire-item-type
Title: "SDC Questionnaire Item Type"
Description: "Item types used by HAIStack SDC questionnaires."
* ^url = "http://hl7.org/fhir/uv/sdc/ValueSet/sdc-questionnaire-item-type"
* ^version = "3.0.0"
* ^status = #active
* ^date = "2026-01-01"
* ^publisher = "HAIStack"
* include codes from system $item-type

CodeSystem: SDCExpressionLanguage
Id: sdc-expression-language
Title: "SDC Expression Language"
Description: "Expression languages supported by HAIStack SDC."
* ^url = "http://hl7.org/fhir/uv/sdc/CodeSystem/sdc-expression-language"
* ^version = "3.0.0"
* ^status = #active
* ^date = "2026-01-01"
* ^publisher = "HAIStack"
* ^caseSensitive = true
* ^content = #complete
* #text/fhirpath "FHIRPath" "FHIRPath expression."
* #text/cql "CQL" "Clinical Quality Language expression."
* #application/x-fhir-query "FHIR Query" "FHIR REST query."
