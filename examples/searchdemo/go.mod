// The example plugin is its OWN module on purpose: it targets wasip1, it is
// not part of the server build, and this is exactly the shape a real plugin
// author's repository has. It also keeps `go vet ./...` on the server module
// away from the unsafe pointer arithmetic the wasm ABI requires.
module theater.multipass.plugins.searchdemo

go 1.26
