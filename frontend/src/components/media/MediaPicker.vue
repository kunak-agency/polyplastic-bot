<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { onClickOutside } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { ChevronDown, Loader2, TriangleAlert, Upload, X } from 'lucide-vue-next'
import MediaThumb from '@/components/media/MediaThumb.vue'
import { apiCreateMedia } from '@/api/media'
import { useMediaStore } from '@/stores/media'
import { useOperatorStore } from '@/stores/operator'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'
import {
  MAX_MEDIA_MB,
  MEDIA_ACCEPT,
  mediaFileProblem,
  mediaKindOfFile,
  mediaNameFromFile,
  mediaNameKey,
  pickerOptions,
} from '@/lib/media'
import type { MediaItem, MediaKind, MediaRef, SiteCode } from '@/types/api'

// Media picker for a step or a branch message: chips of the chosen entries and
// a searchable select over the media library, plus "Upload" that adds a new
// entry and picks it. v-model is the step's `media` list (MediaRef[]): a pick
// writes { media_id, name }; refs that came by name only (an import) resolve
// by name for display and stay as they are.
const props = withDefaults(
  defineProps<{
    // Single pick replaces the list; multiple appends (a gallery).
    multiple?: boolean
    // Up to N refs (MAX: 12 images in one message).
    max?: number
    // Only entries of this kind are offered (and accepted on upload).
    kind?: MediaKind | null
    // Which site's file the previews show (null: common).
    site?: SiteCode | null
    disabled?: boolean
    // Accessible label of the select.
    label?: string
  }>(),
  { multiple: true, max: 12, kind: null, site: null, disabled: false, label: undefined },
)
const model = defineModel<MediaRef[]>({ default: () => [] })
const emit = defineEmits<{ uploaded: [item: MediaItem] }>()
const { t } = useI18n()
const store = useMediaStore()
const operator = useOperatorStore()
const toast = useToast()

onMounted(() => void store.ensureLoaded())

function resolve(ref_: MediaRef): MediaItem | null {
  if (ref_.media_id) {
    const byId = store.items.find((m) => m.id === ref_.media_id)
    if (byId) return byId
  }
  const key = mediaNameKey(ref_.name)
  return (
    store.items.find((m) => mediaNameKey(m.name) === key) ??
    store.items.find((m) => m.aliases.includes(key)) ??
    null
  )
}

const chosen = computed(() => model.value.map((r) => ({ ref: r, item: resolve(r) })))
const full = computed(() => props.multiple && model.value.length >= props.max)

const open = ref(false)
const query = ref('')
const root = ref<HTMLElement | null>(null)
onClickOutside(root, () => (open.value = false))

const options = computed(() => {
  const taken = new Set(chosen.value.map((c) => c.item?.id).filter(Boolean))
  return pickerOptions(store.items, query.value, props.kind).filter((m) => !taken.has(m.id))
})

function pick(item: MediaItem): void {
  const r: MediaRef = { media_id: item.id, name: item.name }
  model.value = props.multiple ? [...model.value, r] : [r]
  query.value = ''
  open.value = false
}

function removeAt(i: number): void {
  model.value = model.value.filter((_, j) => j !== i)
}

const input = ref<HTMLInputElement | null>(null)
const uploading = ref(false)
async function onPicked(e: Event): Promise<void> {
  const el = e.target as HTMLInputElement
  const file = el.files?.[0]
  el.value = ''
  if (!file) return
  const problem = mediaFileProblem(file.name, file.size)
  const kindOk = !props.kind || mediaKindOfFile(file.name) === props.kind
  if (problem || !kindOk) {
    toast.error(t(`media.problem.${problem ?? 'format'}`, { mb: MAX_MEDIA_MB }))
    return
  }
  uploading.value = true
  try {
    const item = await apiCreateMedia({
      file,
      name: mediaNameFromFile(file.name),
      updated_by: operator.name,
    })
    store.upsert(item)
    emit('uploaded', item)
    pick(item)
  } catch (err) {
    toast.error(errorMessage(err, 'media.toast.failed'))
  } finally {
    uploading.value = false
  }
}
</script>

<template>
  <div ref="root" class="mpk" :class="{ 'mpk--disabled': disabled }">
    <ul v-if="chosen.length > 0" class="mpk-chosen">
      <li
        v-for="(c, i) in chosen"
        :key="`${c.ref.media_id ?? ''}:${c.ref.name}:${i}`"
        class="mpk-chip"
      >
        <MediaThumb v-if="c.item" :item="c.item" :site="site" :size="22" />
        <span class="mpk-chip__name">{{ c.item?.name ?? c.ref.name }}</span>
        <span v-if="!c.item" class="mpk-chip__warn">
          <TriangleAlert :size="11" aria-hidden="true" />{{ t('media.picker.notInLibrary') }}
        </span>
        <span v-else-if="c.item.status === 'missing'" class="mpk-chip__warn">
          <TriangleAlert :size="11" aria-hidden="true" />{{ t('media.picker.missing') }}
        </span>
        <button
          type="button"
          class="mpk-chip__x"
          :disabled="disabled"
          :aria-label="t('media.picker.remove', { name: c.item?.name ?? c.ref.name })"
          @click="removeAt(i)"
        >
          <X :size="11" />
        </button>
      </li>
    </ul>

    <div class="mpk-row">
      <div class="mpk-select">
        <input
          v-model="query"
          class="input mpk-input"
          type="search"
          role="combobox"
          :aria-expanded="open"
          :aria-label="label ?? t('media.picker.label')"
          :placeholder="full ? t('media.picker.max', { n: max }) : t('media.picker.placeholder')"
          :disabled="disabled || full"
          @focus="open = true"
          @keydown.esc="open = false"
        />
        <ChevronDown class="mpk-caret" :size="14" aria-hidden="true" />
        <ul v-if="open && !full" class="mpk-list pop" role="listbox">
          <li v-if="store.error && !store.loaded" class="mpk-empty txt-sm">
            {{ t('media.picker.loadFailed') }}
          </li>
          <li v-else-if="options.length === 0" class="mpk-empty txt-sm muted">
            {{ t('media.picker.none') }}
          </li>
          <li v-for="m in options" :key="m.id" role="option" :aria-selected="false">
            <button type="button" class="mpk-opt" @click="pick(m)">
              <MediaThumb :item="m" :site="site" :size="28" />
              <span class="mpk-opt__name">{{ m.name }}</span>
              <span class="mpk-opt__meta txt-xs muted">{{ t(`media.kind.${m.kind}`) }}</span>
              <span v-if="m.status === 'missing'" class="mpk-chip__warn txt-xs">
                {{ t('media.picker.missing') }}
              </span>
            </button>
          </li>
        </ul>
      </div>
      <input
        ref="input"
        type="file"
        class="mpk-hidden"
        :accept="MEDIA_ACCEPT"
        tabindex="-1"
        aria-hidden="true"
        @change="onPicked"
      />
      <button
        type="button"
        class="btn btn-secondary btn-sm"
        :disabled="disabled || full || uploading"
        @click="input?.click()"
      >
        <Loader2 v-if="uploading" :size="14" class="mpk-spin" aria-hidden="true" />
        <Upload v-else :size="14" aria-hidden="true" />
        {{ uploading ? t('media.picker.uploading') : t('media.picker.upload') }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.mpk {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.mpk--disabled {
  opacity: 0.6;
}
.mpk-chosen {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.mpk-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 100%;
  height: 30px;
  padding: 0 2px 0 4px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  font-size: var(--text-sm);
}
.mpk-chip__name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mpk-chip__warn {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  color: var(--warning);
  font-size: var(--text-xs);
  white-space: nowrap;
}
.mpk-chip__x {
  display: inline-grid;
  place-items: center;
  width: 22px;
  height: 22px;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: var(--fg-faint);
  cursor: pointer;
}
.mpk-chip__x:hover {
  background: var(--bg-muted);
  color: var(--fg);
}
.mpk-row {
  display: flex;
  align-items: center;
  gap: 6px;
}
.mpk-select {
  position: relative;
  flex: 1;
  min-width: 0;
}
.mpk-input {
  padding-right: 28px;
}
.mpk-caret {
  position: absolute;
  right: 9px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--fg-faint);
  pointer-events: none;
}
.mpk-list {
  position: absolute;
  z-index: 20;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  max-height: 260px;
  overflow-y: auto;
  margin: 0;
  padding: 4px;
  list-style: none;
}
.mpk-empty {
  padding: 8px;
}
.mpk-opt {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 4px 6px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--fg);
  text-align: left;
  cursor: pointer;
  font: inherit;
  font-size: var(--text-sm);
}
.mpk-opt:hover,
.mpk-opt:focus-visible {
  background: var(--surface-hover);
}
.mpk-opt__name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.mpk-hidden {
  display: none;
}
.mpk-spin {
  animation: mpk-spin 1s linear infinite;
}
@keyframes mpk-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
