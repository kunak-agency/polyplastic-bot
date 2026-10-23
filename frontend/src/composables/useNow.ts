import { onBeforeUnmount, onMounted, ref, type Ref } from 'vue'

// A clock for countdowns (SLA timers): one tick per `everyMs`, stopped with the
// component. Pure functions take its value, so one render agrees on "now".
export function useNow(everyMs = 30_000): Ref<number> {
  const now = ref(Date.now())
  let timer: ReturnType<typeof setInterval> | undefined
  onMounted(() => {
    timer = setInterval(() => (now.value = Date.now()), everyMs)
  })
  onBeforeUnmount(() => clearInterval(timer))
  return now
}
