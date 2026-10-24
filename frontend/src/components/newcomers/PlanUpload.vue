<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { FileUp } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { apiUploadNewcomerFile, type NewcomerFile } from '@/api/newcomers'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'

// "Upload the adaptation plan": the newcomer's own file, which the step with
// media personal:adaptation_plan sends (api_contract.md v4). The server has no
// GET for it yet, so the card shows only what was uploaded in this visit.
const props = defineProps<{ newcomerId: string }>()
const { t, d } = useI18n()
const toast = useToast()
const MAX_BYTES = 20 * 1024 * 1024

const input = ref<HTMLInputElement | null>(null)
const busy = ref(false)
const uploaded = ref<NewcomerFile | null>(null)

async function onPick(e: Event): Promise<void> {
  const file = (e.target as HTMLInputElement).files?.[0]
  ;(e.target as HTMLInputElement).value = ''
  if (!file) return
  if (file.size > MAX_BYTES) {
    toast.error(t('newcomers.plan.tooBig'))
    return
  }
  busy.value = true
  try {
    uploaded.value = await apiUploadNewcomerFile(props.newcomerId, file)
    toast.success(t('newcomers.plan.done'))
  } catch (err) {
    toast.error(errorMessage(err, 'newcomers.plan.failed'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <span class="plan">
    <input
      ref="input"
      type="file"
      class="sr-only"
      accept=".pdf,.doc,.docx,.xls,.xlsx,.ppt,.pptx,.png,.jpg,.jpeg"
      @change="onPick"
    />
    <Button :disabled="busy" @click="input?.click()">
      <FileUp :size="14" aria-hidden="true" />
      {{
        busy
          ? t('newcomers.plan.uploading')
          : uploaded
            ? t('newcomers.plan.replace')
            : t('newcomers.plan.upload')
      }}
    </Button>
    <span v-if="uploaded" class="plan__file txt-sm muted">
      {{
        t('newcomers.plan.file', {
          name: uploaded.filename,
          date: d(new Date(uploaded.updated_at), 'short'),
        })
      }}
    </span>
  </span>
</template>

<style scoped>
.plan {
  display: inline-flex;
  flex-direction: column;
  gap: 2px;
}
.plan__file {
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
