# Build the argguard example plugin (requires the Go toolchain only).
# -buildmode=c-shared produces a WASI *reactor* module: the guest keeps
# running after initialization so hooks stay callable (see docs/PLUGINS.md).
$env:GOOS = "wasip1"
$env:GOARCH = "wasm"
# -trimpath and -ldflags="-s -w" keep the build machine's paths out of the shipped
# module: without them the wasm carries the builder's home directory in its debug info.
go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o argguard.wasm .
Write-Output "built argguard.wasm"
