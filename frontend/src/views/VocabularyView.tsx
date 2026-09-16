import { useCallback, useEffect, useState } from 'react'
import { App as AntApp, Button, Empty, Form, Input, InputNumber, Modal, Segmented, Spin, Tooltip, Typography } from 'antd'
import {
  CheckOutlined,
  DeleteOutlined,
  EditOutlined,
  FormOutlined,
  PlusOutlined,
  ReloadOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import type { VocabularyReviewLite, VocabularyStatsLite, VocabularyWordInputLite, VocabularyWordLite } from '../shared/types/vocabulary'
import { vocabularyRepository } from '../shared/repositories'
import { useWailsEvent } from '../shared/wails/events'

const emptyStats: VocabularyStatsLite = { total: 0, unreviewed: 0, practising: 0, mastered: 0, reviewCount: 0 }

export default function VocabularyView() {
  const { message, modal } = AntApp.useApp()
  const [mode, setMode] = useState<'words' | 'review'>('words')
  const [words, setWords] = useState<VocabularyWordLite[]>([])
  const [stats, setStats] = useState(emptyStats)
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(true)
  const [editor, setEditor] = useState<VocabularyWordLite | 'new' | null>(null)
  const [reviewCount, setReviewCount] = useState(3)
  const [review, setReview] = useState<VocabularyReviewLite | null>(null)
  const [answer, setAnswer] = useState('')
  const [reference, setReference] = useState('')
  const [reviewLoading, setReviewLoading] = useState(false)
  const [referenceLoading, setReferenceLoading] = useState(false)

  const load = useCallback(async (search = query) => {
    try {
      const [items, summary] = await Promise.all([vocabularyRepository.list(search), vocabularyRepository.stats()])
      setWords(items)
      setStats(summary)
    } catch (error) {
      message.error(`读取生词本失败：${String(error)}`)
    } finally {
      setLoading(false)
    }
  }, [message, query])

  useEffect(() => { void load('') }, []) // eslint-disable-line react-hooks/exhaustive-deps
  useWailsEvent<VocabularyWordLite>('vocabulary.changed', useCallback(() => { void load(query) }, [load, query]))

  const startReview = async () => {
    setReviewLoading(true)
    try {
      const next = await vocabularyRepository.createReview(reviewCount)
      setReview(next)
      setAnswer('')
      setReference('')
      setMode('review')
    } catch (error) {
      message.error(String(error))
    } finally {
      setReviewLoading(false)
    }
  }

  const showReference = async () => {
    if (!review) return
    setReferenceLoading(true)
    try {
      setReference(await vocabularyRepository.generateSentence(review.words.map((word) => word.id)))
    } catch (error) {
      message.error(`生成参考句失败：${String(error)}`)
    } finally {
      setReferenceLoading(false)
    }
  }

  const grade = async (mastered: boolean) => {
    if (!review) return
    try {
      await vocabularyRepository.recordReview(review.words.map((word) => word.id), mastered)
      message.success(mastered ? '已记录为掌握' : '已加入后续复习')
      await startReview()
      await load(query)
    } catch (error) {
      message.error(String(error))
    }
  }

  const remove = (word: VocabularyWordLite) => {
    modal.confirm({
      title: `删除“${word.term}”？`,
      content: '复习记录也会一并删除。',
      okText: '删除',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: async () => {
        await vocabularyRepository.delete(word.id)
        message.success('已删除')
        await load(query)
      },
    })
  }

  return <div className="bm-vocabulary-page">
    <section className="bm-vocabulary-summary" aria-label="生词统计">
      <Stat value={stats.total} label="全部生词" />
      <Stat value={stats.unreviewed} label="尚未复习" />
      <Stat value={stats.practising} label="练习中" />
      <Stat value={stats.mastered} label="已掌握" />
      <div className="bm-vocabulary-shortcut"><span>划词收藏</span><kbd>Alt</kbd><b>+</b><kbd>W</kbd></div>
    </section>

    <div className="bm-vocabulary-toolbar">
      <Segmented
        value={mode}
        options={[{ label: '词表', value: 'words' }, { label: '组句复习', value: 'review' }]}
        onChange={(value) => setMode(value as 'words' | 'review')}
      />
      <span className="bm-vocabulary-toolbar-spacer" />
      {mode === 'words' && <Input allowClear prefix={<SearchOutlined />} value={query} placeholder="搜索词语、释义或例句" onChange={(event) => { const value = event.target.value; setQuery(value); void load(value) }} />}
      <Button icon={<PlusOutlined />} onClick={() => setEditor('new')}>添加生词</Button>
      <Button type="primary" icon={<FormOutlined />} loading={reviewLoading} onClick={() => void startReview()}>开始复习</Button>
    </div>

    {mode === 'words'
      ? <WordList loading={loading} words={words} onEdit={setEditor} onDelete={remove} />
      : <ReviewPanel
          review={review}
          count={reviewCount}
          answer={answer}
          reference={reference}
          loading={reviewLoading}
          referenceLoading={referenceLoading}
          onCountChange={setReviewCount}
          onAnswerChange={setAnswer}
          onStart={() => void startReview()}
          onReference={() => void showReference()}
          onGrade={(mastered) => void grade(mastered)}
        />}

    <WordEditor open={editor !== null} word={editor === 'new' ? null : editor} onClose={() => setEditor(null)} onSaved={async () => { setEditor(null); await load(query) }} />
  </div>
}

function Stat({ value, label }: { value: number; label: string }) {
  return <div className="bm-vocabulary-stat"><strong>{value}</strong><span>{label}</span></div>
}

function WordList({ loading, words, onEdit, onDelete }: { loading: boolean; words: VocabularyWordLite[]; onEdit: (word: VocabularyWordLite) => void; onDelete: (word: VocabularyWordLite) => void }) {
  if (loading) return <div className="bm-vocabulary-centered"><Spin /></div>
  if (!words.length) return <div className="bm-vocabulary-centered"><Empty description="还没有生词" /></div>
  return <div className="bm-vocabulary-list" role="list">
    {words.map((word) => <article className="bm-vocabulary-row" role="listitem" key={word.id}>
      <div className="bm-vocabulary-term"><strong>{word.term}</strong><span>{word.reviewCount ? `已复习 ${word.reviewCount} 次` : '尚未复习'}</span></div>
      <div className="bm-vocabulary-detail">
        <span>{word.meaning || '未填写释义'}</span>
        {word.example && <q>{word.example}</q>}
      </div>
      <div className="bm-vocabulary-row-actions">
        <Tooltip title="编辑"><Button type="text" aria-label={`编辑 ${word.term}`} icon={<EditOutlined />} onClick={() => onEdit(word)} /></Tooltip>
        <Tooltip title="删除"><Button type="text" danger aria-label={`删除 ${word.term}`} icon={<DeleteOutlined />} onClick={() => onDelete(word)} /></Tooltip>
      </div>
    </article>)}
  </div>
}

interface ReviewPanelProps {
  review: VocabularyReviewLite | null
  count: number
  answer: string
  reference: string
  loading: boolean
  referenceLoading: boolean
  onCountChange: (value: number) => void
  onAnswerChange: (value: string) => void
  onStart: () => void
  onReference: () => void
  onGrade: (mastered: boolean) => void
}

function ReviewPanel(props: ReviewPanelProps) {
  const { review, count, answer, reference, loading, referenceLoading, onCountChange, onAnswerChange, onStart, onReference, onGrade } = props
  return <section className="bm-vocabulary-review">
    <header className="bm-vocabulary-review-header">
      <div><Typography.Title level={3}>随机组句</Typography.Title><Typography.Text type="secondary">用抽到的全部词语写一个自然句子。</Typography.Text></div>
      <label>每组 <InputNumber min={1} max={8} value={count} onChange={(value) => onCountChange(value ?? 3)} /> 个词</label>
    </header>
    {!review
      ? <div className="bm-vocabulary-review-empty"><FormOutlined /><strong>准备好后抽取一组生词</strong><Button type="primary" loading={loading} onClick={onStart}>抽取生词</Button></div>
      : <>
        <div className="bm-vocabulary-word-track">{review.words.map((word, index) => <div className="bm-vocabulary-word-chip" key={word.id}><span>{String(index + 1).padStart(2, '0')}</span><strong>{word.term}</strong>{word.meaning && <small>{word.meaning}</small>}</div>)}</div>
        <Input.TextArea className="bm-vocabulary-answer" autoSize={{ minRows: 4, maxRows: 8 }} value={answer} placeholder="在这里写下你的句子" onChange={(event) => onAnswerChange(event.target.value)} />
        <div className="bm-vocabulary-review-actions">
          <Button icon={<ReloadOutlined />} loading={loading} onClick={onStart}>换一组</Button>
          <Button loading={referenceLoading} disabled={!answer.trim()} onClick={onReference}>查看参考句</Button>
        </div>
        {reference && <div className="bm-vocabulary-reference"><span>参考句</span><p>{reference}</p><div><Button icon={<ReloadOutlined />} onClick={() => onGrade(false)}>再练一次</Button><Button type="primary" icon={<CheckOutlined />} onClick={() => onGrade(true)}>已经掌握</Button></div></div>}
      </>}
  </section>
}

function WordEditor({ open, word, onClose, onSaved }: { open: boolean; word: VocabularyWordLite | null; onClose: () => void; onSaved: () => Promise<void> }) {
  const { message } = AntApp.useApp()
  const [form] = Form.useForm<VocabularyWordInputLite>()
  const [saving, setSaving] = useState(false)
  useEffect(() => {
    if (!open) return
    form.setFieldsValue({ id: word?.id ?? 0, term: word?.term ?? '', meaning: word?.meaning ?? '', example: word?.example ?? '', source: word?.source ?? 'manual' })
  }, [form, open, word])
  const save = async (values: VocabularyWordInputLite) => {
    setSaving(true)
    try {
      if (word) await vocabularyRepository.update(values)
      else await vocabularyRepository.add(values)
      message.success(word ? '已更新' : '已加入生词本')
      await onSaved()
    } catch (error) {
      message.error(String(error))
    } finally {
      setSaving(false)
    }
  }
  return <Modal title={word ? '编辑生词' : '添加生词'} open={open} okText="保存" cancelText="取消" confirmLoading={saving} onCancel={onClose} onOk={() => form.submit()} destroyOnHidden>
    <Form form={form} layout="vertical" onFinish={(values) => void save(values)}>
      <Form.Item name="id" hidden><Input /></Form.Item><Form.Item name="source" hidden><Input /></Form.Item>
      <Form.Item name="term" label="词语或短语" rules={[{ required: true, whitespace: true, message: '请输入词语或短语' }, { max: 200, message: '不能超过 200 个字符' }]}><Input autoFocus /></Form.Item>
      <Form.Item name="meaning" label="释义"><Input /></Form.Item>
      <Form.Item name="example" label="例句"><Input.TextArea autoSize={{ minRows: 3, maxRows: 6 }} /></Form.Item>
    </Form>
  </Modal>
}
