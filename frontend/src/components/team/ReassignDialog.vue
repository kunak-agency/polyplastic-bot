<script setup lang="ts">
import { ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import AppDialog from '@/components/common/AppDialog.vue'
import FormField from '@/components/common/FormField.vue'
import { Button } from '@/components/ui/button'
import { apiReassign } from '@/api/team'
import { useUsersStore } from '@/stores/users'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'

// Mass reassignment (hr_lead): the newcomers ticked in the registry go to one
// recruiter, or to nobody ("" → recruiter_id null).
const open = defineModel<boolean>('open', { required: true })
const props = defineProps<{ newcomerIds: string[] }>()
const emit = defineEmits<{ done: [] }>()
const { t } = useI18n()
const toast = useToast()
const users = useUsersStore()
const { recruiters } = storeToRefs(users)

const target = ref('')
const busy = ref(false)

watch(open, (v) => {
  if (!v) return
  target.value = ''
  users.loadRecruiters(true).catch((e) => toast.error(errorMessage(e, 'team.usersLoadError')))
})

async function submit(): Promise<void> {
  busy.value = true
  try {
    const r = await apiReassign(props.newcomerIds, target.value || null)
    toast.success(t('team.reassign.done', { n: r.updated }, r.updated))
    open.value = false
    emit('done')
  } catch (e) {
    toast.error(errorMessage(e, 'team.reassign.failed'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AppDialog
    v-model:open="open"
    :title="t('team.reassign.title')"
    :description="t('team.reassign.desc', { n: newcomerIds.length }, newcomerIds.length)"
  >
    <form id="reassign-form" @submit.prevent="submit">
      <FormField :label="t('team.reassign.recruiter')" html-for="ra-rec">
        <select id="ra-rec" v-model="target" class="input">
          <option value="">{{ t('team.noRecruiter') }}</option>
          <option v-for="u in recruiters" :key="u.id" :value="u.id">{{ u.full_name }}</option>
        </select>
      </FormField>
    </form>
    <template #foot>
      <Button type="button" @click="open = false">{{ t('common.actions.cancel') }}</Button>
      <Button type="submit" form="reassign-form" variant="primary" :disabled="busy">
        {{ busy ? t('team.reassign.submitting') : t('team.reassign.submit') }}
      </Button>
    </template>
  </AppDialog>
</template>
