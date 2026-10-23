import { http } from '@/api/http'
import type {
  ListResponse,
  MediaEnsureResponse,
  MediaItem,
  MediaRenamed,
  MediaStatus,
  MediaUploadRequest,
  SiteCode,
} from '@/types/api'

// api_contract.md v4, media library. Same HR right as the knowledge base.

const UPLOAD_TIMEOUT_MS = 120_000

export async function apiListMedia(status?: MediaStatus): Promise<ListResponse<MediaItem>> {
  const r = await http.get<ListResponse<MediaItem>>('/media', {
    params: { status: status || undefined },
  })
  return r.data
}

function form(body: MediaUploadRequest): FormData {
  const f = new FormData()
  f.append('file', body.file)
  if (body.name) f.append('name', body.name)
  if (body.site) f.append('site', body.site)
  if (body.updated_by) f.append('updated_by', body.updated_by)
  return f
}

// The instance default (JSON) would serialise the FormData into JSON; with
// multipart the browser sets the boundary itself.
const MULTIPART = { 'Content-Type': 'multipart/form-data' }

// New entry with its first file. A name taken by a stub without files (an
// import placeholder) fills that stub; a name with files answers 409.
export async function apiCreateMedia(body: MediaUploadRequest): Promise<MediaItem> {
  const r = await http.post<MediaItem>('/media', form(body), {
    headers: MULTIPART,
    timeout: UPLOAD_TIMEOUT_MS,
  })
  return r.data
}

// Add or replace the file of a site (no site: the common file).
export async function apiPutMediaFile(
  id: string,
  body: Omit<MediaUploadRequest, 'name'>,
): Promise<MediaItem> {
  const r = await http.put<MediaItem>(`/media/${encodeURIComponent(id)}`, form(body), {
    headers: MULTIPART,
    timeout: UPLOAD_TIMEOUT_MS,
  })
  return r.data
}

export async function apiDeleteMediaFile(id: string, site: SiteCode | null): Promise<MediaItem> {
  const r = await http.delete<MediaItem>(
    `/media/${encodeURIComponent(id)}/files/${site ?? 'common'}`,
  )
  return r.data
}

// Rename; references by name in scenario drafts follow (updated_refs).
export async function apiRenameMedia(
  id: string,
  name: string,
  updatedBy?: string,
): Promise<MediaRenamed> {
  const r = await http.patch<MediaRenamed>(`/media/${encodeURIComponent(id)}`, {
    name,
    updated_by: updatedBy || undefined,
  })
  return r.data
}

// Only when used_in is 0, otherwise 409.
export async function apiDeleteMedia(id: string): Promise<void> {
  await http.delete(`/media/${encodeURIComponent(id)}`)
}

// Missing-status stubs for names the library does not know (scenario import).
export async function apiEnsureMedia(
  names: string[],
  kind?: 'image' | 'file',
): Promise<MediaEnsureResponse> {
  const r = await http.post<MediaEnsureResponse>('/media/ensure', { names, kind })
  return r.data
}

// The file's bytes as a Blob: previews go through the API client (operator
// header, mocks) rather than a bare <img src>.
export async function apiMediaContent(id: string, site?: SiteCode | null): Promise<Blob> {
  const r = await http.get<Blob>(`/media/${encodeURIComponent(id)}/content`, {
    params: { site: site || undefined },
    responseType: 'blob',
  })
  return r.data
}
