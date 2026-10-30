# ---------- Tahap 1: build ----------
FROM golang:1.26 AS build
WORKDIR /src

# Salin go.mod & go.sum dulu agar layer download dependency bisa di-cache
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
# modernc.org/sqlite pure Go -> bisa CGO_ENABLED=0 (binary statis)
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/todo-api ./cmd/api

# ---------- Tahap 2: runtime ----------
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /data
COPY --from=build /out/todo-api /usr/local/bin/todo-api
ENV APP_ADDR=:8080 \
    APP_DB_PATH=/data/todo.db
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/todo-api"]
