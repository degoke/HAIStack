package view_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/view"
)

func TestParseDefinition_NestedSelect(t *testing.T) {
	spec, err := view.ParseDefinition(viewWithNestedSelect(), defaultEngine(t))
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	if len(spec.Columns) != 1 || spec.Columns[0].Name != "id" {
		t.Fatalf("Columns = %+v, want id column", spec.Columns)
	}
}

func TestParseDefinition_ForEachSelect(t *testing.T) {
	spec, err := view.ParseDefinition(viewWithForEachTelecom(), defaultEngine(t))
	if err != nil {
		t.Fatalf("ParseDefinition: %v", err)
	}
	names := spec.ColumnNames()
	if len(names) != 2 || names[0] != "id" || names[1] != "phone" {
		t.Fatalf("ColumnNames = %v, want [id phone]", names)
	}
}

func TestExecutor_ForEachTelecom(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t), patientJohn(t))
	reg := view.NewRegistry()
	if _, err := reg.Register(viewWithForEachTelecom(), defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_phones_foreach"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Total != 2 {
		t.Fatalf("Total = %d, want 2 phone rows for Jane", res.Total)
	}
	if len(res.Rows) != 2 {
		t.Fatalf("len(Rows) = %d, want 2", len(res.Rows))
	}
	for _, row := range res.Rows {
		if row["id"] != "pat-jane" {
			t.Errorf("id = %v, want pat-jane", row["id"])
		}
	}
}

func TestExecutor_ForEachOrNull(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	resources.Seed(t, patientJohn(t))
	reg := view.NewRegistry()
	def := []byte(`{
		"resourceType": "ViewDefinition",
		"name": "patient_optional_phone",
		"version": "1.0.0",
		"resource": "Patient",
		"select": [{
			"column": [{"name": "id", "path": "Patient.id"}],
			"select": [{
				"forEachOrNull": "Patient.telecom.where(system = 'phone')",
				"column": [{"name": "phone", "path": "value"}]
			}]
		}]
	}`)
	if _, err := reg.Register(def, defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_optional_phone"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(res.Rows))
	}
	if res.Rows[0]["id"] != "pat-john" {
		t.Errorf("id = %v, want pat-john", res.Rows[0]["id"])
	}
	if res.Rows[0]["phone"] != nil {
		t.Errorf("phone = %v, want null", res.Rows[0]["phone"])
	}
}

func TestExecutor_UnionAllTelecom(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t))
	reg := view.NewRegistry()
	def := []byte(`{
		"resourceType": "ViewDefinition",
		"name": "patient_contact_union",
		"version": "1.0.0",
		"resource": "Patient",
		"select": [{
			"unionAll": [
				{
					"forEach": "Patient.telecom.where(system = 'phone')",
					"column": [
						{"name": "id", "path": "Patient.id"},
						{"name": "contact", "path": "value"}
					]
				},
				{
					"forEach": "Patient.telecom.where(system = 'email')",
					"column": [
						{"name": "id", "path": "Patient.id"},
						{"name": "contact", "path": "value"}
					]
				}
			]
		}]
	}`)
	if _, err := reg.Register(def, defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_contact_union"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Total != 3 {
		t.Fatalf("Total = %d, want 3 (2 phones + 1 email)", res.Total)
	}
}

func TestExecutor_ReferenceJoin(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t), appointmentBooked(t))
	reg := view.NewRegistry()
	def := []byte(`{
		"resourceType": "ViewDefinition",
		"name": "appointment_patient_join",
		"version": "1.0.0",
		"resource": "Appointment",
		"select": [{
			"column": [{"name": "appt_id", "path": "Appointment.id"}],
			"select": [{
				"forEach": "Appointment.participant.actor",
				"select": [{
					"column": [
						{"name": "patient_id", "path": "Patient.id"},
						{"name": "family", "path": "Patient.name.first().family"}
					]
				}]
			}]
		}]
	}`)
	if _, err := reg.Register(def, defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "appointment_patient_join"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(res.Rows))
	}
	row := res.Rows[0]
	if row["appt_id"] != "appt-1" {
		t.Errorf("appt_id = %v, want appt-1", row["appt_id"])
	}
	if row["patient_id"] != "pat-jane" {
		t.Errorf("patient_id = %v, want pat-jane", row["patient_id"])
	}
	if row["family"] != "Doe" {
		t.Errorf("family = %v, want Doe", row["family"])
	}
}

func TestExecutor_MaterializeRows(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t))
	views := newMemMaterializedViewStore()
	reg := view.NewRegistry()
	def := []byte(`{
		"resourceType": "ViewDefinition",
		"name": "patient_summary_materialized",
		"version": "1.0.0",
		"resource": "Patient",
		"metadata": {"materialize": "true", "materializeKey": "id"},
		"select": [{
			"column": [
				{"name": "id", "path": "Patient.id"},
				{"name": "family", "path": "Patient.name.first().family"}
			]
		}]
	}`)
	if _, err := reg.Register(def, defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources:         resources,
		Engine:            defaultEngine(t),
		Registry:          reg,
		MaterializedViews: views,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	res, err := exec.Execute(ctx, view.ExecuteRequest{ViewName: "patient_summary_materialized"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.Rows) != 1 {
		t.Fatalf("len(Rows) = %d, want 1", len(res.Rows))
	}

	record, err := views.Get(ctx, "patient_summary_materialized|1.0.0", "pat-jane")
	if err != nil {
		t.Fatalf("Get materialized row: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(record.Payload, &payload); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	if payload["family"] != "Doe" {
		t.Errorf("payload family = %v, want Doe", payload["family"])
	}
}

func TestExecutor_MaterializeRequiresStore(t *testing.T) {
	ctx := context.Background()
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t))
	reg := view.NewRegistry()
	def := []byte(`{
		"resourceType": "ViewDefinition",
		"name": "needs_store",
		"version": "1.0.0",
		"resource": "Patient",
		"metadata": {"materialize": "true"},
		"select": [{"column": [{"name": "id", "path": "Patient.id"}]}]
	}`)
	if _, err := reg.Register(def, defaultEngine(t)); err != nil {
		t.Fatalf("Register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    defaultEngine(t),
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}

	_, err = exec.Execute(ctx, view.ExecuteRequest{ViewName: "needs_store"})
	if !errors.Is(err, view.ErrMissingMaterializedViewStore) {
		t.Fatalf("Execute err = %v, want ErrMissingMaterializedViewStore", err)
	}
}

type memMaterializedViewStore struct {
	views map[string]store.MaterializedViewRecord
}

func newMemMaterializedViewStore() *memMaterializedViewStore {
	return &memMaterializedViewStore{views: make(map[string]store.MaterializedViewRecord)}
}

func materializedKey(viewName, key string) string {
	return viewName + "\x00" + key
}

func (s *memMaterializedViewStore) Upsert(_ context.Context, record store.MaterializedViewRecord) error {
	s.views[materializedKey(record.ViewName, record.Key)] = record
	return nil
}

func (s *memMaterializedViewStore) Get(_ context.Context, viewName, key string) (*store.MaterializedViewRecord, error) {
	record, ok := s.views[materializedKey(viewName, key)]
	if !ok {
		return nil, errors.New("not found")
	}
	return &record, nil
}

func (s *memMaterializedViewStore) Delete(_ context.Context, viewName, key string) error {
	delete(s.views, materializedKey(viewName, key))
	return nil
}

func (s *memMaterializedViewStore) ListKeys(_ context.Context, viewName string) ([]string, error) {
	var keys []string
	prefix := viewName + "\x00"
	for k, record := range s.views {
		if record.ViewName == viewName || len(k) > len(prefix) && k[:len(prefix)] == prefix {
			keys = append(keys, record.Key)
		}
	}
	return keys, nil
}

func viewWithForEachTelecom() []byte {
	return []byte(`{
		"resourceType": "ViewDefinition",
		"name": "patient_phones_foreach",
		"version": "1.0.0",
		"resource": "Patient",
		"select": [{
			"column": [{"name": "id", "path": "Patient.id"}],
			"select": [{
				"forEach": "Patient.telecom.where(system = 'phone')",
				"column": [{"name": "phone", "path": "value"}]
			}]
		}]
	}`)
}
