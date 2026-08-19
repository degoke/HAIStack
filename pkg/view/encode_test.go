package view_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/proto"
	protor4 "github.com/degoke/health-ai-stack/pkg/proto/r4"
	"github.com/degoke/health-ai-stack/pkg/view"
	dtpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/datatypes_go_proto"
)

func TestRowEncoder_ProtoDate(t *testing.T) {
	enc := view.NewRowEncoderForTest()
	date := &dtpb.Date{
		ValueUs:   631152000000000,
		Precision: dtpb.Date_YEAR,
		Timezone:  "Z",
	}
	val, err := enc.EncodeColumn([]fhirpath.Value{fhirpath.NewValue(date)}, false)
	if err != nil {
		t.Fatalf("EncodeColumn: %v", err)
	}
	if val != "1990" {
		t.Fatalf("date = %v, want 1990", val)
	}
}

func TestRowEncoder_CollectionWrapsSingleton(t *testing.T) {
	enc := view.NewRowEncoderForTest()
	val, err := enc.EncodeColumn([]fhirpath.Value{fhirpath.NewValue("one")}, true)
	if err != nil {
		t.Fatalf("EncodeColumn: %v", err)
	}
	arr, ok := val.([]any)
	if !ok || len(arr) != 1 || arr[0] != "one" {
		t.Fatalf("value = %#v, want [one]", val)
	}
}

func TestRowEncoder_ScalarRejectsMultiValue(t *testing.T) {
	enc := view.NewRowEncoderForTest()
	_, err := enc.EncodeColumn([]fhirpath.Value{fhirpath.NewValue("a"), fhirpath.NewValue("b")}, false)
	if err == nil {
		t.Fatal("expected error for scalar multi-value column")
	}
}

func TestRowEncoder_PatientBirthDatePath(t *testing.T) {
	eng, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	patient := protor4.NewPatient("p1")
	patient.BirthDate = &dtpb.Date{
		ValueUs:   631152000000000,
		Precision: dtpb.Date_YEAR,
		Timezone:  "Z",
	}
	envelope, err := proto.ToEnvelope(patient)
	if err != nil {
		t.Fatalf("ToEnvelope: %v", err)
	}
	expr, err := eng.Compile("Patient.birthDate")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	values, err := expr.Eval(t.Context(), envelope)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	enc := view.NewRowEncoderForTest()
	out, err := enc.EncodeColumn(values, false)
	if err != nil {
		t.Fatalf("EncodeColumn: %v", err)
	}
	if out != "1990" {
		t.Fatalf("birthDate = %v, want 1990", out)
	}
}
