<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppDialog from '@/components/common/AppDialog.vue'
import FormField from '@/components/common/FormField.vue'
import { Button } from '@/components/ui/button'
import type { PublishApply, PublishRequest } from '@/types/api'

// "What changed" is mandatory (ux-spec 3.5): the version history is read by
// people who did not make the change.
defineProps<{ nextVersion: number; activeNewcomers: number; busy: boolean }>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ submit: [body: PublishRequest] }>()
const { t } = useI18n()

const comment = ref('')
const apply = ref<PublishApply>('new')
const touched = ref(false)

watch(open, (v) => {
  if (!v) return
  comment.value = ''
  apply.value = 'new'
  touched.value = false
})

function submit(): void {
  touched.value = true
  if (!comment.value.trim()) return
  emit('submit', { comment: comment.value.trim(), apply: apply.value })
}
</script>

<template>
  <AppDialog
    v-model:open="open"
    :title="t('scenarios.publish.title')"
    :description="t('scenarios.publish.description', { version: nextVersion })"
  >
    <form id="publish-form" class="publish" novalidate @submit.prevent="submit">
      <FormField
        :label="t('scenarios.publish.comment')"
        html-for="pub-comment"
        required
        :error="touched && !comment.trim() ? t('scenarios.publish.commentRequired') : undefined"
      >
        <textarea
          id="pub-comment"
          v-model="comment"
          class="textarea publish__comment"
          :class="{ err: touched && !comment.trim() }"
          :placeholder="t('scenarios.publish.commentPlaceholder')"
        />
      </FormField>
      <FormField :label="t('scenarios.publish.apply')">
        <div class="publish__radios" role="radiogroup" :aria-label="t('scenarios.publish.apply')">
          <label class="publish__radio">
            <input v-model="apply" type="radio" name="pub-apply" value="new" />
            {{ t('scenarios.publish.applyNew') }}
          </label>
          <label class="publish__radio">
            <input v-model="apply" type="radio" name="pub-apply" value="active" />
            {{
              t('scenarios.publish.applyActive', {
                n: t('scenarios.publish.activeCount', activeNewcomers),
              })
            }}
          </label>
        </div>
      </FormField>
    </form>
    <template #foot>
      <Button type="button" @click="open = false">{{ t('common.actions.cancel') }}</Button>
      <Button type="submit" form="publish-form" variant="primary" :disabled="busy">
        {{ busy ? t('scenarios.publish.submitting') : t('scenarios.publish.submit') }}
      </Button>
    </template>
  </AppDialog>
</template>

<style scoped>
.publish {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.publish__comment {
  height: 96px;
  padding: 8px 10px;
  resize: vertical;
}
.publish__radios {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.publish__radio {
  display: inline-flex;
  gap: 8px;
  align-items: center;
  cursor: pointer;
}
</style>
