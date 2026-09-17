package app

import "strings"

// AppVersion is replaced by the release build through -ldflags -X.
var AppVersion = "dev"

// ApplicationService exposes build metadata to the desktop UI.
type ApplicationService struct{}

func NewApplicationService() *ApplicationService { return &ApplicationService{} }

// Version returns the version embedded in the running executable.
func (*ApplicationService) Version() string {
	version := strings.TrimSpace(AppVersion)
	if version == "" {
		return "dev"
	}
	return version
}
