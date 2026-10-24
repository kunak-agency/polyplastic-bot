<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppDialog from '@/components/common/AppDialog.vue'
import FormField from '@/components/common/FormField.vue'
import { Button } from '@/components/ui/button'
import { DONE_NOTE_MAX, normDoneNote, type DoneNoteBlock } from '@/lib/tickets'

// "Resolve" (wave 4, #31): an optional line "what was done". It goes to the
// newcomer as "On your signal: ..." unless the ticket is anonymous or
// confidential (F5/F9/F10) — the hint says so before HR writes anything.
const props = defineProps<{
  number: number
  block: DoneNoteBlock
  busy: boolean
}>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ submit: [doneNote: string] }>()
const { t } = useI18n()

const note = ref('')
watch(open, (v) => {
  if (v) note.value = ''
})

const tooLong = computed(() => normDoneNote(note.value).length > DONE_NOTE_MAX)
const hint = computed(() =>
  props.block ? t(`tickets.resolve.notSent.${props.block}`) : t('tickets.resolve.sent'),
)

function submit(): void {
  if (tooLong.value) return
  emit('submit', normDoneNote(note.value))
}
</script>

<template>
  <AppDialog
    v-model:open="open"
    :title="t('tickets.resolve.title', { number })"
    :description="t('tickets.resolve.description')"
  >
    <form id="resolve-form" class="resolve" novalidate @submit.prevent="submit">
      <FormField
        :label="t('tickets.resolve.doneNote')"
        html-for="resolve-note"
        :hint="hint"
        :error="tooLong ? t('tickets.resolve.tooLong', { max: DONE_NOTE_MAX }) : undefined"
      >
        <input
          id="resolve-note"
          v-model="note"
          class="input"
          autocomplete="off"
          :maxlength="DONE_NOTE_MAX + 50"
          :placeholder="t('tickets.resolve.doneNotePlaceholder')"
        />
      </FormField>
    </form>
    <template #foot>
      <Button type="button" @click="open = false">{{ t('common.actions.cancel') }}</Button>
      <Button type="submit" form="resolve-form" variant="primary" :disabled="busy || tooLong">
        {{ t('tickets.resolve.submit') }}
      </Button>
    </template>
  </AppDialog>
</template>

<style scoped>
.resolve {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
</style>
