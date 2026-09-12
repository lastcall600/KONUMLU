# backend/Dockerfile
# Build API or worker without choosing a cloud vendor or orchestrator.
# Usage: docker build -f backend/Dockerfile --build-arg COMMAND=server backend
#        docker build -f backend/Dockerfile --build-arg COMMAND=worker backend

FROM golang:1.26.8-bookworm AS build
WORKDIR /src
ARG COMMAND=server
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN test "$COMMAND" = "server" || test "$COMMAND" = "worker"
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/konumlu ./cmd/${COMMAND}

FROM alpine:3.21
RUN adduser -D -H -u 65532 konumlu
USER konumlu
WORKDIR /home/konumlu
COPY --from=build /out/konumlu /usr/local/bin/konumlu
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/konumlu"]
