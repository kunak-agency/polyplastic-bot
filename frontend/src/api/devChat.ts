import { http } from '@/api/http'
import type { DevChatResponse } from '@/types/devChat'

// Development only (APP_ENV=development and the bot on the fake MAX): the
// server answers 404 otherwise. A RAG answer can take tens of seconds and the
// server replies only after the bot is done, hence the long timeout.
const SLOW = { timeout: 120_000 }

export async function apiDevChat(newcomerId: string): Promise<DevChatResponse> {
  const r = await http.get<DevChatResponse>(`/dev/chat/${encodeURIComponent(newcomerId)}`)
  return r.data
}

export async function apiDevChatPress(
  newcomerId: string,
  messageId: string,
  payload: string,
): Promise<void> {
  await http.post(
    `/dev/chat/${encodeURIComponent(newcomerId)}/press`,
    { message_id: messageId, payload },
    SLOW,
  )
}

// The newcomer types text: same path as long polling (simulate-message).
export async function apiDevChatSay(newcomerId: string, text: string): Promise<void> {
  await http.post('/dev/simulate-message', { newcomer_id: newcomerId, text }, SLOW)
}
