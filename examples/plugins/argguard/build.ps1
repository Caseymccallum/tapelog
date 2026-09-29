# Build the argguard example plugin (requires the Go toolchain only).
# -buildmode=c-shared produces a WASI *reactor* module: the guest keeps
# running after initialization so hooks stay callable (see docs/PLUGINS.md).
$env:GOOS = "wasip1"
$env:GOARCH = "wasm"
go build -buildmode=c-shared -o argguard.wasm .
Write-Output "built argguard.wasm"
