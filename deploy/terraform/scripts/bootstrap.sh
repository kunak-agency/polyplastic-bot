#!/bin/sh
# Подготовка ВМ (remote-exec, root): docker + compose plugin, swap, ufw. Идемпотентно: повторный
# запуск ничего не ломает и не переустанавливает.
set -eu
export DEBIAN_FRONTEND=noninteractive

if ! command -v docker >/dev/null 2>&1 || ! docker compose version >/dev/null 2>&1; then
  apt-get update -q
  apt-get install -y -q docker.io docker-compose-v2
fi
systemctl enable --now docker

# Swap 1 ГБ: на 2 ГБ памяти Postgres + API + Caddy помещаются, swap — страховка от OOM.
if ! swapon --show=NAME --noheadings | grep -qx /swapfile; then
  [ -f /swapfile ] || { fallocate -l 1G /swapfile; chmod 600 /swapfile; mkswap /swapfile >/dev/null; }
  swapon /swapfile
fi
grep -q '^/swapfile ' /etc/fstab || echo '/swapfile none swap sw 0 0' >> /etc/fstab

# Firewall. 10050 (zabbix_agentd хостера) до нас был открыт — оставляем открытым.
command -v ufw >/dev/null 2>&1 || apt-get install -y -q ufw
ufw allow 22/tcp >/dev/null
ufw allow 80/tcp >/dev/null
ufw allow 443/tcp >/dev/null
ufw allow 443/udp >/dev/null
ufw allow 10050/tcp >/dev/null
ufw --force enable >/dev/null
ufw status

mkdir -p /opt/korsik /root/korsik-dumps
chmod 750 /opt/korsik
chmod 700 /root/korsik-dumps
docker --version
docker compose version
