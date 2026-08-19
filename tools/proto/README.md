# Proto generation and compatibility

`proto/turing/v1` is the source of truth for Turing gRPC contracts.

Normal backend builds use checked-in generated code and do not require code generation. Regeneration and determinism checks require the pinned Go and Dart plugins because both backend and Flutter stubs are tracked. Compatibility checks use Buf CLI 1.72.0 without changing the generation path.

Install the required toolchain:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
dart pub global activate protoc_plugin 22.5.0
export PATH="$(go env GOPATH)/bin:$HOME/.pub-cache/bin:$PATH"
```

Download the Buf 1.72.0 binary for your platform and its checksum file from the [official release](https://github.com/bufbuild/buf/releases/tag/v1.72.0), verify it, place `buf` on `PATH`, and confirm `buf --version` prints `1.72.0`.

Use protoc 34.1, then regenerate and verify the checked-in Go and Dart output:

```bash
tools/proto/generate.sh
tools/proto/check.sh
```

Check the working protobuf schema against the current remote `main`:

```bash
tools/proto/breaking.sh
```

To compare with another base branch, pass its remote-tracking ref:

```bash
tools/proto/breaking.sh origin/release-branch
```

The script validates the ref and refreshes that branch. It uses a depth-one fetch only when the checkout is already shallow; a full local repository stays full. The script fails instead of falling back to a stale local baseline when the fetch cannot complete. CI passes the pull request's base branch, so compatibility is not hardcoded to `main`.

`buf.yaml` uses Buf's `FILE` breaking category because the repository tracks generated Flutter and Go source. Additive fields, messages, enums, and RPCs pass. Removing or renumbering a live field fails even if the removed field is reserved, because generated client source would still break.

Mainline history contains no removed protobuf fields, enum values, or files, so TUR-019 adds no speculative reservations. If a future versioned API policy permits removal, reserve both the old number and name before either can be reused; that policy change must use an explicit new compatibility baseline because the current `FILE` policy intentionally rejects source deletion.

`generate.sh` resolves the global pub cache from `PUB_CACHE`; when unset it uses `$HOME/.pub-cache` on Unix-like systems and `%LOCALAPPDATA%\Pub\Cache` on Windows. It accepts the platform's extensionless, `.bat`, `.cmd`, or `.exe` `protoc-gen-dart` shim and verifies that the selected cache has `protoc_plugin` 22.5.0 globally activated. Unix-like systems pass that absolute executable directly to protoc. On Windows, the selected cache's `bin` directory is prepended to `PATH` for protoc's platform-aware lookup, which is required to launch Dart's `.bat` shim through `cmd.exe`. In both cases a different `protoc-gen-dart` elsewhere on `PATH` cannot win. Missing or mismatched installations fail with the exact `PUB_CACHE=... dart pub global activate protoc_plugin 22.5.0` repair command.

Canonical generation is deliberately limited to pinned Go and Dart outputs. Unpinned platform generators are not run implicitly, so installing unrelated Swift, C#, or Java tooling cannot change `tools/proto/check.sh` results. The reserved platform directories remain placeholders until their generators and outputs are pinned:

- `gen/turing/v1/swift`
- `gen/turing/v1/csharp`
- `gen/turing/v1/kotlin`

`gen/turing/v1/dart` also remains a reserved placeholder; tracked Dart output is generated into `turing-client/turing_app/lib/generated`.
