import { App } from "antd";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const repositories = vi.hoisted(() => ({
  listProviders: vi.fn(),
  modelCatalog: vi.fn(),
  listSkills: vi.fn(),
  listHub: vi.fn(),
  setSkillEnabled: vi.fn(),
  listRuntimes: vi.fn(),
  environmentStatus: vi.fn(),
  inspectDependencies: vi.fn(),
  confirmDependencyPlan: vi.fn(),
  initialize: vi.fn(),
}));

vi.mock("../shared/repositories", () => ({
  settingsRepository: {
    listProviders: repositories.listProviders,
    modelCatalog: repositories.modelCatalog,
    providerModels: vi.fn().mockResolvedValue([]),
    providerTemplates: vi.fn().mockResolvedValue([]),
  },
  skillRepository: {
    list: repositories.listSkills,
    listHub: repositories.listHub,
    pickLocal: vi.fn(),
    setEnabled: repositories.setSkillEnabled,
    delete: vi.fn(),
    installHub: vi.fn(),
    listRuntimes: repositories.listRuntimes,
    environmentStatus: repositories.environmentStatus,
    inspectDependencies: repositories.inspectDependencies,
    generateDependencyPlan: vi.fn(),
    confirmDependencyPlan: repositories.confirmDependencyPlan,
    initialize: repositories.initialize,
    repair: vi.fn(),
    removeEnvironment: vi.fn(),
  },
  directoryRepository: {
    paths: vi.fn().mockResolvedValue({ root: "D:/data" }),
    openDataDir: vi.fn(),
  },
  memoryRepository: {
    getSettings: vi.fn(),
    list: vi.fn(),
    saveSettings: vi.fn(),
    add: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    clear: vi.fn(),
    export: vi.fn(),
  },
}));

vi.mock("../components/ProviderFormModal", () => ({ default: () => null }));
vi.mock("../components/MemorySettingsPanel", () => ({
  default: () => <div>memory panel</div>,
}));
vi.mock("../theme/ThemeContext", () => ({
  BM_THEMES: [],
  useBMTheme: () => ({ themeId: "system", setTheme: vi.fn() }),
}));

import SettingsView from "./SettingsView";

describe("SettingsView loading boundaries", () => {
  beforeEach(() => {
    repositories.listProviders.mockReset().mockResolvedValue([]);
    repositories.modelCatalog.mockReset().mockResolvedValue([]);
    repositories.listSkills.mockReset().mockResolvedValue([]);
    repositories.listHub
      .mockReset()
      .mockResolvedValue({ skills: [], total: 0, page: 1, pages: 0 });
    repositories.setSkillEnabled.mockReset().mockResolvedValue(undefined);
    repositories.listRuntimes.mockReset().mockResolvedValue([]);
    repositories.environmentStatus
      .mockReset()
      .mockResolvedValue({ skill: "todo", state: "none", updatedAt: 0 });
    repositories.inspectDependencies.mockReset();
    repositories.confirmDependencyPlan.mockReset().mockResolvedValue(undefined);
    repositories.initialize.mockReset().mockResolvedValue({
      skill: "watermark",
      state: "ready",
      updatedAt: 1,
    });
  });

  it("loads only the active section", async () => {
    render(
      <App>
        <SettingsView />
      </App>,
    );
    await waitFor(() =>
      expect(repositories.listProviders).toHaveBeenCalledTimes(1),
    );
    expect(repositories.listSkills).not.toHaveBeenCalled();
    expect(repositories.listHub).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: /技能管理/ }));
    await waitFor(() =>
      expect(repositories.listSkills).toHaveBeenCalledTimes(1),
    );
    expect(repositories.listHub).not.toHaveBeenCalled();
  });

  it("requests SkillHub only after its tab is selected", async () => {
    render(
      <App>
        <SettingsView />
      </App>,
    );
    await userEvent.click(screen.getByRole("button", { name: /技能管理/ }));
    await waitFor(() => expect(repositories.listSkills).toHaveBeenCalled());
    await userEvent.click(screen.getByRole("tab", { name: /SkillHub/ }));
    await waitFor(() =>
      expect(repositories.listHub).toHaveBeenCalledWith(1, 12, ""),
    );
  });

  it("allows built-in skills to be disabled without allowing deletion", async () => {
    repositories.listSkills.mockResolvedValue([
      {
        name: "todo",
        description: "Todo",
        source: "builtin",
        enabled: true,
        path: "skills/todo",
        updatedAt: 1,
      },
    ]);
    render(
      <App>
        <SettingsView />
      </App>,
    );
    await userEvent.click(screen.getByRole("button", { name: /技能管理/ }));
    const toggle = await screen.findByRole("switch", { name: "停用 todo" });
    expect(toggle).toBeEnabled();
    expect(
      screen.queryByRole("button", { name: "删除 todo" }),
    ).not.toBeInTheDocument();
    await userEvent.click(toggle);
    await waitFor(() =>
      expect(repositories.setSkillEnabled).toHaveBeenCalledWith("todo", false),
    );
  });

  it("opens dependency confirmation when backend returns null evidence", async () => {
    repositories.listSkills.mockResolvedValue([
      {
        name: "watermark",
        description: "Watermark",
        source: "skillhub",
        enabled: true,
        path: "skills/watermark",
        updatedAt: 1,
      },
    ]);
    repositories.inspectDependencies.mockResolvedValue({
      schemaVersion: 2,
      skill: "watermark",
      runtime: "python",
      evidence: null,
      confidence: "confirmed",
      packageManager: "uv",
      dependencySources: null,
      entryArgs: null,
      network: false,
      needsReview: false,
      source: "manifest",
      autoUpdate: true,
    });

    render(
      <App>
        <SettingsView />
      </App>,
    );
    await userEvent.click(screen.getByRole("button", { name: /技能管理/ }));
    await userEvent.click(
      await screen.findByRole("button", { name: "检查 watermark 依赖" }),
    );
    expect(
      (await screen.findAllByText("初始化 Skill 依赖")).length,
    ).toBeGreaterThan(0);
  });
});
