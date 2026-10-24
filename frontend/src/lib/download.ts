// File downloads from the API (scenario export, api_contract.md v4): the name
// from Content-Disposition and saving a Blob through a temporary link. Pure
// parsing lives here so the node tests load it directly.

// filename*=UTF-8''... (RFC 5987) wins over the ASCII filename="...".
export function filenameFromDisposition(header: string | null | undefined): string | null {
  if (!header) return null
  const star = /filename\*\s*=\s*([^']*)'[^']*'([^;]+)/i.exec(header)
  if (star) {
    try {
      return decodeURIComponent(star[2].trim().replace(/^"|"$/g, ''))
    } catch {
      // a broken percent-encoding falls back to the plain name
    }
  }
  const plain = /filename\s*=\s*("([^"]*)"|[^;]+)/i.exec(header)
  if (!plain) return null
  const name = (plain[2] ?? plain[1]).trim()
  return name === '' ? null : name
}

// A name the file system accepts: no path separators or control characters.
export function safeFilename(name: string, fallback: string): string {
  const cleaned = name
    .replace(/[\\/:*?"<>|]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
  return cleaned === '' ? fallback : cleaned
}

export function saveBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  a.rel = 'noopener'
  document.body.appendChild(a)
  a.click()
  a.remove()
  // The click starts the download synchronously; revoke on the next tick.
  setTimeout(() => URL.revokeObjectURL(url), 0)
}
