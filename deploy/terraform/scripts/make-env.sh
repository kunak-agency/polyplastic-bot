#!/usr/bin/env bash
# .env для ВМ (local-exec): значения из корневого .env и backend/.env (backend/.env главнее —
# его читает API при локальном запуске), плюс переопределения под ВМ. Секреты в Terraform state
# не попадают: файл пишется сразу в $VM_DIR/.env с правами 600. Значения не печатаются.
set -euo pipefail
: "${REPO:?}" "${VM_DIR:?}" "${RAG_BASE_URL:?}" "${ORIGINS:?}" "${CABINET_URL:?}" "${POSTGRES_PASSWORD:?}"

# Что берём из локальных .env как есть. Адреса, пути и база — ниже, под ВМ.
PASS=(
  MAX_BOT_TOKEN MAX_BOT_USERNAME MAX_HR_CHAT_ID MAX_SB_CHAT_ID MAX_POLL_TIMEOUT MAX_DIGEST_USER_IDS
  INVITE_LINK_FORMAT INVITE_TTL SCHEDULER_INTERVAL SCHEDULER_PACK_GAP CONTINUE_LABEL
  RAG_API_KEY RAG_ASK_TIMEOUT RAG_HEALTH_TIMEOUT RAG_FLAGS_TIMEOUT
  KB_MAX_UPLOAD_MB KB_UPLOAD_TIMEOUT KB_OPERATOR_TOKEN MEDIA_MAX_MB
  LLM_BASE_URL LLM_API_KEY LLM_MODEL LLM_TEMPERATURE LLM_PRESENCE_PENALTY
  FEATURE_REPLIES FEATURE_AUTH FEATURE_TZ FEATURE_NUDGES_DEFAULT FEATURE_CATCHUP FEATURE_RISK_V2 FEATURE_CLARIFY
  FEATURE_CALL CALL_MAX_CONCURRENT CALL_MAX_SECONDS CALL_BARGE_IN CALL_TTS_SPEAKER CALL_TTS_ENGINE CALL_TTS_VOICE CALL_MUTE_WHILE_BOT
)

# lookup KEY — последнее непустое значение KEY: сначала корневой .env, потом backend/.env.
lookup() {
  local key=$1 val="" f line v
  for f in "$REPO/.env" "$REPO/backend/.env"; do
    [ -f "$f" ] || continue
    while IFS= read -r line || [ -n "$line" ]; do
      line=${line%$'\r'}
      line=${line#export }
      [[ $line == "$key="* ]] || continue
      v=${line#"$key="}
      # Снять парные кавычки вокруг значения.
      if [[ $v == \"*\" && ${#v} -ge 2 ]] || [[ $v == \'*\' && ${#v} -ge 2 ]]; then v=${v:1:${#v}-2}; fi
      [ -n "$v" ] && val=$v
    done < "$f"
  done
  printf '%s' "$val"
}

out="$VM_DIR/.env"
tmp="$out.tmp"
mkdir -p "$VM_DIR"
umask 077
: > "$tmp"
chmod 600 "$tmp"

# emit KEY VALUE — одинарные кавычки: compose не подставляет в них ${...} и не трогает «$».
emit() {
  case $2 in *"'"*) echo "make-env: значение $1 содержит одинарную кавычку — не поддерживается" >&2; exit 1 ;; esac
  printf "%s='%s'\n" "$1" "$2" >> "$tmp"
}

emit APP_ENV development
emit PORT 0.0.0.0:8081
emit POSTGRES_PASSWORD "$POSTGRES_PASSWORD"
emit DATABASE_URL "postgres://polyplastic:${POSTGRES_PASSWORD}@postgres:5432/polyplastic?sslmode=disable"
emit TAXONOMY_PATH /app/data/taxonomy.yaml
emit MAX_CA_FILE /app/certs/russian_trusted_root_ca.pem
emit RAG_BASE_URL "${RAG_BASE_URL%/}"
emit ORIGINS "$ORIGINS"
emit CABINET_URL "$CABINET_URL"

missing=()
for k in "${PASS[@]}"; do
  v=$(lookup "$k")
  [ -n "$v" ] && emit "$k" "$v"
done
for k in MAX_BOT_TOKEN MAX_BOT_USERNAME RAG_API_KEY; do
  [ -n "$(lookup "$k")" ] || missing+=("$k")
done

mv -f "$tmp" "$out"
chmod 600 "$out"
echo "make-env: $out собран ($(grep -c . "$out") переменных; значения не выводятся)"
if [ ${#missing[@]} -gt 0 ]; then
  echo "make-env: WARN в локальных .env не заданы: ${missing[*]}" >&2
fi
