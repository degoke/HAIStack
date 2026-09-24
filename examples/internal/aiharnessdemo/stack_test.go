package aiharnessdemo

import (
	"context"
	"testing"
)

func TestDemoPatientCleanup(t *testing.T) {
	_, _, cleanup, err := DemoPatient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
}
