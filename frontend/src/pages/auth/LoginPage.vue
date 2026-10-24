<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLogo from '@/components/common/AppLogo.vue'
import { Button } from '@/components/ui/button'
import { useAuthStore } from '@/stores/auth'
import { errorMessage, isApiError } from '@/lib/apiError'
import { canOpen, homeRoute } from '@/lib/roles'

// Login and password, one error line, nothing else (api_contract_auth.md).
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const login = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

async function submit(): Promise<void> {
  if (!login.value.trim() || !password.value) {
    error.value = t('auth.login.required')
    return
  }
  busy.value = true
  error.value = ''
  try {
    const me = await auth.login(login.value.trim(), password.value)
    // Back to where the 401 caught the user, if the role may open it.
    const raw = typeof route.query.next === 'string' ? route.query.next : ''
    // "/" means "the start page", which depends on the role.
    const next = raw === '/' ? '' : raw
    const target = next ? router.resolve(next) : null
    if (target && target.name && canOpen(me.role, String(target.name))) {
      await router.replace(next)
    } else {
      await router.replace({ name: homeRoute(me.role) })
    }
  } catch (e) {
    error.value =
      isApiError(e) && e.status === 401
        ? t('auth.login.invalid')
        : errorMessage(e, 'auth.login.failed')
    password.value = ''
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <main class="login">
    <form class="login__card" novalidate @submit.prevent="submit">
      <AppLogo :size="36" wordmark />
      <h1 class="login__title">{{ t('auth.login.title') }}</h1>
      <label class="login__field">
        <span class="field-label">{{ t('auth.login.login') }}</span>
        <input
          v-model="login"
          class="input"
          name="username"
          autocomplete="username"
          autofocus
          :aria-invalid="!!error"
        />
      </label>
      <label class="login__field">
        <span class="field-label">{{ t('auth.login.password') }}</span>
        <input
          v-model="password"
          class="input"
          type="password"
          name="password"
          autocomplete="current-password"
          :aria-invalid="!!error"
        />
      </label>
      <p v-if="error" class="login__error" role="alert">{{ error }}</p>
      <Button type="submit" variant="primary" :disabled="busy" class="login__submit">
        {{ busy ? t('auth.login.submitting') : t('auth.login.submit') }}
      </Button>
    </form>
  </main>
</template>

<style scoped>
.login {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: var(--space-4);
  background: var(--bg-subtle);
}
.login__card {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  width: 100%;
  max-width: 360px;
  padding: var(--space-6, 24px);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  background: var(--surface);
  box-shadow: var(--shadow-sm);
}
.login__title {
  margin: var(--space-2) 0 0;
  font-size: var(--text-xl);
  font-weight: 600;
}
.login__field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.login__error {
  margin: 0;
  color: var(--danger);
  font-size: var(--text-sm);
}
.login__submit {
  margin-top: var(--space-2);
}
</style>
