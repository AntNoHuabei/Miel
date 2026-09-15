export interface SkillLite {
  name: string;
  description: string;
  source: "builtin" | "local" | "skillhub" | string;
  enabled: boolean;
  path: string;
  updatedAt: number;
}
export interface SkillHubSkillLite {
  slug: string;
  name: string;
  description: string;
  fallback: string;
  category: string;
  downloads: number;
  stars: number;
  version: string;
  iconUrl: string;
  verified: boolean;
}
export interface SkillHubPageLite {
  skills: SkillHubSkillLite[];
  total: number;
  page: number;
  pages: number;
}
export interface SkillRuntimeStatusLite {
  name: string;
  version: string;
  path: string;
  available: boolean;
  message?: string;
}
export interface SkillDependencyPlanLite {
  schemaVersion: number;
  skill: string;
  runtime: string;
  evidence: string[];
  confidence: string;
  packageManager?: string;
  lockfile?: string;
  dependencyFile?: string;
  dependencySources?: string[];
  autoUpdate: boolean;
  entryCommand?: string;
  entryArgs?: string[];
  network: boolean;
  needsReview: boolean;
  source: string;
}
export interface SkillEnvironmentStatusLite {
  skill: string;
  state:
    | "none"
    | "needs-review"
    | "initializing"
    | "ready"
    | "failed"
    | "stale"
    | string;
  runtime?: string;
  environment?: string;
  planHash?: string;
  runtimeVersion?: string;
  packageManagerVersion?: string;
  dependencySourceHash?: string;
  lockHash?: string;
  error?: string;
  updatedAt: number;
}
export interface SkillInstallProgressLite {
  skill: string;
  stage: string;
  message?: string;
  state: string;
}
