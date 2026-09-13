package app

// DirectoryService exposes the canonical application layout to Wails clients.
// Business services use the same manager internally, so UI diagnostics and runtime paths cannot drift.
type DirectoryService struct {
	manager *DirectoryManager
}

func NewDirectoryService(manager *DirectoryManager) *DirectoryService {
	if manager == nil {
		manager = DefaultDirectoryManager()
	}
	return &DirectoryService{manager: manager}
}

// Paths returns the current locations of databases, logs, sources, attachments and outputs.
func (s *DirectoryService) Paths() DirectoryPaths {
	if s == nil || s.manager == nil {
		return DirectoryPaths{}
	}
	return s.manager.Paths()
}

// OpenDataDir opens the managed application root in Windows Explorer.
func (s *DirectoryService) OpenDataDir() error {
	if s == nil || s.manager == nil {
		return nil
	}
	return s.manager.OpenRoot()
}
