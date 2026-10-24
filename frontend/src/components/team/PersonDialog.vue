<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppDialog from '@/components/common/AppDialog.vue'
import FormField from '@/components/common/FormField.vue'
import { Button } from '@/components/ui/button'
import { apiCreatePerson } from '@/api/people'
import { apiPatchUser } from '@/api/team'
import { useToast } from '@/composables/useToast'
import { errorMessage } from '@/lib/apiError'

// People directory in Settings (hr_lead; api_contract_auth.md, "People"):
// "create" adds a manager or a mentor (a manager optionally with sign-in),
// "grant" gives sign-in to a manager created without it.
const props = defineProps<{
  mode: 'create' | 'grant'
  role: 'manager' | 'mentor'
  person?: { id: string; full_name: string } | null
}>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ done: [] }>()
const { t } = useI18n()
const toast = useToast()

const MIN_PASSWORD = 8
const form = reactive({ full_name: '', withLogin: false, login: '', password: '' })
const touched = ref(false)
const busy = ref(false)

watch(open, (v) => {
  if (!v) return
  Object.assign(form, { full_name: '', withLogin: props.mode === 'grant', login: '', password: '' })
  touched.value = false
})

const needsLogin = computed(() => props.mode === 'grant' || form.withLogin)
const title = computed(() =>
  props.mode === 'grant'
    ? t('people.users.grant.title', { name: props.person?.full_name ?? '' })
    : t(`people.users.create.title.${props.role}`),
)

const errors = computed(() => {
  const out: Record<string, string> = {}
  if (!touched.value) return out
  if (props.mode === 'create' && !form.full_name.trim()) out.full_name = t('team.add.required')
  if (needsLogin.value) {
    if (!form.login.trim()) out.login = t('team.add.required')
    if (form.password.length < MIN_PASSWORD)
      out.password = t('team.add.passwordShort', { n: MIN_PASSWORD })
  }
  return out
})

async function submit(): Promise<void> {
  touched.value = true
  if (Object.keys(errors.value).length > 0) return
  busy.value = true
  try {
    if (props.mode === 'grant' && props.person) {
      await apiPatchUser(props.person.id, { login: form.login.trim(), password: form.password })
      toast.success(t('people.users.grant.done'))
    } else {
      const p = await apiCreatePerson({
        full_name: form.full_name.trim(),
        role: props.role,
        ...(needsLogin.value ? { login: form.login.trim(), password: form.password } : {}),
      })
      toast.success(t('people.users.create.done', { name: p.full_name }))
    }
    open.value = false
    emit('done')
  } catch (e) {
    toast.error(
      errorMessage(
        e,
        props.mode === 'grant' ? 'people.users.grant.failed' : 'people.picker.failed',
      ),
    )
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <AppDialog
    v-model:open="open"
    :title="title"
    :description="mode === 'grant' ? t('people.users.grant.desc') : t('people.users.create.desc')"
  >
    <form id="person-form" class="person-form" novalidate @submit.prevent="submit">
      <FormField
        v-if="mode === 'create'"
        :label="t('people.picker.fullName')"
        html-for="pd-name"
        required
        :error="errors.full_name"
      >
        <input
          id="pd-name"
          v-model="form.full_name"
          class="input"
          autocomplete="off"
          :placeholder="t('people.picker.fullNamePlaceholder')"
        />
      </FormField>
      <label v-if="mode === 'create' && role === 'manager'" class="person-form__check">
        <input v-model="form.withLogin" type="checkbox" />
        {{ t('people.users.create.withLogin') }}
      </label>
      <template v-if="needsLogin">
        <FormField
          :label="t('people.users.grant.login')"
          html-for="pd-login"
          required
          :error="errors.login"
        >
          <input id="pd-login" v-model="form.login" class="input" autocomplete="off" />
        </FormField>
        <FormField
          :label="t('people.users.grant.password')"
          html-for="pd-pass"
          required
          :error="errors.password"
        >
          <input
            id="pd-pass"
            v-model="form.password"
            class="input"
            type="password"
            autocomplete="new-password"
          />
        </FormField>
      </template>
    </form>
    <template #foot>
      <Button type="button" @click="open = false">{{ t('common.actions.cancel') }}</Button>
      <Button type="submit" form="person-form" variant="primary" :disabled="busy">
        {{
          busy
            ? t('people.users.grant.submitting')
            : mode === 'grant'
              ? t('people.users.grant.submit')
              : t('people.users.create.submit')
        }}
      </Button>
    </template>
  </AppDialog>
</template>

<style scoped>
.person-form {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.person-form__check {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
}
</style>
