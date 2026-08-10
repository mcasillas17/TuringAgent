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

`generate.sh` fails with an installation command if `protoc-gen-dart` is absent or the globally activated `protoc_plugin` is not exactly 22.5.0. This prevents developer and CI environments from silently accepting stale Flutter stubs or producing version-dependent churn.

Other client generators remain optional and are used when installed:

- `protoc-gen-swift` and `protoc-gen-grpc-swift` for macOS
- `grpc_csharp_plugin` for Windows
- `protoc-gen-grpc-java` for Android-compatible stubs

When optional generators are not installed, `gen/turing/v1/swift`, `gen/turing/v1/csharp`, and `gen/turing/v1/kotlin` may contain only `.gitkeep` placeholders. `gen/turing/v1/dart` remains a reserved placeholder; tracked Dart output is generated into `turing-client/turing_app/lib/generated`.
