import { defineConfig, loadEnv, type Plugin } from 'vite'
import { fileURLToPath, URL } from 'node:url'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// ИНВАРИАНТ: ноль внешних origin. В политике только 'self' и адрес API —
// шрифты лежат в public/fonts, иконки приезжают Vue-компонентами, графики
// (echarts) бандлятся, всё остальное тоже. Любой CDN (шрифт, тайлы карты,
// аналитика) — это правка политики И решение, записанное в README, а не тихая
// строка в конфиге.
//
// Нарушение здесь тихое: ресурс просто не грузится, в интерфейсе пусто, а
// причина видна только строкой CSP в консоли.
function apiOrigins(apiBase: string): string[] {
  try {
    return [new URL(apiBase).origin]
  } catch {
    // A relative base ("/api") is same-origin: 'self' already covers it.
    return []
  }
}

function buildCsp(apiBase: string): string {
  const connect = ["'self'", ...apiOrigins(apiBase)].join(' ')
  return [
    "default-src 'self'",
    "script-src 'self'",
    // 'unsafe-inline' is required by the inline style attributes Vue and the
    // Tailwind runtime emit; scripts stay strict.
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data: blob:",
    // Предпросмотр документа базы знаний: файл ФЕТЧИТСЯ через клиент API и
    // показывается как blob:-адрес, который приложение создало само, — новый
    // origin в политику при этом не приезжает.
    "frame-src 'self' blob:",
    "font-src 'self' data:",
    `connect-src ${connect}`,
    "object-src 'none'",
    // frame-ancestors здесь НЕТ намеренно: из <meta> браузер эту директиву не
    // читает и пишет ошибку в консоль. Запрет встраивания в чужой фрейм отдаёт
    // заголовком сервер, который раздаёт интерфейс.
    "base-uri 'self'",
    "form-action 'self'",
  ].join('; ')
}

function cspPlugin(apiBase: string): Plugin {
  const policy = buildCsp(apiBase)
  return {
    name: 'polyplastic-csp-meta',
    transformIndexHtml(html) {
      const meta = `    <meta http-equiv="Content-Security-Policy" content="${policy}" />\n`
      return html.replace(/<head>\s*/i, (match) => `${match}${meta}`)
    },
  }
}

// A hackathon build must never be indexed, in any environment.
// The meta tag is defense-in-depth next to public/robots.txt and any CDN header.
function noindexPlugin(): Plugin {
  return {
    name: 'polyplastic-noindex-meta',
    transformIndexHtml(html) {
      const meta = `    <meta name="robots" content="noindex, nofollow" />\n`
      return html.replace(/<head>\s*/i, (match) => `${match}${meta}`)
    },
  }
}

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const apiBase = env.VITE_API_BASE_URL || 'http://localhost:8081/api'

  return {
    plugins: [vue(), tailwindcss(), cspPlugin(apiBase), noindexPlugin()],
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
      },
    },
    server: {
      port: 5173,
      // В разработке API — на :8081 (agency/system/api_contract.md). Прокси нужен,
      // только если VITE_API_BASE_URL задан относительным ("/api").
      proxy: {
        '/api': { target: 'http://localhost:8081' },
      },
    },
  }
})
