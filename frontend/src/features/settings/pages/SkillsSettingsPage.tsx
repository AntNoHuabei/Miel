import { useCallback, useEffect, useState } from "react";
import {
  App as AntApp,
  Button,
  Empty,
  Input,
  Pagination,
  Popconfirm,
  Space,
  Spin,
  Switch,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import {
  AppstoreOutlined,
  DeleteOutlined,
  ReloadOutlined,
  ToolOutlined,
  UploadOutlined,
} from "@ant-design/icons";
import type {
  SkillDependencyPlanLite,
  SkillEnvironmentStatusLite,
  SkillHubSkillLite,
  SkillInstallProgressLite,
  SkillLite,
  SkillRuntimeStatusLite,
} from "../../../api";
import { skillRepository } from "../../../shared/repositories";
import { subscribeWailsEvent } from "../../../shared/wails/events";

const { Title, Text } = Typography;
const PAGE_SIZE = 12;

export function SkillsSettingsPage() {
  const { message, modal } = AntApp.useApp();
  const [skills, setSkills] = useState<SkillLite[]>([]);
  const [loading, setLoading] = useState(true);
  const [importing, setImporting] = useState(false);
  const [hubSkills, setHubSkills] = useState<SkillHubSkillLite[]>([]);
  const [tab, setTab] = useState<"installed" | "skillhub">("installed");
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [keyword, setKeyword] = useState("");
  const [hubLoading, setHubLoading] = useState(false);
  const [installing, setInstalling] = useState("");
  const [runtimes, setRuntimes] = useState<SkillRuntimeStatusLite[]>([]);
  const [environments, setEnvironments] = useState<
    Record<string, SkillEnvironmentStatusLite>
  >({});
  const [dependencyBusy, setDependencyBusy] = useState("");
  const [dependencyProgress, setDependencyProgress] = useState<
    Record<string, SkillInstallProgressLite>
  >({});

  const loadInstalled = useCallback(async () => {
    setLoading(true);
    try {
      const items = await skillRepository.list();
      setSkills(items);
      const statuses = await Promise.all(
        items.map(
          async (skill) =>
            [
              skill.name,
              await skillRepository.environmentStatus(skill.name),
            ] as const,
        ),
      );
      setEnvironments(Object.fromEntries(statuses));
      setRuntimes(await skillRepository.listRuntimes());
    } catch (error) {
      message.error(`加载技能失败:${String(error)}`);
    } finally {
      setLoading(false);
    }
  }, [message]);

  const loadHub = useCallback(
    async (nextPage = 1, query = "") => {
      setHubLoading(true);
      try {
        const result = await skillRepository.listHub(
          nextPage,
          PAGE_SIZE,
          query,
        );
        setHubSkills(result.skills);
        setPage(result.page);
        setTotal(result.total);
      } catch (error) {
        message.error(`加载 SkillHub 列表失败:${String(error)}`);
      } finally {
        setHubLoading(false);
      }
    },
    [message],
  );

  useEffect(() => {
    void loadInstalled();
  }, [loadInstalled]);
  useEffect(() => {
    if (tab === "skillhub" && hubSkills.length === 0) void loadHub(1, keyword);
  }, [tab, hubSkills.length, keyword, loadHub]);
  useEffect(() => {
    const offProgress = subscribeWailsEvent<SkillInstallProgressLite>(
      "skill.dependency.progress",
      (progress) => {
        setDependencyProgress((current) => ({ ...current, [progress.skill]: progress }));
        setEnvironments((current) => ({
          ...current,
          [progress.skill]: {
            ...(current[progress.skill] || {
              skill: progress.skill,
              updatedAt: 0,
            }),
            state: progress.state,
            error: progress.message,
          },
        }));
      },
    );
    const update = (status: SkillEnvironmentStatusLite) =>
      setEnvironments((current) => ({ ...current, [status.skill]: status }));
    const offReady = subscribeWailsEvent<SkillEnvironmentStatusLite>(
      "skill.dependency.ready",
      update,
    );
    const offFailed = subscribeWailsEvent<SkillEnvironmentStatusLite>(
      "skill.dependency.failed",
      update,
    );
    return () => {
      offProgress();
      offReady();
      offFailed();
    };
  }, []);

  const importLocal = async () => {
    setImporting(true);
    try {
      const installed = await skillRepository.pickLocal();
      await loadInstalled();
      await initializeImportedSkill(installed);
      message.success(installed.kind === "instruction" ? "文档型技能已安装" : "技能已安装并初始化");
    } catch (error) {
      message.error(String(error));
    } finally {
      setImporting(false);
    }
  };

  const toggleSkill = async (skill: SkillLite, enabled: boolean) => {
    try {
      if (enabled && !(await prepareDependencies(skill))) return;
      await skillRepository.setEnabled(skill.name, enabled);
      setSkills((items) =>
        items.map((item) =>
          item.name === skill.name ? { ...item, enabled } : item,
        ),
      );
    } catch (error) {
      message.error(String(error));
    }
  };

  const planSummary = (plan: SkillDependencyPlanLite) => (
    <div className="bm-skill-plan">
      <p>
        <b>运行时：</b>
        {plan.runtime || "无需运行时"}　<b>包管理器：</b>
        {plan.packageManager || "无"}
      </p>
      <p>
        <b>依赖文件：</b>
        {plan.dependencyFile || "无"}　<b>网络：</b>
        {plan.network ? "需要" : "不需要"}
      </p>
      <p>
        <b>锁文件：</b>
        {plan.lockfile || "初始化时生成"}　<b>更新：</b>
        {plan.autoUpdate ? "自动更新锁并重建" : "固定"}
      </p>
      {plan.dependencySources && plan.dependencySources.length > 0 && (
        <p>
          <b>依赖来源：</b>
          {plan.dependencySources.join("、")}
        </p>
      )}
      <p>
        <b>入口：</b>
        {plan.entryCommand
          ? `${plan.entryCommand} ${(plan.entryArgs || []).join(" ")}`
          : "未声明（仅加载知识）"}
      </p>
      {(plan.evidence || []).length > 0 && (
        <p>
          <b>判断依据：</b>
          {plan.evidence.join("、")}
        </p>
      )}
    </div>
  );

  const confirmPlan = (plan: SkillDependencyPlanLite) =>
    new Promise<boolean>((resolve) => {
      modal.confirm({
        title:
          plan.source === "ai" ? "确认 AI 生成的依赖草案" : "初始化 Skill 依赖",
        content: planSummary(plan),
        okText: "确认并初始化",
        cancelText: "取消",
        width: 620,
        onOk: () => resolve(true),
        onCancel: () => resolve(false),
      });
    });

  const prepareDependencies = async (
    skill: SkillLite,
    repair = false,
  ): Promise<boolean> => {
    setDependencyBusy(skill.name);
    try {
      let plan = await skillRepository.inspectDependencies(skill.name);
      if (plan.kind === "instruction" || plan.kind === "builtin") return true;
      if (plan.needsReview && plan.confidence !== "high")
        plan = await skillRepository.generateDependencyPlan(skill.name);
      if (plan.kind === "instruction") return true;
      if (plan.kind === "unresolved") {
        message.error(`无法启用 ${skill.name}：${plan.reason || "执行入口无法确定"}`);
        return false;
      }
      if (!(await confirmPlan(plan))) return false;
      if (plan.source !== "manifest")
        await skillRepository.confirmDependencyPlan(skill.name, plan);
      const status = repair
        ? await skillRepository.repair(skill.name)
        : await skillRepository.initialize(skill.name);
      setEnvironments((current) => ({ ...current, [skill.name]: status }));
      message.success(`${skill.name} 依赖已就绪`);
      return true;
    } catch (error) {
      message.error(`初始化依赖失败：${String(error)}`);
      return false;
    } finally {
      setDependencyBusy("");
    }
  };

  async function initializeImportedSkill(skill: SkillLite) {
    const previous = skills.find((item) => item.name === skill.name);
    setDependencyBusy(skill.name);
    setDependencyProgress((current) => ({
      ...current,
      [skill.name]: { skill: skill.name, stage: "detect", message: "正在检测依赖和执行入口", state: "initializing" },
    }));
    try {
      const plan = await skillRepository.inspectDependencies(skill.name);
      if (plan.kind === "instruction") {
        setDependencyProgress((current) => ({ ...current, [skill.name]: {skill:skill.name, stage:"ready", state:"not-required", message:"文档型 · 按说明使用"} }));
        await loadInstalled();
        return;
      }
      if (plan.kind === "unresolved" || !plan.canRun || !plan.entryCommand || !(plan.entryArgs || []).length) {
        throw new Error("无法安全确定 Skill 的运行时依赖或执行入口，已中断自动初始化");
      }
      const confirmed = plan.source === "manifest"
        ? plan
        : await skillRepository.confirmDependencyPlan(skill.name, plan);
      const normalized = confirmed as SkillDependencyPlanLite;
      if (!normalized.entryCommand || !(normalized.entryArgs || []).length) {
        throw new Error("Skill manifest 缺少可执行入口，已中断自动初始化");
      }
      const status = await skillRepository.initialize(skill.name);
      if (status.state !== "ready") throw new Error(status.error || "Skill 环境初始化未完成");
      setEnvironments((current) => ({ ...current, [skill.name]: status }));
      setDependencyProgress((current) => ({
        ...current,
        [skill.name]: { skill: skill.name, stage: "ready", message: "依赖环境已就绪", state: "ready" },
      }));
      if (!previous || previous.enabled) await skillRepository.setEnabled(skill.name, true);
      await loadInstalled();
    } catch (error) {
      setDependencyProgress((current) => ({
        ...current,
        [skill.name]: { skill: skill.name, stage: "failed", message: String(error), state: "failed" },
      }));
      throw error;
    } finally {
      setDependencyBusy("");
    }
  }

  const environmentLabel = (skill: SkillLite) => {
    if (skill.kind === "instruction") return <Tag color="blue">文档型 · 按说明使用</Tag>;
    if (skill.kind === "unresolved") return <Tag color="warning">无法确定入口</Tag>;
    const state = environments[skill.name]?.state || "none";
    const labels: Record<string, string> = {
      none: "未初始化",
      "needs-review": "待确认",
      "not-required": "无需独立环境",
      initializing: "初始化中",
      ready: "已就绪",
      failed: "失败",
      stale: "需更新",
    };
    return (
      <Tag
        color={
          state === "ready"
            ? "success"
            : state === "failed"
              ? "error"
              : state === "needs-review"
                ? "warning"
                : "default"
        }
      >
        {skill.kind === "executable" ? "可执行型 · " : ""}{labels[state] || state}
      </Tag>
    );
  };

  const deleteSkill = async (skill: SkillLite) => {
    try {
      await skillRepository.delete(skill.name);
      message.success("技能已删除");
      await loadInstalled();
    } catch (error) {
      message.error(String(error));
    }
  };

  const installFromHub = async (skill: SkillHubSkillLite) => {
    setInstalling(skill.slug);
    try {
      const installed = await skillRepository.installHub(skill.slug);
      await loadInstalled();
      await initializeImportedSkill(installed);
      message.success(installed.kind === "instruction" ? `${skill.name || skill.slug} 已安装` : `${skill.name || skill.slug} 已安装并初始化`);
    } catch (error) {
      message.error(`安装失败:${String(error)}`);
    } finally {
      setInstalling("");
    }
  };

  return (
    <section
      className="bm-settings-panel bm-skills-panel"
      aria-labelledby="settings-skills-title"
    >
      <header className="bm-settings-page-heading">
        <div>
          <span className="bm-settings-section-number">02</span>
          <Title id="settings-skills-title" level={3}>
            技能管理
          </Title>
        </div>
      </header>
      <Tabs
        className="bm-skills-tabs"
        activeKey={tab}
        onChange={(key) => setTab(key as "installed" | "skillhub")}
        items={[
          {
            key: "installed",
            label: (
              <span className="bm-skills-tab-label">
                已安装 <b>{skills.length}</b>
              </span>
            ),
            children: (
              <div className="bm-skills-tab-panel">
                <div className="bm-skill-runtimes">
                  <Text type="secondary">内置运行时</Text>
                  <Space size={6}>
                    {runtimes.map((runtime) => (
                      <Tag
                        key={runtime.name}
                        color={runtime.available ? "success" : "error"}
                      >
                        {runtime.name} {runtime.version}
                        {runtime.available ? "" : " 不可用"}
                      </Tag>
                    ))}
                  </Space>
                </div>
                <div className="bm-skills-toolbar">
                  <Text type="secondary">管理 Miel 可加载的技能</Text>
                  <div>
                    <Tooltip title="刷新技能列表">
                      <Button
                        type="text"
                        icon={<ReloadOutlined />}
                        aria-label="刷新技能列表"
                        onClick={() => void loadInstalled()}
                      />
                    </Tooltip>
                    <Button
                      type="primary"
                      icon={<UploadOutlined />}
                      loading={importing}
                      onClick={() => void importLocal()}
                    >
                      导入本地 Skill
                    </Button>
                  </div>
                </div>
                {loading ? (
                  <div className="bm-settings-loading">
                    <Spin />
                  </div>
                ) : skills.length === 0 ? (
                  <div className="bm-settings-empty">
                    <Empty description="还没有安装 Skill" />
                  </div>
                ) : (
                  <div className="bm-skills-list">
                    <div className="bm-skills-table-head">
                      <span>技能</span>
                      <span>来源</span>
                      <span>状态</span>
                    </div>
                    {skills.map((skill, index) => (
                      <article className="bm-skills-row" key={skill.name}>
                        <span className="bm-skills-index">
                          {String(index + 1).padStart(2, "0")}
                        </span>
                        <div className="bm-skills-main">
                          <Text strong ellipsis={{ tooltip: skill.name }}>
                            {skill.name}
                          </Text>
                          <div>
                            <Text
                              type="secondary"
                              ellipsis={{ tooltip: skill.description }}
                            >
                              {skill.description || "无描述"}
                            </Text>
                            {environmentLabel(skill)}
                            {skill.kind === "unresolved" && <Text type="warning">{skill.capabilityReason}</Text>}
                          </div>
                          {dependencyProgress[skill.name]?.message && (
                            <Text type={dependencyProgress[skill.name].state === "failed" ? "danger" : "secondary"} className="bm-skill-progress-message">
                              {dependencyProgress[skill.name].message}
                            </Text>
                          )}
                        </div>
                        <span className={`bm-skills-source is-${skill.source}`}>
                          {skill.source === "builtin"
                            ? "内置"
                            : skill.source === "skillhub"
                              ? "SkillHub"
                              : "本地"}
                        </span>
                        <div className="bm-skills-actions">
                          {skill.kind !== "instruction" && skill.kind !== "builtin" && <Tooltip
                            title={
                              environments[skill.name]?.state === "failed"
                                ? "修复依赖环境"
                                : "检查依赖"
                            }
                          >
                            <Button
                              type="text"
                              icon={<ToolOutlined />}
                              loading={dependencyBusy === skill.name}
                              aria-label={`检查 ${skill.name} 依赖`}
                              onClick={() =>
                                void prepareDependencies(
                                  skill,
                                  environments[skill.name]?.state === "failed",
                                )
                              }
                            />
                          </Tooltip>}
                          <Switch
                            size="small"
                            checked={skill.enabled}
                            aria-label={`${skill.enabled ? "停用" : "启用"} ${skill.name}`}
                            onChange={(value) => void toggleSkill(skill, value)}
                          />
                          {skill.source !== "builtin" && (
                            <Popconfirm
                              title={`删除 ${skill.name}?`}
                              onConfirm={() => void deleteSkill(skill)}
                            >
                              <Tooltip title="删除技能">
                                <Button
                                  type="text"
                                  danger
                                  icon={<DeleteOutlined />}
                                  aria-label={`删除 ${skill.name}`}
                                />
                              </Tooltip>
                            </Popconfirm>
                          )}
                        </div>
                      </article>
                    ))}
                  </div>
                )}
              </div>
            ),
          },
          {
            key: "skillhub",
            label: (
              <span className="bm-skills-tab-label">
                SkillHub {total > 0 && <b>{total.toLocaleString()}</b>}
              </span>
            ),
            children: (
              <div className="bm-skills-tab-panel">
                <div className="bm-skillhub-toolbar">
                  <Input.Search
                    value={keyword}
                    onChange={(event) => setKeyword(event.target.value)}
                    onSearch={(value) => void loadHub(1, value)}
                    placeholder="搜索技能名称或描述"
                    allowClear
                    enterButton="搜索"
                    aria-label="搜索 SkillHub 技能"
                  />
                  <Text type="secondary">
                    {total > 0
                      ? `${total.toLocaleString()} 个技能`
                      : "SkillHub 公开技能库"}
                  </Text>
                </div>
                {hubLoading ? (
                  <div className="bm-settings-loading">
                    <Spin />
                  </div>
                ) : hubSkills.length === 0 ? (
                  <div className="bm-settings-empty">
                    <Empty description="没有找到匹配的 Skill" />
                  </div>
                ) : (
                  <div className="bm-skillhub-list">
                    <div className="bm-skillhub-table-head">
                      <span>技能</span>
                      <span>分类</span>
                      <span>下载</span>
                      <span />
                    </div>
                    {hubSkills.map((skill, index) => {
                      const installed = skills.some(
                        (item) => item.name === skill.slug,
                      );
                      return (
                        <article
                          className="bm-skillhub-row"
                          key={`${skill.slug}-${skill.version}`}
                        >
                          <span className="bm-skillhub-index">
                            {String(
                              (page - 1) * PAGE_SIZE + index + 1,
                            ).padStart(2, "0")}
                          </span>
                          <div className="bm-skillhub-identity">
                            <span className="bm-skillhub-icon">
                              {skill.iconUrl ? (
                                <img src={skill.iconUrl} alt="" />
                              ) : (
                                <AppstoreOutlined />
                              )}
                            </span>
                            <div>
                              <div className="bm-skillhub-name">
                                <Text
                                  strong
                                  ellipsis={{
                                    tooltip: skill.name || skill.slug,
                                  }}
                                >
                                  {skill.name || skill.slug}
                                </Text>
                                {skill.verified && <span>已审核</span>}
                              </div>
                              <Text
                                type="secondary"
                                ellipsis={{ tooltip: skill.description }}
                              >
                                {skill.description || "暂无描述"}
                              </Text>
                            </div>
                          </div>
                          <span className="bm-skillhub-category">
                            {skill.category || "未分类"}
                          </span>
                          <span className="bm-skillhub-downloads">
                            {skill.downloads.toLocaleString()}
                          </span>
                          <Button
                            size="small"
                            type={installed ? "text" : "primary"}
                            disabled={installed}
                            loading={installing === skill.slug}
                            onClick={() => void installFromHub(skill)}
                          >
                            {installed ? "已安装" : "安装"}
                          </Button>
                        </article>
                      );
                    })}
                  </div>
                )}
                {total > PAGE_SIZE && (
                  <div className="bm-skillhub-pagination">
                    <Pagination
                      current={page}
                      total={total}
                      pageSize={PAGE_SIZE}
                      showSizeChanger={false}
                      onChange={(nextPage) => void loadHub(nextPage, keyword)}
                    />
                  </div>
                )}
              </div>
            ),
          },
        ]}
      />
    </section>
  );
}
