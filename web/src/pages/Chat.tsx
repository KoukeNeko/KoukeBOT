import ChatComposer from '../features/chat/components/ChatComposer'
import ChatMessageList from '../features/chat/components/ChatMessageList'
import ConversationSidebar from '../features/chat/components/ConversationSidebar'
import NewConversationModal from '../features/chat/components/NewConversationModal'
import { useChatController } from '../features/chat/hooks/useChatController'

export default function Chat() {
  const controller = useChatController()

  return (
    <div className="persona-layout">
      {controller.showList && (
        <ConversationSidebar
          conversations={controller.conversations}
          selectedConversationID={controller.selectedConv?.conversationId ?? null}
          onSelectConversation={controller.handleSelectConv}
          onDeleteConversation={controller.handleDeleteConv}
          onOpenNewConversation={controller.openNewConversationModal}
          onReload={() => {
            void controller.reloadSidebar()
          }}
          getConvSourceBadge={controller.helpers.getConvSourceBadge}
          formatRelativeTime={controller.helpers.formatRelativeTime}
        />
      )}

      {controller.showChat && (
        <div className="persona-detail-col" style={{ padding: 0, display: 'flex', flexDirection: 'column', flex: 1 }}>
          <div className="chat-container">
            {!controller.selectedConv ? (
              <div className="empty-state" style={{ flexDirection: 'column', gap: '1rem' }}>
                <p>選擇一個對話或建立新對話開始聊天</p>
                <button onClick={controller.openNewConversationModal}>建立新對話</button>
              </div>
            ) : (
              <>
                <div className="chat-header">
                  <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
                    {controller.isMobile && (
                      <button className="btn-icon" onClick={controller.closeChatForMobile}>
                        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                          <path d="M19 12H5M12 19l-7-7 7-7" />
                        </svg>
                      </button>
                    )}
                    <div>
                      <h2 style={{ fontSize: controller.isMobile ? '1.1rem' : '1.5rem' }}>
                        {controller.helpers.getPersonaName(controller.selectedConv.personaId)}
                      </h2>
                      <div className="meta">Channel: {controller.selectedConv.channelId}</div>
                    </div>
                  </div>
                </div>

                <ChatMessageList
                  messages={controller.messages}
                  sending={controller.sending}
                  onReply={controller.handleReply}
                  bottomRef={controller.bottomRef}
                  expandedRawData={controller.expandedRawData}
                  expandedToolUsage={controller.expandedToolUsage}
                  showToonRawInput={controller.showToonRawInput}
                  setExpandedRawData={controller.setExpandedRawData}
                  setExpandedToolUsage={controller.setExpandedToolUsage}
                  setShowToonRawInput={controller.setShowToonRawInput}
                />

                <ChatComposer
                  input={controller.input}
                  sending={controller.sending}
                  selectedImages={controller.selectedImages}
                  isMobile={controller.isMobile}
                  fileInputRef={controller.fileInputRef}
                  onInputChange={controller.setInput}
                  onKeyDown={controller.handleKeyDown}
                  onSend={() => {
                    void controller.handleSend()
                  }}
                  onImagesChange={controller.setSelectedImages}
                />
              </>
            )}
          </div>
        </div>
      )}

      <NewConversationModal
        open={controller.showNewConvModal}
        personas={controller.personas}
        selectedPersonaID={controller.newConvPersonaId}
        onClose={() => controller.setShowNewConvModal(false)}
        onSelectPersona={controller.setNewConvPersonaId}
        onConfirm={() => {
          void controller.handleNewConv()
        }}
      />
    </div>
  )
}
