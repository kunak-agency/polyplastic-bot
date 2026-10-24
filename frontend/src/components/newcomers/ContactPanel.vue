<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { apiMarkContacted } from '@/api/team'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'

// "Contact" from the dashboard opens the card with this panel: the recruiter
// calls or writes outside the bot, then marks it. The Today row goes away and
// returns in 2 working days if the newcomer still does not answer.
const props = defineProps<{ newcomerId: string; phone: string }>()
const emit = defineEmits<{ done: []; close: [] }>()
const { t } = useI18n()
const toast = useToast()
const note = ref('')
const busy = ref(false)

async function mark(): Promise<void> {
  busy.value = true
  try {
    await apiMarkContacted(props.newcomerId, note.value.trim())
    toast.success(t('newcomers.contact.done'))
    emit('done')
  } catch (e) {
    toast.error(errorMessage(e, 'newcomers.contact.failed'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <form class="tt-panel contact" @submit.prevent="mark">
    <div class="contact__text">
      <div class="fw-semi">{{ t('newcomers.contact.title') }}</div>
      <p class="muted txt-sm">
        {{ t('newcomers.contact.hint') }}
        <a v-if="phone" :href="`tel:${phone}`" class="tabular">{{ phone }}</a>
      </p>
    </div>
    <label class="contact__note">
      <span class="field-label">{{ t('newcomers.contact.note') }}</span>
      <input v-model="note" class="input" :placeholder="t('newcomers.contact.notePlaceholder')" />
    </label>
    <div class="row gap-2">
      <Button type="button" @click="emit('close')">{{ t('common.actions.cancel') }}</Button>
      <Button type="submit" variant="primary" :disabled="busy">
        {{ t('newcomers.contact.mark') }}
      </Button>
    </div>
  </form>
</template>

<style scoped>
.contact {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  gap: 12px 16px;
  margin-top: 16px;
  padding: 12px 14px;
  border: 1px solid var(--accent-border);
  border-radius: var(--radius-md);
  background: var(--bg-subtle);
}
.contact__text {
  flex: 1 1 280px;
}
.contact__text p {
  margin: 4px 0 0;
}
.contact__note {
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex: 1 1 240px;
}
</style>
