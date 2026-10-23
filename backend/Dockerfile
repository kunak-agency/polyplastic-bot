# syntax=docker/dockerfile:1

# ---- builder ----
# Версия base image должна соответствовать go.mod (сейчас go 1.25.0).
FROM golang:1.25 AS builder

WORKDIR /src

# Модули отдельным слоем: правка кода не инвалидирует go mod download.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api .

# ---- runtime ----
# debian-slim, а не distroless: healthcheck в compose нужен curl. ca-certificates — MAX и модель
# по https (сертификат Минцифры подкладывается через MAX_CA_FILE); tzdata — расписание шагов.
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 65532 --no-create-home --shell /usr/sbin/nologin nonroot

WORKDIR /app
COPY --from=builder /out/api /app/api
USER nonroot:nonroot
# Снаружи контейнера: по умолчанию API слушает только loopback (config.addr).
ENV PORT=0.0.0.0:8081
EXPOSE 8081
CMD ["/app/api"]
