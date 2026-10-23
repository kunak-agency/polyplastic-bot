import { http } from '@/api/http'
import type {
  KnowledgeAskRequest,
  KnowledgeAskResponse,
  KnowledgeDocument,
  KnowledgeDocumentList,
  KnowledgeDocumentVersion,
  KnowledgeUnansweredQuery,
  KnowledgeUnansweredResponse,
  KnowledgeUploadRequest,
  ListResponse,
} from '@/types/api'

// api_contract.md, v3 (knowledge base). The backend proxies to the rag service
// (which has no auth of its own) and checks the HR right first.

// Upload re-indexes synchronously on the rag side: with the dense model a
// hundred-clause regulation takes tens of seconds, far above the default 15 s.
const UPLOAD_TIMEOUT_MS = 200_000
// The check question runs the same pipeline the newcomer gets (≤ 8 s on the
// backend) — a bit of headroom on top.
const ASK_TIMEOUT_MS = 20_000

export async function apiListDocuments(): Promise<KnowledgeDocumentList> {
  const r = await http.get<KnowledgeDocumentList>('/documents')
  return r.data
}

export interface UploadProgress {
  loaded: number
  total: number
}

export async function apiUploadDocument(
  body: KnowledgeUploadRequest,
  onProgress?: (p: UploadProgress) => void,
  signal?: AbortSignal,
): Promise<KnowledgeDocument> {
  const form = new FormData()
  form.append('file', body.file)
  if (body.doc) form.append('doc', body.doc)
  if (body.sites && body.sites.length > 0) form.append('sites', body.sites.join(','))
  if (body.uploaded_by) form.append('uploaded_by', body.uploaded_by)
  const r = await http.post<KnowledgeDocument>('/documents', form, {
    // The instance default (JSON) would make axios serialise the FormData into
    // JSON; with multipart the browser then sets the boundary itself.
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: UPLOAD_TIMEOUT_MS,
    signal,
    onUploadProgress: (e) => {
      onProgress?.({ loaded: e.loaded, total: e.total ?? body.file.size })
    },
  })
  return r.data
}

export async function apiDocumentVersions(
  id: string,
): Promise<ListResponse<KnowledgeDocumentVersion>> {
  const r = await http.get<ListResponse<KnowledgeDocumentVersion>>(
    `/documents/${encodeURIComponent(id)}/versions`,
  )
  return r.data
}

export async function apiAskDocuments(body: KnowledgeAskRequest): Promise<KnowledgeAskResponse> {
  const r = await http.post<KnowledgeAskResponse>('/documents/ask', body, {
    timeout: ASK_TIMEOUT_MS,
  })
  return r.data
}

export async function apiUnanswered(
  query: KnowledgeUnansweredQuery = {},
): Promise<KnowledgeUnansweredResponse> {
  const r = await http.get<KnowledgeUnansweredResponse>('/knowledge/unanswered', {
    params: {
      site: query.site || undefined,
      days: query.days || undefined,
      include_test: query.include_test ? 'true' : undefined,
    },
  })
  return r.data
}
