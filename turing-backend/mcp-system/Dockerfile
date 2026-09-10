FROM golang:1.27-alpine AS builder
WORKDIR /app
COPY . .
# No `go mod download` step, and no separate go.mod/go.sum COPY to cache one:
# this module's ./cmd/server imports only the standard library (see
# TestTheShippedBinaryImportsOnlyTheStandardLibrary), so its image build needs
# nothing from the module proxy at all. Its only requirement is test-only — the
# pinned MCP SDK the conformance fixtures run against — and CI downloads that
# for the test job instead.
#
# GOPROXY=off enforces that here; TestTheShippedBinaryBuildsWithNoModuleProxy is
# what proves it, by running this same build against an empty module cache. That
# guard exists because the invariant stopped being free when the SDK entered
# go.mod: it now rests on module-graph pruning rather than on there being no
# requirements at all.
#
# This is specific to mcp-system, not a rule the sibling images break: the
# orchestrator, agent-runtime and mcp-files binaries genuinely link third-party
# modules, so their Dockerfiles keep the cached `go mod download` layer.
RUN GOPROXY=off CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

FROM alpine:3.24
RUN addgroup -g 1000 -S mcp-system \
    && adduser -u 1000 -S -G mcp-system mcp-system
WORKDIR /app
COPY --from=builder /app/server ./server
USER mcp-system:mcp-system
EXPOSE 7100
CMD ["./server"]
