package app

// SkillRuntimeStatus describes one bundled runtime installed next to Miel.
type SkillRuntimeStatus struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Path      string `json:"path"`
	Available bool   `json:"available"`
	Message   string `json:"message,omitempty"`
}

// SkillDependencyPlan is the reviewable, non-shell installation contract.
type SkillDependencyPlan struct {
	SchemaVersion     int      `json:"schemaVersion"`
	Skill             string   `json:"skill"`
	Runtime           string   `json:"runtime"`
	Evidence          []string `json:"evidence"`
	Confidence        string   `json:"confidence"`
	PackageManager    string   `json:"packageManager,omitempty"`
	Lockfile          string   `json:"lockfile,omitempty"`
	DependencyFile    string   `json:"dependencyFile,omitempty"`
	DependencySources []string `json:"dependencySources,omitempty"`
	AutoUpdate        bool     `json:"autoUpdate"`
	EntryCommand      string   `json:"entryCommand,omitempty"`
	EntryArgs         []string `json:"entryArgs,omitempty"`
	Network           bool     `json:"network"`
	NeedsReview       bool     `json:"needsReview"`
	Source            string   `json:"source"`
	Kind              string   `json:"kind"`
	CanRun            bool     `json:"canRun"`
	Reason            string   `json:"reason,omitempty"`
	legacy            bool
}

type SkillEnvironmentStatus struct {
	Skill                 string `json:"skill"`
	State                 string `json:"state"`
	Runtime               string `json:"runtime,omitempty"`
	Environment           string `json:"environment,omitempty"`
	PlanHash              string `json:"planHash,omitempty"`
	RuntimeVersion        string `json:"runtimeVersion,omitempty"`
	PackageManagerVersion string `json:"packageManagerVersion,omitempty"`
	DependencySourceHash  string `json:"dependencySourceHash,omitempty"`
	LockHash              string `json:"lockHash,omitempty"`
	Error                 string `json:"error,omitempty"`
	UpdatedAt             int64  `json:"updatedAt"`
}

type SkillInstallProgress struct {
	Skill   string `json:"skill"`
	Stage   string `json:"stage"`
	Message string `json:"message,omitempty"`
	State   string `json:"state"`
}
