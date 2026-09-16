import type {
  SkillDependencyPlanLite,
  SkillEnvironmentStatusLite,
  SkillHubPageLite,
  SkillLite,
  SkillRuntimeStatusLite,
} from "../types/skills";
import { Services } from "./bindings";
import { normalizeList } from "./helpers";

export const skillRepository = {
  list: async () =>
    normalizeList(await Services.SkillService.ListSkills()) as SkillLite[],
  listHub: async (
    page: number,
    size: number,
    query: string,
  ): Promise<SkillHubPageLite> =>
    normalizeSkillHubPage(
      await Services.SkillService.ListSkillHubSkills(page, size, query),
    ),
  pickLocal: () => Services.SkillService.PickLocal(),
  setEnabled: (name: string, enabled: boolean) =>
    Services.SkillService.SetEnabled(name, enabled),
  delete: (name: string) => Services.SkillService.Delete(name),
  installHub: (slug: string) => Services.SkillService.InstallSkillHub(slug),
  listRuntimes: () =>
    Services.SkillDependencyService.ListRuntimeStatus() as Promise<
      SkillRuntimeStatusLite[]
    >,
  inspectDependencies: async (name: string) =>
    normalizeSkillDependencyPlan(
      await Services.SkillDependencyService.InspectSkillDependencies(name),
    ),
  generateDependencyPlan: async (name: string) =>
    normalizeSkillDependencyPlan(
      await Services.SkillDependencyService.GenerateSkillDependencyPlan(name),
    ),
  confirmDependencyPlan: (name: string, plan: SkillDependencyPlanLite) =>
    Services.SkillDependencyService.ConfirmSkillDependencyPlan(name, plan),
  environmentStatus: (name: string) =>
    Services.SkillDependencyService.GetSkillEnvironmentStatus(
      name,
    ) as Promise<SkillEnvironmentStatusLite>,
  initialize: (name: string) =>
    Services.SkillDependencyService.InitializeSkill(
      name,
    ) as Promise<SkillEnvironmentStatusLite>,
  repair: (name: string) =>
    Services.SkillDependencyService.RepairSkillEnvironment(
      name,
    ) as Promise<SkillEnvironmentStatusLite>,
  removeEnvironment: (name: string) =>
    Services.SkillDependencyService.RemoveSkillEnvironment(name),
};

export function normalizeSkillDependencyPlan(
  plan: Omit<
    SkillDependencyPlanLite,
    "evidence" | "dependencySources" | "entryArgs"
  > & {
    evidence?: string[] | null;
    dependencySources?: string[] | null;
    entryArgs?: string[] | null;
  },
): SkillDependencyPlanLite {
  return {
    ...plan,
    kind: plan.kind || (plan.runtime ? "unresolved" : "instruction"),
    canRun: Boolean(plan.canRun),
    evidence: normalizeList(plan.evidence),
    dependencySources: normalizeList(plan.dependencySources),
    entryArgs: normalizeList(plan.entryArgs),
  };
}

export function normalizeSkillHubPage(result: {
  skills?: Array<{
    slug: string;
    name: string;
    description_zh?: string;
    description: string;
    category: string;
    downloads: number;
    stars: number;
    version: string;
    iconUrl: string;
    verified: boolean;
  }> | null;
  total: number;
  page: number;
  pages: number;
}): SkillHubPageLite {
  return {
    skills: normalizeList(result.skills).map((skill) => ({
      ...skill,
      description: skill.description_zh || skill.description,
      fallback: skill.description,
    })),
    total: result.total,
    page: result.page,
    pages: result.pages,
  };
}
