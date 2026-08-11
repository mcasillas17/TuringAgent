# Proto generation

`proto/turing/v1` is the source of truth for Turing gRPC contracts.

Normal backend builds use checked-in generated code and do not require code generation. Regeneration and determinism checks require the pinned Go and Dart plugins because both backend and Flutter stubs are tracked.

Install the required toolchain:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
dart pub global activate protoc_plugin 22.5.0
export PATH="$(go env GOPATH)/bin:$HOME/.pub-cache/bin:$PATH"
```

Use protoc 34.1, then regenerate and verify the checked-in Go and Dart output:

```bash
tools/proto/generate.sh
tools/proto/check.sh
```

`generate.sh` resolves the global pub cache from `PUB_CACHE`; when unset it uses `$HOME/.pub-cache` on Unix-like systems and `%LOCALAPPDATA%\Pub\Cache` on Windows. It accepts the platform's extensionless, `.bat`, `.cmd`, or `.exe` `protoc-gen-dart` shim, verifies that the selected cache has `protoc_plugin` 22.5.0 globally activated, and passes the absolute executable path directly to protoc. A different `protoc-gen-dart` earlier on `PATH` is ignored. Missing or mismatched installations fail with the exact `PUB_CACHE=... dart pub global activate protoc_plugin 22.5.0` repair command.

Canonical generation is deliberately limited to pinned Go and Dart outputs. Unpinned platform generators are not run implicitly, so installing unrelated Swift, C#, or Java tooling cannot change `tools/proto/check.sh` results. The reserved platform directories remain placeholders until their generators and outputs are pinned:

- `gen/turing/v1/swift`
- `gen/turing/v1/csharp`
- `gen/turing/v1/kotlin`

`gen/turing/v1/dart` also remains a reserved placeholder; tracked Dart output is generated into `turing-client/turing_app/lib/generated`.
