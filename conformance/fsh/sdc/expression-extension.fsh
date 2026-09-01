Extension: SDCQuestionnaireExpression
Id: sdc-questionnaire-expression
Title: "SDC Questionnaire Expression"
Description: "Expression used to populate or calculate a questionnaire item."
* ^url = "http://hl7.org/fhir/uv/sdc/StructureDefinition/sdc-questionnaire-expression"
* ^version = "3.0.0"
* ^name = "SDCQuestionnaireExpression"
* ^status = #active
* ^date = "2026-01-01"
* ^publisher = "HAIStack"
* ^context[+].type = #element
* ^context[=].expression = "Questionnaire.item"
* . 0..1
* . ^short = "SDC item expression"
* value[x] only Expression
* value[x] 1..1
