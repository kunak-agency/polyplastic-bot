<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppDialog from '@/components/common/AppDialog.vue'
import FormField from '@/components/common/FormField.vue'
import { Button } from '@/components/ui/button'
import type { TicketAssignRequest } from '@/types/api'

// "Assign" (ux-spec 3.4): reassigning needs a comment (Leena) — the next
// person reads what was already done. The first assignment does not.
const props = defineProps<{
  number: number
  current: string | null
  me: string
  suggestions: string[]
  busy: boolean
}>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ submit: [body: TicketAssignRequest] }>()
const { t } = useI18n()

const name = ref('')
const comment = ref('')
const touched = ref(false)

watch(open, (v) => {
  if (!v) return
  name.value = props.current ? '' : props.me
  comment.value = ''
  touched.value = false
})

const reassign = computed(() => !!props.current && name.value.trim() !== props.current)
const nameError = computed(() =>
  touched.value && !name.value.trim() ? t('tickets.assign.assigneeRequired') : undefined,
)
const commentError = computed(() =>
  touched.value && reassign.value && !comment.value.trim()
    ? t('tickets.assign.commentRequired')
    : undefined,
)

function submit(): void {
  touched.value = true
  if (nameError.value || commentError.value) return
  emit('submit', { assignee_name: name.value.trim(), comment: comment.value.trim() })
}
</script>

<template>
  <AppDialog
    v-model:open="open"
    :title="t('tickets.assign.title', { number })"
    :description="t('tickets.assign.description')"
  >
    <form id="assign-form" class="assign" novalidate @submit.prevent="submit">
      <FormField
        :label="t('tickets.assign.assignee')"
        html-for="assign-name"
        required
        :error="nameError"
      >
        <div class="assign__row">
          <input
            id="assign-name"
            v-model="name"
            class="input"
            list="assign-names"
            autocomplete="off"
            :placeholder="t('tickets.assign.assigneePlaceholder')"
          />
          <Button type="button" size="sm" @click="name = me">{{ t('tickets.assign.me') }}</Button>
        </div>
        <datalist id="assign-names">
          <option v-for="s in suggestions" :key="s" :value="s" />
        </datalist>
      </FormField>
      <FormField
        :label="t('tickets.assign.comment')"
        html-for="assign-comment"
        :required="reassign"
        :error="commentError"
      >
        <textarea
          id="assign-comment"
          v-model="comment"
          class="textarea assign__comment"
          :placeholder="t('tickets.assign.commentPlaceholder')"
        />
      </FormField>
    </form>
    <template #foot>
      <Button type="button" @click="open = false">{{ t('common.actions.cancel') }}</Button>
      <Button type="submit" form="assign-form" variant="primary" :disabled="busy">
        {{ busy ? t('tickets.assign.submitting') : t('tickets.assign.submit') }}
      </Button>
    </template>
  </AppDialog>
</template>

<style scoped>
.assign {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.assign__row {
  display: flex;
  gap: 8px;
  align-items: center;
}
.assign__comment {
  height: 80px;
  padding: 8px 10px;
  resize: vertical;
}
</style>
