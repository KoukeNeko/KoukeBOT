import type { MouseEvent } from 'react'
import type { ConversationSummary } from '../../../types'
import { SidebarItem, SidebarList } from '../../../components/SidebarList'

type Props = {
  conversations: ConversationSummary[]
  selectedConversationID: string | null
  onSelectConversation: (conv: ConversationSummary) => void
  onDeleteConversation: (conv: ConversationSummary, event: MouseEvent) => void
  onOpenNewConversation: () => void
  onReload: () => void
  getConvSourceBadge: (channelId: string) => { source: string; color: string }
  formatRelativeTime: (timestamp: string) => string
}

export default function ConversationSidebar({
  conversations,
  selectedConversationID,
  onSelectConversation,
  onDeleteConversation,
  onOpenNewConversation,
  onReload,
  getConvSourceBadge,
  formatRelativeTime,
}: Props) {
  return (
    <SidebarList title="對話" onReload={onReload}>
      <div style={{ padding: '0.75rem 1.25rem' }}>
        <button
          style={{ width: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '0.5rem' }}
          onClick={onOpenNewConversation}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <line x1="12" y1="5" x2="12" y2="19" />
            <line x1="5" y1="12" x2="19" y2="12" />
          </svg>
          新對話
        </button>
      </div>

      {conversations.length === 0 && (
        <p className="meta" style={{ padding: '1rem', textAlign: 'center' }}>
          尚無對話記錄
        </p>
      )}

      {conversations.map((conv) => {
        const { source, color } = getConvSourceBadge(conv.channel_id)
        return (
          <SidebarItem
            key={conv.id}
            active={selectedConversationID === conv.id}
            onClick={() => onSelectConversation(conv)}
            title={conv.is_active ? `${conv.persona_name} (Active)` : conv.persona_name}
            subtitle={
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%', gap: '0.5rem' }}>
                <span
                  style={{
                    fontSize: '0.65rem',
                    padding: '1px 5px',
                    borderRadius: '4px',
                    background: color,
                    color: 'white',
                    fontWeight: 500,
                    opacity: 0.9,
                    whiteSpace: 'nowrap',
                    flexShrink: 0,
                  }}
                >
                  {source}
                </span>
                <span style={{ fontSize: '0.7rem', color: '#666', whiteSpace: 'nowrap', flexShrink: 0 }}>
                  {formatRelativeTime(conv.last_timestamp)}
                </span>
              </div>
            }
            description={conv.last_message || '無訊息'}
          >
            <button
              className="btn-icon"
              title="刪除對話"
              onClick={(event) => onDeleteConversation(conv, event)}
              style={{ padding: '4px', opacity: 0.6, marginLeft: 'auto' }}
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <polyline points="3 6 5 6 21 6" />
                <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
              </svg>
            </button>
          </SidebarItem>
        )
      })}
    </SidebarList>
  )
}
