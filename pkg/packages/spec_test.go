package packages

import "testing"

func TestInstallSpecValidate(t *testing.T) {
	if err := (InstallSpec{PackageID: "a", Version: "1"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (InstallSpec{Path: "/tmp/ig", Version: "1"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (InstallSpec{Path: "/tmp/ig"}).Validate(); err == nil {
		t.Fatal("expected version required for path install")
	}
	if err := (InstallSpec{PackageID: "a"}).Validate(); err == nil {
		t.Fatal("expected version required for registry install")
	}
}
