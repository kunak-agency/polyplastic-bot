<script setup lang="ts">
import { computed, defineAsyncComponent, ref, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { Paperclip, X } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import type { MediaRef } from '@/types/api'

// Attachments of a message or a send_file action, by media library name.
// The media library's own picker (components/media/MediaPicker.vue, another
// workstream) is used once it exists; until then — a name select over the names
// the scenario already uses, plus a new name.
const props = withDefaults(
  defineProps<{ modelValue: MediaRef[]; names: string[]; mode?: 'multi' | 'single' }>(),
  { mode: 'multi' },
)
const emit = defineEmits<{ 'update:modelValue': [value: MediaRef[]] }>()
const { t } = useI18n()

const pickers = import.meta.glob<{ default: Component }>('../media/MediaPicker.vue')
const loader = Object.values(pickers)[0]
const MediaPicker = loader ? defineAsyncComponent(loader) : null

const draft = ref('')
const listId = `media-names-${Math.random().toString(36).slice(2, 8)}`
const options = computed(() =>
  props.names.filter((n) => !props.modelValue.some((r) => r.name === n)),
)

function add(): void {
  const name = draft.value.trim()
  draft.value = ''
  if (!name || props.modelValue.some((r) => r.name === name)) return
  emit('update:modelValue', props.mode === 'single' ? [{ name }] : [...props.modelValue, { name }])
}
function remove(i: number): void {
  emit(
    'update:modelValue',
    props.modelValue.filter((_, j) => j !== i),
  )
}
</script>

<template>
  <component
    :is="MediaPicker"
    v-if="MediaPicker"
    :model-value="modelValue"
    :multiple="mode === 'multi'"
    @update:model-value="emit('update:modelValue', $event)"
  />
  <div v-else class="media">
    <div v-if="modelValue.length > 0" class="media__chips">
      <span
        v-for="(r, i) in modelValue"
        :key="r.name"
        :class="['media__chip', { 'media__chip--missing': !r.media_id }]"
        :title="r.media_id ? t('scenarios.media.ready') : t('scenarios.media.missing')"
      >
        <Paperclip :size="12" />{{ r.name }}
        <button
          type="button"
          class="media__x"
          :aria-label="t('scenarios.media.remove', { name: r.name })"
          @click="remove(i)"
        >
          <X :size="12" />
        </button>
      </span>
    </div>
    <div v-if="mode === 'multi' || modelValue.length === 0" class="media__add">
      <input
        v-model="draft"
        class="input"
        :list="listId"
        :placeholder="t('scenarios.media.placeholder')"
        :aria-label="t('scenarios.media.placeholder')"
        @keydown.enter.prevent="add"
        @change="add"
      />
      <datalist :id="listId">
        <option v-for="n in options" :key="n" :value="n" />
      </datalist>
      <Button size="sm" variant="ghost" :disabled="!draft.trim()" @click="add">
        {{ t('scenarios.media.attach') }}
      </Button>
    </div>
  </div>
</template>

<style scoped>
.media {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.media__chips {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.media__chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 6px;
  border-radius: var(--radius-sm);
  background: var(--bg-muted);
  font-size: var(--text-sm);
}
.media__chip--missing {
  background: var(--warning-soft);
}
.media__x {
  display: inline-flex;
  color: var(--fg-muted);
  cursor: pointer;
}
.media__add {
  display: flex;
  gap: 6px;
}
</style>
