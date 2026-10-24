<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Sunrise } from 'lucide-vue-next'
import SectionCard from '@/components/common/SectionCard.vue'
import { Button } from '@/components/ui/button'
import { apiDevRecruiterMorning } from '@/api/team'
import { useToast } from '@/composables/useToast'
import { errorMessage, isApiError } from '@/lib/apiError'

// Demo (agent L, POST /api/dev/recruiter-morning): the recruiters' morning
// summary now, without waiting for 9:00 on a working day.
const { t } = useI18n()
const toast = useToast()
const busy = ref(false)

async function send(): Promise<void> {
  busy.value = true
  try {
    const n = await apiDevRecruiterMorning()
    if (n === null) toast.success(t('settings.demo.morningSent'))
    else if (n === 0) toast.info(t('settings.demo.morningNone'))
    else toast.success(t('settings.demo.morningSentN', { n }, n))
  } catch (e) {
    // The dev route exists only on the demo stand (APP_ENV=development).
    if (isApiError(e) && e.status === 404) toast.info(t('settings.demo.notHere'))
    else toast.error(errorMessage(e, 'settings.demo.morningFailed'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <SectionCard :title="t('settings.demo.title')" :desc="t('settings.demo.desc')">
    <Button :disabled="busy" @click="send">
      <Sunrise :size="14" aria-hidden="true" />{{ t('settings.demo.morning') }}
    </Button>
  </SectionCard>
</template>
