//go:build wasip1

// The D165 segments-family reference: a skip-marker provider with no
// network at all.
//
// A real provider looks a title up in a community database. This one works
// its markers out from the facts the host already handed it, which is what
// makes it a fixture: the harness can predict every span, and the family's
// promises can be proven without a third party being up.
//
// What is worth copying from here is the SHAPE, not the arithmetic:
//
//   - you are asked once per title, while the file is analyzed, and the
//     answer is stored. Nothing you write runs when somebody presses play.
//   - answer null (or an empty list) for a title you do not know. That is
//     recorded as an answer, and you are not asked again.
//   - an empty list for a title you HAVE answered for withdraws your
//     markers, which is how you take back something you got wrong.
//   - leave endMs out for a span that runs to the end of the file.
//
// Build:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"unsafe"
)

// ---- ABI boilerplate (see examples/plugins/hello for the walkthrough) ----

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

func read(ptr, n int32) []byte {
	if n <= 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), n)
}

func packed(data []byte) int64 {
	ptr := mpAlloc(int32(len(data)))
	copy(live[ptr], data)
	return int64(ptr)<<32 | int64(len(data))
}

func unpack(v int64) []byte {
	if v == 0 {
		return nil
	}
	return read(int32(v>>32), int32(v))
}

//go:wasmimport mp config
func hostConfig() int64

func settings() map[string]string {
	out := map[string]string{}
	json.Unmarshal(unpack(hostConfig()), &out)
	return out
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
		return packed([]byte(`{"error":"could not encode the reply"}`))
	}
	return packed(out)
}

// ---- the plugin ----

// facts is the item the host is asking about. It is the same shape every
// family that gets an item receives, so a plugin implementing two of them
// parses one struct.
type facts struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Series     string `json:"series"`
	Season     int    `json:"season"`
	Episode    int    `json:"episode"`
	RuntimeMin int    `json:"runtimeMin"`
}

// span is one marker. endMs is omitted when the span runs to the end of
// the file, which is what a credits marker usually does.
type span struct {
	Type    string `json:"type"`
	StartMS int64  `json:"startMs"`
	EndMS   int64  `json:"endMs,omitempty"`
}

func number(v string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

// marks works out what to skip.
//
// An episode gets a recap and a title sequence, because that is the shape
// of episodic television and it is what a viewer of a whole season presses
// Skip on twenty times an evening. A film gets neither. Both get credits,
// derived from the runtime the host passed in: the one number here that
// comes from real data rather than convention.
func marks(f facts) []span {
	cfg := settings()
	if strings.EqualFold(strings.TrimSpace(cfg["off"]), "true") {
		return nil // the owner switched it off: answer nothing, honestly
	}
	intro := number(cfg["introSeconds"], 60)
	var out []span
	if f.Kind == "episode" {
		if f.Episode > 1 {
			// A recap only makes sense once there is something to recap.
			out = append(out, span{Type: "recap", StartMS: 0, EndMS: 20_000})
		}
		out = append(out, span{
			Type: "intro", StartMS: 20_000, EndMS: int64(20+intro) * 1000,
		})
	}
	if f.RuntimeMin > 0 {
		// Two minutes from the end, and no end of its own: the credits run
		// to the end of the file by definition.
		if credits := int64(f.RuntimeMin)*60_000 - 120_000; credits > 0 {
			out = append(out, span{Type: "credits", StartMS: credits})
		}
	}
	return out
}

//go:wasmexport mp_handle
func mpHandle(ptr, n int32) int64 {
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
	case "segments.forItem":
		var f facts
		if err := json.Unmarshal(req.Data, &f); err != nil {
			return respond(response{Error: "malformed item"})
		}
		spans := marks(f)
		if len(spans) == 0 {
			// Null is "I do not know this title". The host records that it
			// asked, and stops asking.
			return respond(response{Data: nil})
		}
		return respond(response{Data: map[string]any{"segments": spans}})
	}
	return respond(response{Error: "this plugin does not handle " + req.Op})
}

func main() {}
