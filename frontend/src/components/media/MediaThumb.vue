<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { FileText, ImageOff } from 'lucide-vue-next'
import { apiMediaContent } from '@/api/media'
import { fileFor, formatLabel } from '@/lib/media'
import type { MediaItem, SiteCode } from '@/types/api'

// Preview of an entry: the image itself (fetched through the API client, so
// the operator header and the mocks apply), a format tile for documents, a
// placeholder for a stub without files.
const props = withDefaults(
  defineProps<{ item: MediaItem; site?: SiteCode | null; size?: number }>(),
  { site: null, size: 44 },
)

// The site's file, else the common one, else any: a preview of something.
const file = computed(() => fileFor(props.item, props.site) ?? props.item.files[0] ?? null)
const url = ref<string | null>(null)
const failed = ref(false)

function revoke(): void {
  if (url.value) URL.revokeObjectURL(url.value)
  url.value = null
}

let ticket = 0
watch(
  () => [props.item.id, props.item.kind, file.value?.sha256, file.value?.site] as const,
  async () => {
    const mine = ++ticket
    revoke()
    failed.value = false
    if (props.item.kind !== 'image' || !file.value) return
    try {
      const blob = await apiMediaContent(props.item.id, file.value.site)
      if (mine !== ticket) return
      url.value = URL.createObjectURL(blob)
    } catch {
      if (mine === ticket) failed.value = true
    }
  },
  { immediate: true },
)
onBeforeUnmount(revoke)
</script>

<template>
  <div
    class="mthumb"
    :class="{ 'mthumb--empty': !file }"
    :style="{ width: `${size}px`, height: `${size}px` }"
    aria-hidden="true"
  >
    <img v-if="url" :src="url" alt="" class="mthumb__img" />
    <template v-else-if="file && item.kind === 'file'">
      <FileText :size="Math.round(size * 0.4)" />
      <span class="mthumb__fmt">{{ formatLabel(file) }}</span>
    </template>
    <ImageOff v-else-if="!file || failed" :size="Math.round(size * 0.4)" />
    <span v-else class="mthumb__shimmer shimmer" />
  </div>
</template>

<style scoped>
.mthumb {
  position: relative;
  display: inline-flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 1px;
  flex-shrink: 0;
  overflow: hidden;
  border-radius: var(--radius-sm);
  border: 1px solid var(--border);
  background: var(--bg-muted);
  color: var(--fg-subtle);
}
.mthumb--empty {
  border-style: dashed;
  color: var(--fg-faint);
  background: var(--surface);
}
.mthumb__img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.mthumb__fmt {
  font-size: 9px;
  font-weight: 600;
  letter-spacing: 0.02em;
}
.mthumb__shimmer {
  position: absolute;
  inset: 0;
}
</style>
