import { useCallback, useEffect, useState } from 'react'
import { App as AntApp, Flex } from 'antd'
import { useStore } from 'zustand'
import { chatRepository } from '../shared/repositories'
import { useWailsEvent } from '../shared/wails/events'
import { useChatAttachments } from '../components/ChatAttachments'
import { useAgentPermissions } from '../hooks/useAgentPermissions'
import { ConversationComposer } from '../features/chat/components/ConversationComposer'
import { ConversationSidebar } from '../features/chat/components/ConversationSidebar'
import { ConversationViewport } from '../features/chat/components/ConversationViewport'
import { useChatControls } from '../features/chat/controllers/useChatControls'
import { useConversationRuntime } from '../features/chat/controllers/useConversationRuntime'
import { mainConversationStore } from '../features/chat/model/conversationStore'
import { useShellStore } from '../features/shell/shellStore'
import { useArtifactPreviewStore } from '../features/artifacts/artifactStore'
import { PlanPreviewPanel } from '../features/chat/components/PlanPreviewPanel'
import '../styles/chat-md.css'

const QUICK_PROMPTS = [
  '帮我生成最近一周的周报',
  '列出我当前所有待办',
  '我的里程碑进度如何',
]

export default function ChatView() {
  const { message } = AntApp.useApp()
  const activeConversationId = useStore(mainConversationStore, (state) => state.conversationId)
  const controls = useChatControls(activeConversationId)
  const [mode, setMode] = useState<'chat' | 'plan'>('chat')
  const sidebarOpen = useShellStore((state) => state.sidebarOpen)
  const newChatRequest = useShellStore((state) => state.newChatVersion)
  const openConversationRequest = useShellStore((state) => state.openConversationRequest)
  const activeView = useShellStore((state) => state.view)
  const reminderCount = useShellStore((state) => state.unread)
  const navigate = useShellStore((state) => state.navigate)
  const [conversations, setConversations] = useState<Array<{ id: number; title: string; workspacePath: string }>>([])
  const selectedPlan = useArtifactPreviewStore((state) => state.selectedPlan)
  const openPlan = useArtifactPreviewStore((state) => state.openPlan)
  const attachments = useChatAttachments()
  const permissions = useAgentPermissions()

  const reloadConversations = useCallback(async () => {
    try {
      const items = await chatRepository.listConversations()
      setConversations(items.map((item) => ({ id: item.id, title: item.title, workspacePath: item.workspacePath ?? '' })))
    }
    catch { /* Keep the last valid conversation list. */ }
  }, [])

  const runtime = useConversationRuntime({
    store: mainConversationStore,
    attachments: attachments.attachments,
    supportsImages: controls.supportsImages,
    enabled: controls.modelAvailable,
    requestPrefix: 'chat',
    unsupportedImagesMessage: '当前模型不支持图片输入，请切换到支持图片的模型',
    consumeAttachments: attachments.consumeAttachments,
    discardAttachments: attachments.discardAttachments,
    beforeConversationChange: permissions.resetSession,
    getRequestContext: () => ({
      reasoning: controls.effectiveReasoning,
      workspacePath: controls.currentWorkspace?.path ?? '',
      permissionSessionId: permissions.sessionId,
      mode,
      agentProfile: controls.agentProfile,
    }),
    onConversationCompleted: reloadConversations,
    onPlanGenerated: openPlan,
  })

  useEffect(() => { void reloadConversations() }, [reloadConversations])
  useEffect(() => {
    if (!selectedPlan) return
    const current = runtime.timeline.find((item) => item.kind === 'plan' && item.plan.id === selectedPlan.id && item.plan.revision === selectedPlan.revision)
    if (current?.kind === 'plan' && current.plan !== selectedPlan) openPlan(current.plan)
  }, [openPlan, runtime.timeline, selectedPlan])
  useWailsEvent<string>('conversations.changed', useCallback(() => void reloadConversations(), [reloadConversations]))

  useEffect(() => {
    if (newChatRequest > 0) {
      runtime.reset()
      setMode('chat')
    }
    // The shell version makes repeated new-chat requests imperative.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [newChatRequest])

  useEffect(() => {
    if (openConversationRequest?.id) {
      runtime.openConversation(openConversationRequest.id)
      navigate('chat')
    }
    // The request version permits reopening the same conversation.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [openConversationRequest.version])

  const deleteCurrent = async () => {
    if (!runtime.conversationId) return
    try {
      await chatRepository.deleteConversation(runtime.conversationId)
      runtime.reset()
      await reloadConversations()
      message.success('已删除该会话')
    } catch (error) {
      message.error(`删除失败:${String(error)}`)
    }
  }

  const openConversation = (id: number) => {
    runtime.openConversation(id)
    setMode('chat')
    navigate('chat')
  }
  const newChat = () => {
    runtime.reset()
    setMode('chat')
  }
  const showWorkspaceControl = runtime.conversationId === 0 && runtime.timeline.length === 0
  const reviseFromPreview = async (plan: Parameters<typeof runtime.runPlanAction>[1], instruction: string) => {
    await runtime.runPlanAction('revise', plan, instruction)
    const latest = mainConversationStore.getState().timeline
      .filter((item) => item.kind === 'plan' && item.plan.id === plan.id)
      .sort((left, right) => (right.kind === 'plan' ? right.plan.revision : 0) - (left.kind === 'plan' ? left.plan.revision : 0))[0]
    if (latest?.kind === 'plan') openPlan(latest.plan)
  }
  const executePlan = async (plan: Parameters<typeof runtime.runPlanAction>[1]) => {
    openPlan(plan)
    await runtime.runPlanAction('execute', plan)
  }
  return (
    <>
    <div className={`bm-chat bm-chat-shell ${sidebarOpen ? 'is-sidebar-open' : 'is-sidebar-collapsed'}`}>
      <ConversationSidebar
        activeView={activeView}
        agentProfile={controls.agentProfile}
        profileReady={controls.profileReady}
        sending={runtime.sending}
        conversations={conversations}
        workspaces={controls.workspaces}
        currentConversationId={runtime.conversationId}
        reminderCount={reminderCount}
        onDeleteCurrent={deleteCurrent}
        onNavigate={navigate}
        onChangeAgentProfile={controls.switchProfile}
        onNewChat={newChat}
        onOpenConversation={openConversation}
      />
      <Flex vertical className="bm-chat-main-shell" style={{ minWidth: 0, minHeight: 0 }}>
        <Flex vertical className="bm-chat-main" style={{ flex: 1, minWidth: 0, minHeight: 0 }}>
          <ConversationViewport
            conversationId={runtime.conversationId}
            timeline={runtime.timeline}
            streaming={runtime.run.streaming}
            sending={runtime.sending}
            phase={runtime.run.phase}
            process={runtime.run.process}
            tools={runtime.run.tools}
            skillProgress={runtime.run.skillProgress}
            artifacts={runtime.run.artifacts}
            error={runtime.run.error}
            pendingApproval={permissions.pendingApproval}
            resolvingApproval={permissions.resolving}
            scrollRef={runtime.scrollRef}
            onScroll={runtime.onMessagesScroll}
            quickPrompts={QUICK_PROMPTS}
            onResolveApproval={permissions.resolveApproval}
            onExecutePlan={executePlan}
            onRevisePlan={(plan, instruction) => runtime.runPlanAction('revise', plan, instruction)}
            onAbandonPlan={runtime.abandonPlan}
            onOpenPlan={openPlan}
            onQuickPrompt={(prompt) => {
              runtime.setInput(prompt)
              void (document.querySelector('#bm-chat-input') as HTMLTextAreaElement | null)?.focus()
            }}
          />
          <ConversationComposer
            input={runtime.input}
            mode={mode}
            sending={runtime.sending}
            attachments={attachments.attachments}
            supportsImages={controls.supportsImages}
            permissionMode={permissions.mode}
            showWorkspaceControl={showWorkspaceControl}
            workspaces={controls.workspaces}
            currentWorkspace={controls.currentWorkspace}
            selectedModel={controls.selectedModel}
            modelOptions={controls.modelOptions}
            activeModelLabel={controls.activeModelLabel}
            modelAvailable={controls.modelAvailable}
            profileReady={controls.profileReady}
            reasoningPillLabel={controls.reasoningPillLabel}
            reasoningStatus={controls.reasoningStatus}
            reasoningSteps={controls.reasoningSteps}
            reasoningIndex={controls.reasoningIndex}
            reasoningMarks={controls.reasoningMarks}
            reasoningLocked={controls.reasoningLocked}
            showCompatibleModelNote={controls.showCompatibleModelNote}
            onAddWorkspace={controls.addWorkspace}
            onChangeInput={runtime.setInput}
            onChangeMode={setMode}
            onChangePermissionMode={permissions.changeMode}
            onChangeReasoning={controls.changeReasoning}
            onChooseWorkspace={controls.chooseWorkspace}
            onPaste={attachments.onPaste}
            onPickImages={attachments.pickImages}
            onRemoveAttachment={attachments.removeAttachment}
            onRemoveWorkspace={controls.removeWorkspace}
            onSend={runtime.send}
            onStop={runtime.stop}
            onSwitchModel={controls.switchModel}
          />
        </Flex>
      </Flex>
    </div>
    <PlanPreviewPanel sending={runtime.sending} onExecute={executePlan} onRevise={reviseFromPreview} onAbandon={runtime.abandonPlan} />
    </>
  )
}
