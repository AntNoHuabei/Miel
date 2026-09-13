import { useCallback, useEffect, useState } from 'react'
import { App as AntApp, Button, Empty, Input, Pagination, Popconfirm, Spin, Switch, Tabs, Tooltip, Typography } from 'antd'
import { AppstoreOutlined, DeleteOutlined, ReloadOutlined, UploadOutlined } from '@ant-design/icons'
import type { SkillHubSkillLite, SkillLite } from '../../../api'
import { skillRepository } from '../../../shared/repositories'

const { Title, Text } = Typography
const PAGE_SIZE = 12

export function SkillsSettingsPage() {
  const { message } = AntApp.useApp()
  const [skills, setSkills] = useState<SkillLite[]>([])
  const [loading, setLoading] = useState(true)
  const [importing, setImporting] = useState(false)
  const [hubSkills, setHubSkills] = useState<SkillHubSkillLite[]>([])
  const [tab, setTab] = useState<'installed' | 'skillhub'>('installed')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [keyword, setKeyword] = useState('')
  const [hubLoading, setHubLoading] = useState(false)
  const [installing, setInstalling] = useState('')

  const loadInstalled = useCallback(async () => {
    setLoading(true)
    try { setSkills(await skillRepository.list()) } catch (error) { message.error(`加载技能失败:${String(error)}`) } finally { setLoading(false) }
  }, [message])

  const loadHub = useCallback(async (nextPage = 1, query = '') => {
    setHubLoading(true)
    try {
      const result = await skillRepository.listHub(nextPage, PAGE_SIZE, query)
      setHubSkills(result.skills)
      setPage(result.page)
      setTotal(result.total)
    } catch (error) {
      message.error(`加载 SkillHub 列表失败:${String(error)}`)
    } finally {
      setHubLoading(false)
    }
  }, [message])

  useEffect(() => { void loadInstalled() }, [loadInstalled])
  useEffect(() => { if (tab === 'skillhub' && hubSkills.length === 0) void loadHub(1, keyword) }, [tab, hubSkills.length, keyword, loadHub])

  const importLocal = async () => {
    setImporting(true)
    try { await skillRepository.pickLocal(); message.success('技能已导入'); await loadInstalled() } catch (error) { message.error(String(error)) } finally { setImporting(false) }
  }

  const toggleSkill = async (skill: SkillLite, enabled: boolean) => {
    try {
      await skillRepository.setEnabled(skill.name, enabled)
      setSkills((items) => items.map((item) => item.name === skill.name ? { ...item, enabled } : item))
    } catch (error) { message.error(String(error)) }
  }

  const deleteSkill = async (skill: SkillLite) => {
    try { await skillRepository.delete(skill.name); message.success('技能已删除'); await loadInstalled() } catch (error) { message.error(String(error)) }
  }

  const installFromHub = async (skill: SkillHubSkillLite) => {
    setInstalling(skill.slug)
    try { await skillRepository.installHub(skill.slug); message.success(`已安装 ${skill.name || skill.slug}`); await loadInstalled() } catch (error) { message.error(`安装失败:${String(error)}`) } finally { setInstalling('') }
  }

  return (
    <section className="bm-settings-panel bm-skills-panel" aria-labelledby="settings-skills-title">
      <header className="bm-settings-page-heading"><div><span className="bm-settings-section-number">02</span><Title id="settings-skills-title" level={3}>技能管理</Title></div></header>
      <Tabs className="bm-skills-tabs" activeKey={tab} onChange={(key) => setTab(key as 'installed' | 'skillhub')} items={[
        {
          key: 'installed',
          label: <span className="bm-skills-tab-label">已安装 <b>{skills.length}</b></span>,
          children: <div className="bm-skills-tab-panel">
            <div className="bm-skills-toolbar"><Text type="secondary">管理 BlankMind 可加载的技能</Text><div><Tooltip title="刷新技能列表"><Button type="text" icon={<ReloadOutlined />} aria-label="刷新技能列表" onClick={() => void loadInstalled()} /></Tooltip><Button type="primary" icon={<UploadOutlined />} loading={importing} onClick={() => void importLocal()}>导入本地 Skill</Button></div></div>
            {loading ? <div className="bm-settings-loading"><Spin /></div> : skills.length === 0 ? <div className="bm-settings-empty"><Empty description="还没有安装 Skill" /></div> : (
              <div className="bm-skills-list"><div className="bm-skills-table-head"><span>技能</span><span>来源</span><span>状态</span></div>{skills.map((skill, index) => (
                <article className="bm-skills-row" key={skill.name}><span className="bm-skills-index">{String(index + 1).padStart(2, '0')}</span><div className="bm-skills-main"><Text strong ellipsis={{ tooltip: skill.name }}>{skill.name}</Text><Text type="secondary" ellipsis={{ tooltip: skill.description }}>{skill.description || '无描述'}</Text></div><span className={`bm-skills-source is-${skill.source}`}>{skill.source === 'builtin' ? '内置' : skill.source === 'skillhub' ? 'SkillHub' : '本地'}</span><div className="bm-skills-actions"><Switch size="small" checked={skill.enabled} aria-label={`${skill.enabled ? '停用' : '启用'} ${skill.name}`} onChange={(value) => void toggleSkill(skill, value)} />{skill.source !== 'builtin' && <Popconfirm title={`删除 ${skill.name}?`} onConfirm={() => void deleteSkill(skill)}><Tooltip title="删除技能"><Button type="text" danger icon={<DeleteOutlined />} aria-label={`删除 ${skill.name}`} /></Tooltip></Popconfirm>}</div></article>
              ))}</div>
            )}
          </div>,
        },
        {
          key: 'skillhub',
          label: <span className="bm-skills-tab-label">SkillHub {total > 0 && <b>{total.toLocaleString()}</b>}</span>,
          children: <div className="bm-skills-tab-panel">
            <div className="bm-skillhub-toolbar"><Input.Search value={keyword} onChange={(event) => setKeyword(event.target.value)} onSearch={(value) => void loadHub(1, value)} placeholder="搜索技能名称或描述" allowClear enterButton="搜索" aria-label="搜索 SkillHub 技能" /><Text type="secondary">{total > 0 ? `${total.toLocaleString()} 个技能` : 'SkillHub 公开技能库'}</Text></div>
            {hubLoading ? <div className="bm-settings-loading"><Spin /></div> : hubSkills.length === 0 ? <div className="bm-settings-empty"><Empty description="没有找到匹配的 Skill" /></div> : (
              <div className="bm-skillhub-list"><div className="bm-skillhub-table-head"><span>技能</span><span>分类</span><span>下载</span><span /></div>{hubSkills.map((skill, index) => {
                const installed = skills.some((item) => item.name === skill.slug)
                return <article className="bm-skillhub-row" key={`${skill.slug}-${skill.version}`}><span className="bm-skillhub-index">{String((page - 1) * PAGE_SIZE + index + 1).padStart(2, '0')}</span><div className="bm-skillhub-identity"><span className="bm-skillhub-icon">{skill.iconUrl ? <img src={skill.iconUrl} alt="" /> : <AppstoreOutlined />}</span><div><div className="bm-skillhub-name"><Text strong ellipsis={{ tooltip: skill.name || skill.slug }}>{skill.name || skill.slug}</Text>{skill.verified && <span>已审核</span>}</div><Text type="secondary" ellipsis={{ tooltip: skill.description }}>{skill.description || '暂无描述'}</Text></div></div><span className="bm-skillhub-category">{skill.category || '未分类'}</span><span className="bm-skillhub-downloads">{skill.downloads.toLocaleString()}</span><Button size="small" type={installed ? 'text' : 'primary'} disabled={installed} loading={installing === skill.slug} onClick={() => void installFromHub(skill)}>{installed ? '已安装' : '安装'}</Button></article>
              })}</div>
            )}
            {total > PAGE_SIZE && <div className="bm-skillhub-pagination"><Pagination current={page} total={total} pageSize={PAGE_SIZE} showSizeChanger={false} onChange={(nextPage) => void loadHub(nextPage, keyword)} /></div>}
          </div>,
        },
      ]} />
    </section>
  )
}
