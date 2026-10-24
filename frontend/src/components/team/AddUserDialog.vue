<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppDialog from '@/components/common/AppDialog.vue'
import FormField from '@/components/common/FormField.vue'
import { Button } from '@/components/ui/button'
import { apiCreateUser } from '@/api/team'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'
import type { Role } from '@/types/api'

// "Add a recruiter" (hr_lead). The role can be changed to add a head of unit
// or another HR lead with the same form.
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ created: [] }>()
const { t } = useI18n()
const toast = useToast()

const MIN_PASSWORD = 8
const ROLES: Role[] = ['recruiter', 'manager', 'hr_lead']
const form = reactive({
  full_name: '',
  login: '',
  password: '',
  role: 'recruiter' as Role,
  manager_name: '',
})
const touched = ref(false)
const busy = ref(false)

watch(open, (v) => {
  if (!v) return
  Object.assign(form, {
    full_name: '',
    login: '',
    password: '',
    role: 'recruiter',
    manager_name: '',
  })
  touched.value = false
})

const errors = computed(() => {
  const out: Record<string, string> = {}
  if (!touched.value) return out
  if (!form.full_name.trim()) out.full_name = t('team.add.required')
  if (!form.login.trim()) out.login = t('team.add.required')
  if (form.password.length < MIN_PASSWORD)
    out.password = t('team.add.passwordShort', { n: MIN_PASSWORD })
  return out
})

async function submit(): Promise<void> {
  touched.value = true
  if (Object.keys(errors.value).length > 0) return
  busy.value = true
  try {
    await apiCreateUser({
      full_name: form.full_name.trim(),
      login: form.login.trim(),
      password: form.password,
      role: form.role,
      // The head of unit sees the newcomers whose card names him.
      manager_name:
        form.role === 'manager' ? form.manager_name.trim() || form.full_name.trim() : null,
    })
    toast.success(t('team.add.done'))
    open.value = false
    emit('created')
  } catch (e) {
    toast.error(errorMessage(e, 'team.add.failed'))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AppDialog v-model:open="open" :title="t('team.add.title')" :description="t('team.add.desc')">
    <form id="add-user-form" class="add-user" novalidate @submit.prevent="submit">
      <FormField
        :label="t('team.add.fullName')"
        html-for="au-name"
        required
        :error="errors.full_name"
      >
        <input id="au-name" v-model="form.full_name" class="input" autocomplete="off" />
      </FormField>
      <FormField :label="t('team.add.login')" html-for="au-login" required :error="errors.login">
        <input id="au-login" v-model="form.login" class="input" autocomplete="off" />
      </FormField>
      <FormField
        :label="t('team.add.password')"
        html-for="au-pass"
        required
        :error="errors.password"
      >
        <input
          id="au-pass"
          v-model="form.password"
          class="input"
          type="password"
          autocomplete="new-password"
        />
      </FormField>
      <FormField :label="t('team.add.role')" html-for="au-role">
        <select id="au-role" v-model="form.role" class="input">
          <option v-for="r in ROLES" :key="r" :value="r">{{ t(`auth.roles.${r}`) }}</option>
        </select>
      </FormField>
      <FormField
        v-if="form.role === 'manager'"
        :label="t('team.add.managerName')"
        :hint="t('team.add.managerNameHint')"
        html-for="au-mgr"
      >
        <input id="au-mgr" v-model="form.manager_name" class="input" autocomplete="off" />
      </FormField>
    </form>
    <template #foot>
      <Button type="button" @click="open = false">{{ t('common.actions.cancel') }}</Button>
      <Button type="submit" form="add-user-form" variant="primary" :disabled="busy">
        {{ busy ? t('team.add.submitting') : t('team.add.submit') }}
      </Button>
    </template>
  </AppDialog>
</template>

<style scoped>
.add-user {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
</style>
