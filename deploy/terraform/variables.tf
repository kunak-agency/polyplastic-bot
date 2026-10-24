variable "host" {
  description = "IP ВМ (Timeweb Cloud), куда выкатывается стенд."
  type        = string
  default     = "72.56.247.146"
}

variable "ssh_key" {
  description = "Путь к приватному SSH-ключу root@host."
  type        = string
  default     = "~/.ssh/korsik_deploy"
}

variable "web_domain" {
  description = "Домен кабинета. Пусто — не обслуживать. Сертификат Caddy получит, когда A-запись укажет на host."
  type        = string
  default     = "lk.plexet.ru"
}

variable "api_domain" {
  description = "Отдельный домен API (весь трафик — в API, под тем же basic auth). Пусто — не обслуживать."
  type        = string
  default     = "api-mosprom.plexet.ru"
}

variable "fallback_domain" {
  description = "Запасной домен без настройки DNS (например, <ip-через-дефисы>.sslip.io): кабинет + /api. Пусто — не обслуживать."
  type        = string
  default     = ""
}

variable "cabinet_url" {
  description = "Адрес кабинета для кнопок-ссылок бота в MAX (CABINET_URL). Пусто — https://<web_domain>."
  type        = string
  default     = ""
}

variable "rag_base_url" {
  description = "Внешний адрес RAG на поде RunPod (например https://<pod>-8093.proxy.runpod.net). Ключ RAG_API_KEY берётся из локальных .env."
  type        = string
}

variable "api_enabled" {
  description = "false — поднять только Postgres и Caddy, API с ботом остановлен (первый apply перед переносом базы, см. README)."
  type        = bool
  default     = true
}

variable "acme_email" {
  description = "E-mail для Let's Encrypt (необязательно)."
  type        = string
  default     = ""
}

variable "source_dir" {
  description = "Каталог с исходниками для сборки бинарника и кабинета (чистый git worktree на нужном коммите). Пусто — сам репозиторий. Секреты .env всегда берутся из репозитория."
  type        = string
  default     = ""
}

variable "basic_auth_enabled" {
  description = "Basic auth Caddy на весь сайт. Выключать, когда в API включён вход по ролям (FEATURE_AUTH)."
  type        = bool
  default     = true
}
