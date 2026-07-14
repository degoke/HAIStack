package view_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/view"
)

func TestParseDefinition_ValidView(t *testing.T) {
	def := view.PatientSummaryView()
	spec, err := view.ParseDefinition(def, defaultEngine(t))
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	if spec.Name != "patient_summary_view" {
		t.Errorf("Name = %q, want patient_summary_view", spec.Name)
	}
	if spec.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", spec.Version)
	}
	if spec.ResourceType != "Patient" {
		t.Errorf("ResourceType = %q, want Patient", spec.ResourceType)
	}
	if len(spec.Columns) != 5 {
		t.Errorf("len(Columns) = %d, want 5", len(spec.Columns))
	}
	if len(spec.Filters) != 0 {
		t.Errorf("len(Filters) = %d, want 0", len(spec.Filters))
	}
	if len(spec.Permissions) != 1 || spec.Permissions[0] != "read-patient-summary" {
		t.Errorf("Permissions = %v, want [read-patient-summary]", spec.Permissions)
	}
}

func TestParseDefinition_RequiresResourceType(t *testing.T) {
	def := []byte(`{"resourceType":"Patient","name":"x","version":"1","resource":"Patient"}`)
	_, err := view.ParseDefinition(def, defaultEngine(t))
	if !errors.Is(err, view.ErrInvalidViewDefinition) {
		t.Fatalf("err = %v, want ErrInvalidViewDefinition", err)
	}
}

func TestParseDefinition_RequiresResourceAndNameAndVersion(t *testing.T) {
	cases := []struct {
		name string
		def  string
	}{
		{"missing resource", `{"resourceType":"ViewDefinition","name":"x","version":"1"}`},
		{"missing name", `{"resourceType":"ViewDefinition","resource":"Patient","version":"1"}`},
		{"missing version", `{"resourceType":"ViewDefinition","name":"x","resource":"Patient"}`},
		{"missing columns", `{"resourceType":"ViewDefinition","name":"x","version":"1","resource":"Patient","select":[{"column":[]}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := view.ParseDefinition([]byte(tc.def), defaultEngine(t))
			if !errors.Is(err, view.ErrInvalidViewDefinition) {
				t.Fatalf("err = %v, want ErrInvalidViewDefinition", err)
			}
		})
	}
}

func TestParseDefinition_UnsupportedNestedSelect(t *testing.T) {
	_, err := view.ParseDefinition(viewWithNestedSelect(), defaultEngine(t))
	if !errors.Is(err, view.ErrUnsupportedFeature) {
		t.Fatalf("err = %v, want ErrUnsupportedFeature", err)
	}
}

func TestParseDefinition_UnsupportedForEach(t *testing.T) {
	_, err := view.ParseDefinition(viewWithUnsupportedJoin(), defaultEngine(t))
	if !errors.Is(err, view.ErrUnsupportedFeature) {
		t.Fatalf("err = %v, want ErrUnsupportedFeature", err)
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := reg.Register(view.AppointmentView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := reg.Register(view.ObservationView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := reg.Get("patient_summary_view", "1.0.0")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "patient_summary_view" {
		t.Errorf("Name = %q, want patient_summary_view", got.Name)
	}

	if _, err := reg.Get("missing", "1.0.0"); !errors.Is(err, view.ErrViewNotFound) {
		t.Fatalf("Get missing: err = %v, want ErrViewNotFound", err)
	}
}

func TestRegistry_DuplicateRegistration(t *testing.T) {
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := reg.Register(view.PatientSummaryView(), defaultEngine(t)); !errors.Is(err, view.ErrViewAlreadyRegistered) {
		t.Fatalf("Register duplicate: err = %v, want ErrViewAlreadyRegistered", err)
	}
}

func TestExecutor_PatientSummaryView(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	store.Seed(t, patientJane(t), patientJohn(t))
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: store,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_summary_view"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("len(Rows) = %d, want 2", len(res.Rows))
	}
	if res.Total != 2 {
		t.Errorf("Total = %d, want 2", res.Total)
	}
	if res.Metadata.SourceResourceType != "Patient" {
		t.Errorf("SourceResourceType = %q, want Patient", res.Metadata.SourceResourceType)
	}

	row := res.Rows[0]
	if row["id"] != "pat-jane" {
		t.Errorf("id = %v, want pat-jane", row["id"])
	}
	if row["given"] != "Jane" {
		t.Errorf("given = %v, want Jane", row["given"])
	}
	if row["family"] != "Doe" {
		t.Errorf("family = %v, want Doe", row["family"])
	}
	if row["gender"] != "female" {
		t.Errorf("gender = %v, want female", row["gender"])
	}
	if row["phone"] != "555-0100" {
		t.Errorf("phone = %v, want 555-0100", row["phone"])
	}

	johnFound := false
	for _, r := range res.Rows {
		if r["id"] == "pat-john" {
			johnFound = true
			if r["phone"] != nil {
				t.Errorf("pat-john phone = %v, want null", r["phone"])
			}
		}
	}
	if !johnFound {
		t.Errorf("pat-john not found in rows")
	}
}

func TestExecutor_AppointmentView(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	store.Seed(t, appointmentBooked(t), appointmentFulfilled(t))
	reg := view.NewRegistry()
	if _, err := reg.Register(view.AppointmentView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: store,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "appointment_view"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(res.Rows))
	}
	if res.Total != 1 {
		t.Errorf("Total = %d, want 1", res.Total)
	}
	if res.Rows[0]["id"] != "appt-1" {
		t.Errorf("id = %v, want appt-1", res.Rows[0]["id"])
	}
	if res.Rows[0]["status"] != "booked" {
		t.Errorf("status = %v, want booked", res.Rows[0]["status"])
	}
	if res.Rows[0]["patientRef"] != "Patient/pat-jane" {
		t.Errorf("patientRef = %v, want Patient/pat-jane", res.Rows[0]["patientRef"])
	}
}

func TestExecutor_ObservationView(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	store.Seed(t, observationHeartRate(t), observationDraft(t))
	reg := view.NewRegistry()
	if _, err := reg.Register(view.ObservationView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: store,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "observation_view"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(res.Rows))
	}
	if res.Total != 1 {
		t.Errorf("Total = %d, want 1", res.Total)
	}
	if res.Rows[0]["id"] != "obs-1" {
		t.Errorf("id = %v, want obs-1", res.Rows[0]["id"])
	}
	if res.Rows[0]["codeText"] != "Heart rate" {
		t.Errorf("codeText = %v, want Heart rate", res.Rows[0]["codeText"])
	}
	if res.Rows[0]["value"] != 72.0 {
		t.Errorf("value = %v, want 72.0", res.Rows[0]["value"])
	}
	if res.Rows[0]["unit"] != "beats/min" {
		t.Errorf("unit = %v, want beats/min", res.Rows[0]["unit"])
	}
}

func TestExecutor_Pagination(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	store.Seed(t, patientJane(t), patientJohn(t))
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: store,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_summary_view", Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("Execute page 1: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(res.Rows))
	}
	if res.Total != 2 {
		t.Errorf("Total = %d, want 2", res.Total)
	}
	if res.NextOffset == nil || *res.NextOffset != 1 {
		t.Errorf("NextOffset = %v, want 1", res.NextOffset)
	}

	res2, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_summary_view", Limit: 1, Offset: *res.NextOffset})
	if err != nil {
		t.Fatalf("Execute page 2: %v", err)
	}
	if len(res2.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(res2.Rows))
	}
	if res2.NextOffset != nil {
		t.Errorf("NextOffset = %v, want nil", res2.NextOffset)
	}
	if res.Rows[0]["id"] == res2.Rows[0]["id"] {
		t.Errorf("both pages returned same id %v", res.Rows[0]["id"])
	}
}

func TestExecutor_WithoutAuthorizer(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	store.Seed(t, patientJane(t))
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: store,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_summary_view", Actor: "nurse-1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(res.Rows))
	}
}

func TestExecutor_AuthorizerDenies(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	store.Seed(t, patientJane(t))
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	authz := newFakeAuthorizer() // no allowed permissions
	audit := &fakeAuditLogger{}
	exec, err := view.NewExecutor(view.Config{
		Resources:  store,
		Engine:     defaultEngine(t),
		Registry:   reg,
		Authorizer: authz,
		Audit:      audit,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	_, err = exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_summary_view", Actor: "nurse-1"})
	if !errors.Is(err, view.ErrUnauthorized) {
		t.Fatalf("Execute: err = %v, want ErrUnauthorized", err)
	}
	calls := authz.Calls()
	if len(calls) != 1 {
		t.Fatalf("Authorizer calls = %d, want 1", len(calls))
	}
	if calls[0].Actor != "nurse-1" {
		t.Errorf("Actor = %q, want nurse-1", calls[0].Actor)
	}
	if len(calls[0].Permissions) != 1 || calls[0].Permissions[0] != "read-patient-summary" {
		t.Errorf("Permissions = %v, want [read-patient-summary]", calls[0].Permissions)
	}
	records := audit.Records()
	if len(records) != 1 {
		t.Fatalf("Audit records = %d, want 1", len(records))
	}
	if records[0].Outcome != "denied" {
		t.Errorf("Outcome = %q, want denied", records[0].Outcome)
	}
	if records[0].Actor != "nurse-1" {
		t.Errorf("Actor = %q, want nurse-1", records[0].Actor)
	}
}

func TestExecutor_AuthorizerAllows(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	store.Seed(t, patientJane(t))
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	authz := newFakeAuthorizer("read-patient-summary")
	audit := &fakeAuditLogger{}
	exec, err := view.NewExecutor(view.Config{
		Resources:  store,
		Engine:     defaultEngine(t),
		Registry:   reg,
		Authorizer: authz,
		Audit:      audit,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_summary_view", Actor: "nurse-1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(res.Rows))
	}
	records := audit.Records()
	if len(records) != 1 {
		t.Fatalf("Audit records = %d, want 1", len(records))
	}
	if records[0].Outcome != "success" {
		t.Errorf("Outcome = %q, want success", records[0].Outcome)
	}
	if records[0].ViewName != "patient_summary_view" {
		t.Errorf("ViewName = %q, want patient_summary_view", records[0].ViewName)
	}
}

func TestExecutor_AuditOnMissingView(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	reg := view.NewRegistry()
	audit := &fakeAuditLogger{}
	exec, err := view.NewExecutor(view.Config{
		Resources: store,
		Engine:    defaultEngine(t),
		Registry:  reg,
		Audit:     audit,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	_, err = exec.Execute(ctx, view.ExecuteRequest{ViewName: "missing_view", Actor: "nurse-1"})
	if !errors.Is(err, view.ErrViewNotFound) {
		t.Fatalf("Execute: err = %v, want ErrViewNotFound", err)
	}
	records := audit.Records()
	if len(records) != 1 {
		t.Fatalf("Audit records = %d, want 1", len(records))
	}
	if records[0].Outcome != "error" {
		t.Errorf("Outcome = %q, want error", records[0].Outcome)
	}
	if records[0].ViewName != "missing_view" {
		t.Errorf("ViewName = %q, want missing_view", records[0].ViewName)
	}
}

func TestExecutor_StandaloneWithoutModulesOrAuth(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	store.Seed(t, observationHeartRate(t))
	reg := view.NewRegistry()
	if _, err := reg.Register(view.ObservationView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: store,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "observation_view"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(res.Rows))
	}
}

func TestExecutor_RepeatedColumnResult(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	store.Seed(t, patientJane(t))
	reg := view.NewRegistry()
	def := []byte(`{
		"resourceType": "ViewDefinition",
		"name": "patient_phones",
		"version": "1.0.0",
		"resource": "Patient",
		"select": [{
			"column": [
				{"name": "id", "path": "Patient.id"},
				{"name": "phone", "path": "Patient.telecom.where(system = 'phone').value", "collection": true}
			]
		}]
	}`)
	if _, err := reg.Register(def, defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: store,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_phones"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(res.Rows))
	}
	phones, ok := res.Rows[0]["phone"].([]any)
	if !ok {
		t.Fatalf("phone = %T, want []any", res.Rows[0]["phone"])
	}
	if len(phones) != 2 || phones[0] != "555-0100" || phones[1] != "555-0101" {
		t.Errorf("phone = %v, want [555-0100 555-0101]", phones)
	}
}

func TestExecutor_EmptyColumnResult(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	store.Seed(t, patientJohn(t))
	reg := view.NewRegistry()
	def := []byte(`{
		"resourceType": "ViewDefinition",
		"name": "patient_phones",
		"version": "1.0.0",
		"resource": "Patient",
		"select": [{
			"column": [
				{"name": "id", "path": "Patient.id"},
				{"name": "phone", "path": "Patient.telecom.where(system = 'phone').value"}
			]
		}]
	}`)
	if _, err := reg.Register(def, defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: store,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_phones"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(res.Rows))
	}
	if res.Rows[0]["phone"] != nil {
		t.Errorf("phone = %v, want nil", res.Rows[0]["phone"])
	}
}

func TestExecutor_FilterEvaluation(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	store.Seed(t, patientJane(t), patientJohn(t))
	reg := view.NewRegistry()
	def := []byte(`{
		"resourceType": "ViewDefinition",
		"name": "female_patients",
		"version": "1.0.0",
		"resource": "Patient",
		"select": [{"column": [{"name": "id", "path": "Patient.id"}]}],
		"where": [{"path": "Patient.gender = 'female'"}]
	}`)
	if _, err := reg.Register(def, defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: store,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "female_patients"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(res.Rows))
	}
	if res.Rows[0]["id"] != "pat-jane" {
		t.Errorf("id = %v, want pat-jane", res.Rows[0]["id"])
	}
	if res.Total != 1 {
		t.Errorf("Total = %d, want 1", res.Total)
	}
	if res.Metadata.Filtered != 1 {
		t.Errorf("Filtered = %d, want 1", res.Metadata.Filtered)
	}
	if res.Metadata.Scanned != 2 {
		t.Errorf("Scanned = %d, want 2", res.Metadata.Scanned)
	}
}

func TestExecutor_NewExecutorRequiresResources(t *testing.T) {
	_, err := view.NewExecutor(view.Config{
		Engine: defaultEngine(t),
	})
	if !errors.Is(err, view.ErrMissingResourceStore) {
		t.Fatalf("err = %v, want ErrMissingResourceStore", err)
	}
}

func TestExecutor_NewExecutorRequiresEngine(t *testing.T) {
	_, err := view.NewExecutor(view.Config{
		Resources: newMemResourceStore(),
	})
	if !errors.Is(err, view.ErrMissingEngine) {
		t.Fatalf("err = %v, want ErrMissingEngine", err)
	}
}

func TestExecutor_MetadataTiming(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	store.Seed(t, patientJane(t))
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	now := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	exec, err := view.NewExecutor(view.Config{
		Resources: store,
		Engine:    defaultEngine(t),
		Registry:  reg,
		Now:       newFixedClock(now).Now,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_summary_view"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Metadata.ExecutedAt.Equal(now) {
		t.Errorf("ExecutedAt = %v, want %v", res.Metadata.ExecutedAt, now)
	}
	if res.Metadata.Duration != 0 {
		t.Errorf("Duration = %v, want 0", res.Metadata.Duration)
	}
}

func TestParseDefinition_WithEngineValidatesExpressions(t *testing.T) {
	eng := defaultEngine(t)
	def := view.PatientSummaryView()
	spec, err := view.ParseDefinition(def, eng)
	if err != nil {
		t.Fatalf("ParseDefinition with engine: %v", err)
	}
	if spec.Name != "patient_summary_view" {
		t.Errorf("Name = %q, want patient_summary_view", spec.Name)
	}
}

func TestParseDefinition_WithEngineRejectsInvalidExpression(t *testing.T) {
	eng, err := fhirpath.NewEngine(fhirpath.Config{MaxExpressionLen: 8})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	def := []byte(`{
		"resourceType": "ViewDefinition",
		"name": "bad_expr",
		"version": "1.0.0",
		"resource": "Patient",
		"select": [{"column": [{"name": "id", "path": "Patient.id"}]}]
	}`)
	_, err = view.ParseDefinition(def, eng)
	if err == nil {
		t.Fatal("expected error for expression exceeding max length")
	}
}

func TestRegistry_RegisterValidatesExpression(t *testing.T) {
	eng, err := fhirpath.NewEngine(fhirpath.Config{MaxExpressionLen: 8})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	reg := view.NewRegistry()
	def := []byte(`{
		"resourceType": "ViewDefinition",
		"name": "bad_expr",
		"version": "1.0.0",
		"resource": "Patient",
		"select": [{"column": [{"name": "id", "path": "Patient.id"}]}]
	}`)
	_, err = reg.Register(def, eng)
	if err == nil {
		t.Fatal("expected error for expression exceeding max length")
	}
	if _, err := reg.Get("bad_expr", "1.0.0"); !errors.Is(err, view.ErrViewNotFound) {
		t.Fatalf("invalid view should not be registered: %v", err)
	}
}

func TestExecutor_ResolveRequiresVersionWhenMultiple(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	store.Seed(t, patientJane(t))
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register v1: %v", err)
	}
	v2 := []byte(`{
		"resourceType": "ViewDefinition",
		"name": "patient_summary_view",
		"version": "2.0.0",
		"resource": "Patient",
		"select": [{"column": [{"name": "id", "path": "Patient.id"}]}]
	}`)
	if _, err := reg.Register(v2, defaultEngine(t)); err != nil {
		t.Fatalf("Register v2: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: store,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	_, err = exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_summary_view"})
	if !errors.Is(err, view.ErrViewNotFound) {
		t.Fatalf("Execute without version: err = %v, want ErrViewNotFound", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_summary_view", Version: "2.0.0"})
	if err != nil {
		t.Fatalf("Execute with version: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0]["id"] != "pat-jane" {
		t.Fatalf("rows = %v, want one pat-jane row", res.Rows)
	}
}

func TestExecutor_PassesParametersToAuthorizerAndAudit(t *testing.T) {
	ctx := context.Background()
	store := newMemResourceStore()
	store.Seed(t, patientJane(t))
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	authz := newFakeAuthorizer("read-patient-summary")
	audit := &fakeAuditLogger{}
	exec, err := view.NewExecutor(view.Config{
		Resources:  store,
		Engine:     defaultEngine(t),
		Registry:   reg,
		Authorizer: authz,
		Audit:      audit,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	params := map[string]any{"ward": "A3", "includePhone": true}
	res, err := exec.Execute(ctx, view.ExecuteRequest{
		ViewName:   "patient_summary_view",
		Actor:      "nurse-1",
		Parameters: params,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(res.Rows))
	}

	calls := authz.Calls()
	if len(calls) != 1 {
		t.Fatalf("Authorizer calls = %d, want 1", len(calls))
	}
	if len(calls[0].Parameters) != 2 || calls[0].Parameters["ward"] != "A3" {
		t.Errorf("Authorizer Parameters = %v, want ward=A3", calls[0].Parameters)
	}

	records := audit.Records()
	if len(records) != 1 {
		t.Fatalf("Audit records = %d, want 1", len(records))
	}
	if len(records[0].Parameters) != 2 || records[0].Parameters["ward"] != "A3" {
		t.Errorf("Audit Parameters = %v, want ward=A3", records[0].Parameters)
	}
	if records[0].Outcome != "success" {
		t.Errorf("Outcome = %q, want success", records[0].Outcome)
	}
}
