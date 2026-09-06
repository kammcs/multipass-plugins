// The D165 hello plugin: the smallest complete Multipass plugin, and the
// reference for the Go authoring path.
//
// Build (no toolchain beyond Go itself):
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
//	multipass plugins pack . -out myplugin.mpp
//
// Unsigned: that is what developer mode is for. Registry packages are
// signed on acceptance, so you never need a key to write or test one.
//
// The ABI is two exported functions. mp_alloc lets the host put bytes into
// this module's memory; mp_handle takes a JSON request and returns a packed
// (pointer, length) pair naming the JSON reply. Real plugins will use the
// SDK wrapper rather than writing this by hand.
//go:build wasip1

package main

import (
	"encoding/json"
	"unsafe"
)

// live keeps every buffer we hand out reachable, so Go's collector cannot
// free memory the host is still reading.
var live = map[int32][]byte{}

//go:wasmexport mp_alloc
func mpAlloc(size int32) int32 {
	if size <= 0 {
		size = 1
	}
	buf := make([]byte, size)
	ptr := int32(uintptr(unsafe.Pointer(&buf[0])))
	live[ptr] = buf
	return ptr
}

// read views the host's bytes in place. The uintptr-to-Pointer conversion
// is exactly what the wasm ABI is: the host hands over an offset into this
// module's linear memory, which IS the Go heap under GOARCH=wasm. go vet
// flags the shape by default, which is why this example is its own module.
func read(ptr, n int32) []byte {
	if n <= 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), n) //nolint:govet
}

// reply copies data into a fresh buffer and packs its address and length
// into the single i64 the host reads.
func reply(data []byte) int64 {
	ptr := mpAlloc(int32(len(data)))
	copy(live[ptr], data)
	return int64(ptr)<<32 | int64(len(data))
}

type request struct {
	Op   string          `json:"op"`
	Data json.RawMessage `json:"data,omitempty"`
}

type response struct {
	Error string `json:"error,omitempty"`
	Data  any    `json:"data,omitempty"`
}

func respond(r response) int64 {
	out, err := json.Marshal(r)
	if err != nil {
		return reply([]byte(`{"error":"could not encode the reply"}`))
	}
	return reply(out)
}

//go:wasmexport mp_handle
func mpHandle(ptr, n int32) int64 {
	// Release the previous call's buffers. mp_alloc is driven by the HOST:
	// once for this call's input, and again for every host-function reply
	// (config, http, storage, lookup). Nothing else ever frees them, so
	// without this the guest's memory only grows, and a long run over a real
	// library dies on "out of memory". By the time a new call arrives the
	// host has finished reading the last reply, so everything except the
	// input just written is done with.
	for k := range live {
		if k != ptr {
			delete(live, k)
		}
	}
	var req request
	if err := json.Unmarshal(read(ptr, n), &req); err != nil {
		return respond(response{Error: "malformed request"})
	}
	switch req.Op {
	case "ping":
		return respond(response{Data: map[string]any{"hello": "world", "op": req.Op}})
	case "echo":
		return respond(response{Data: json.RawMessage(req.Data)})
	}
	return respond(response{Error: "this plugin does not handle " + req.Op})
}

func main() {}
