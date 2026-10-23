// GET /api/dev/chat/{newcomer_id} (api_contract.md, "dev"): the newcomer's
// dialog as the fake MAX journal saw it. Development builds only.
export type DevChatFrom = 'bot' | 'newcomer' | 'toast'
export type DevChatKind = 'text' | 'press' | 'start'
export type DevChatButtonType = 'callback' | 'link' | 'message' | 'request_contact' | string

export interface DevChatButton {
  text: string
  type: DevChatButtonType
  payload?: string
  url?: string
}

export interface DevChatAttachment {
  type: string
  url: string
}

export interface DevChatMessage {
  id: string
  from: DevChatFrom
  kind?: DevChatKind
  text: string
  format: '' | 'markdown' | 'html' | string
  buttons: DevChatButton[][] | null
  attachments: DevChatAttachment[] | null
  notify: boolean
  payload?: string
  reply_to?: string
  edited: boolean
  at: string
  edited_at: string | null
}

export interface DevChatResponse {
  messages: DevChatMessage[]
  typing: boolean
}
