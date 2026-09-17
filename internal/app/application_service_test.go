package app

import "testing"

func TestApplicationServiceVersion(t *testing.T) {
	previous := AppVersion
	t.Cleanup(func() { AppVersion = previous })

	service := NewApplicationService()
	AppVersion = "v0.0.1"
	if got := service.Version(); got != "v0.0.1" {
		t.Fatalf("Version() = %q, want v0.0.1", got)
	}

	AppVersion = " "
	if got := service.Version(); got != "dev" {
		t.Fatalf("blank Version() = %q, want dev", got)
	}
}
