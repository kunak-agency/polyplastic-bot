import { defineStore } from 'pinia'
import { ref } from 'vue'
import { apiListMedia } from '@/api/media'
import type { MediaItem } from '@/types/api'

// The media library list, shared by the media page and MediaPicker in the
// scenario editor. Only the answer to the latest request lands.
export const useMediaStore = defineStore('media', () => {
  const items = ref<MediaItem[]>([])
  const loaded = ref(false)
  const loading = ref(false)
  const error = ref<unknown>(null)
  let ticket = 0

  async function load(): Promise<void> {
    const mine = ++ticket
    loading.value = true
    error.value = null
    try {
      const r = await apiListMedia()
      if (mine !== ticket) return
      items.value = r.items
      loaded.value = true
    } catch (e) {
      if (mine === ticket) error.value = e
    } finally {
      if (mine === ticket) loading.value = false
    }
  }

  // Load once; the picker calls this on mount.
  async function ensureLoaded(): Promise<void> {
    if (!loaded.value && !loading.value) await load()
  }

  function upsert(item: MediaItem): void {
    const i = items.value.findIndex((m) => m.id === item.id)
    if (i >= 0) items.value.splice(i, 1, item)
    else items.value.push(item)
  }

  function remove(id: string): void {
    items.value = items.value.filter((m) => m.id !== id)
  }

  return { items, loaded, loading, error, load, ensureLoaded, upsert, remove }
})
