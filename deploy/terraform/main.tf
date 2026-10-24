# Стенд «Корсика» на готовой ВМ: Postgres + API/бот (Go-бинарник с Mac) + Caddy (HTTPS, basic auth,
# статика кабинета). На ВМ ничего не собирается: бинарник и кабинет собираются локально в .build/.
#
# Цепочка: bootstrap (docker, ufw) → build (go + vite) → env (.env для ВМ из локальных .env) →
# файлы (compose, Caddyfile, таксономия, CA) → upload (rsync в /opt/korsik) → up (docker compose).
# Секреты из локальных .env в state не попадают: .env собирает скрипт прямо в .build/vm.

locals {
  repo = abspath("${path.module}/../..")
  # Исходники для сборки: по умолчанию репозиторий, но можно указать чистый git worktree на нужном
  # коммите (var.source_dir), чтобы незакоммиченные правки не уехали в прод. .env берутся из repo.
  src       = var.source_dir != "" ? abspath(pathexpand(var.source_dir)) : local.repo
  build_dir = abspath("${path.module}/.build")
  vm_dir    = "${local.build_dir}/vm" # зеркало /opt/korsik на ВМ
  remote    = "/opt/korsik"
  ssh_key   = pathexpand(var.ssh_key)

  # Кабинет (статика + /api) обслуживается на домене кабинета (и на fallback_domain, если задан); api_domain — только API.
  web_domains = compact([var.web_domain, var.fallback_domain])
  origins     = join(",", [for d in compact(concat(local.web_domains, [var.call_domain])) : "https://${d}"])
  cabinet_url = var.cabinet_url != "" ? trimsuffix(var.cabinet_url, "/") : "https://${var.web_domain}"

  backend_hash = sha1(join("", [
    for f in sort(fileset("${local.src}/backend", "**/*.{go,mod,sum}")) : filesha1("${local.src}/backend/${f}")
  ]))
  frontend_files = sort(setunion(
    fileset("${local.src}/frontend", "src/**"),
    fileset("${local.src}/frontend", "public/**"),
    fileset("${local.src}/frontend", "{package.json,pnpm-lock.yaml,index.html,vite.config.ts,tsconfig*.json}"),
  ))
  frontend_hash = sha1(join("", [for f in local.frontend_files : filesha1("${local.src}/frontend/${f}")]))

  # Хэши локальных .env — только чтобы повторный apply пересобрал .env для ВМ при их правке.
  env_files_hash = sha1(join("", [
    for f in ["${local.repo}/.env", "${local.repo}/backend/.env"] : fileexists(f) ? filesha1(f) : "-"
  ]))
}

resource "random_password" "basic_auth" {
  length  = 20
  special = false
}

resource "random_password" "call_auth" {
  length  = 16
  special = false
}

resource "random_password" "postgres" {
  length  = 32
  special = false
}

# 1. ВМ: docker + compose plugin, swap 1 ГБ, ufw (22, 80, 443, 10050). Идемпотентно.
resource "null_resource" "bootstrap" {
  triggers = {
    host   = var.host
    script = filesha1("${path.module}/scripts/bootstrap.sh")
  }

  connection {
    type        = "ssh"
    host        = var.host
    user        = "root"
    private_key = file(local.ssh_key)
  }

  provisioner "remote-exec" {
    script = "${path.module}/scripts/bootstrap.sh"
  }
}

# 2. Локальная сборка: linux/amd64-бинарник API и статика кабинета (VITE_API_BASE_URL=/api).
resource "null_resource" "build" {
  triggers = {
    backend  = local.backend_hash
    frontend = local.frontend_hash
    script   = filesha1("${path.module}/scripts/build.sh")
  }

  provisioner "local-exec" {
    command = "${path.module}/scripts/build.sh"
    environment = {
      REPO   = local.src
      VM_DIR = local.vm_dir
    }
  }
}

# 3. .env для ВМ (права 600) из корневого .env и backend/.env + переопределения под ВМ.
resource "null_resource" "env" {
  triggers = {
    env_files    = local.env_files_hash
    script       = filesha1("${path.module}/scripts/make-env.sh")
    rag_base_url = var.rag_base_url
    origins      = local.origins
    cabinet_url  = local.cabinet_url
    pg           = random_password.postgres.id
  }

  provisioner "local-exec" {
    command = "${path.module}/scripts/make-env.sh"
    environment = {
      REPO              = local.repo
      VM_DIR            = local.vm_dir
      RAG_BASE_URL      = var.rag_base_url
      ORIGINS           = local.origins
      CABINET_URL       = local.cabinet_url
      POSTGRES_PASSWORD = random_password.postgres.result
    }
  }
}

# 4. Конфиги и данные, которые едут на ВМ как есть.
resource "local_file" "compose" {
  filename        = "${local.vm_dir}/compose.yml"
  content         = file("${path.module}/files/compose.yml")
  file_permission = "0644"
}

resource "local_file" "caddyfile" {
  filename = "${local.vm_dir}/caddy/Caddyfile"
  content = templatefile("${path.module}/templates/Caddyfile.tftpl", {
    web_domains        = local.web_domains
    api_domain         = var.api_domain
    acme_email         = var.acme_email
    user               = "jury"
    hash               = random_password.basic_auth.bcrypt_hash
    basic_auth_enabled = var.basic_auth_enabled
    call_domain        = var.call_domain
    call_user          = "korsik"
    call_hash          = random_password.call_auth.bcrypt_hash
  })
  file_permission      = "0644"
  directory_permission = "0755"
}

resource "local_file" "taxonomy" {
  filename             = "${local.vm_dir}/data/taxonomy.yaml"
  content              = file("${local.repo}/data/taxonomy.yaml")
  file_permission      = "0644"
  directory_permission = "0755"
}

resource "local_file" "max_ca" {
  filename             = "${local.vm_dir}/certs/russian_trusted_root_ca.pem"
  content              = file("${local.repo}/deploy/certs/russian_trusted_root_ca.pem")
  file_permission      = "0644"
  directory_permission = "0755"
}

# 5. Выкладка .build/vm → /opt/korsik (rsync --delete; тома Docker лежат вне каталога).
resource "null_resource" "upload" {
  depends_on = [null_resource.bootstrap]

  triggers = {
    build     = null_resource.build.id
    env       = null_resource.env.id
    compose   = local_file.compose.content_sha1
    caddyfile = local_file.caddyfile.content_sha1
    taxonomy  = local_file.taxonomy.content_sha1
    max_ca    = local_file.max_ca.content_sha1
    script    = filesha1("${path.module}/scripts/upload.sh")
  }

  provisioner "local-exec" {
    command = "${path.module}/scripts/upload.sh"
    environment = {
      VM_DIR  = local.vm_dir
      HOST    = var.host
      SSH_KEY = local.ssh_key
      REMOTE  = local.remote
    }
  }
}

# 6. Запуск. Бинарник и Caddyfile смонтированы каталогами: API перезапускается, Caddy перечитывает
# конфиг. api_enabled=false — API остановлен (перенос базы до первого старта бота).
resource "null_resource" "up" {
  triggers = {
    upload      = null_resource.upload.id
    api_enabled = var.api_enabled
  }

  connection {
    type        = "ssh"
    host        = var.host
    user        = "root"
    private_key = file(local.ssh_key)
  }

  provisioner "remote-exec" {
    inline = [
      "set -e",
      "cd ${local.remote}",
      "chown -R root:root . && chmod 600 .env && chmod 755 bin/api",
      var.api_enabled
      ? "docker compose up -d --remove-orphans --wait --wait-timeout 180 && docker compose restart api"
      : "docker compose up -d --remove-orphans --wait --wait-timeout 180 postgres caddy && docker compose stop api",
      "docker compose exec -T caddy caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile || docker compose restart caddy",
      "docker compose ps",
    ]
  }
}
