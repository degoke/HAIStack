// Package structuremap provides a minimal FHIR StructureMap execution engine
// for SDC questionnaire extraction.
//
// The engine executes StructureMap group rules against Questionnaire and
// QuestionnaireResponse inputs and produces FHIR resources for transaction
// bundle entries. Full FHIR Mapping Language transform parity is not a goal;
// the supported transforms cover common SDC extraction patterns (create, copy,
// uuid, cc, evaluate).
//
// Integrate with pkg/sdc via ExtractorRun or NewExtractor.
package structuremap
