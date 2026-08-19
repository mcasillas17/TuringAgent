FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY gen ./gen
COPY turing-backend ./turing-backend
RUN CGO_ENABLED=0 go build -tags sqlite_fts5 -o /out/turing-agent-runtime-go ./turing-backend/agent-runtime-go/cmd/runtime

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
RUN groupadd --gid 1000 turing-agent-runtime \
    && useradd --uid 1000 --gid 1000 --no-create-home --home-dir /app --shell /usr/sbin/nologin turing-agent-runtime
COPY --from=build /out/turing-agent-runtime-go /app/turing-agent-runtime-go
USER turing-agent-runtime:turing-agent-runtime
ENTRYPOINT ["/app/turing-agent-runtime-go"]
