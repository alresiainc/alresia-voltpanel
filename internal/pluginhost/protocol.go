// Package pluginhost implements VoltPanel's out-of-process extension
// protocol (§7/§21 of the implementation plan). An extension is a
// *separate executable* -- never a Go plugin (.so/.dll) and never WASM,
// both of which are explicitly rejected elsewhere in the plan (§19's risk
// table, §20's "not to build yet" list) in favor of the portability an
// out-of-process boundary gives for free. The daemon launches the
// extension's executable as a subprocess and speaks a small,
// newline-delimited-JSON protocol over its stdin/stdout:
//
//  1. On startup, the extension writes exactly one JSON line to stdout
//     describing itself (a Handshake): protocol version, provider kind
//     ("runtime", "database", ...), its own name (e.g. "python"), and the
//     methods it implements.
//  2. The daemon then sends one JSON Request per line to the subprocess's
//     stdin and reads one JSON Response per line back from stdout, in
//     strict request/response lockstep -- no pipelining, no out-of-order
//     replies. This keeps both sides trivial to implement (a contractor
//     writing an extension in any language just needs readline/writeline
//     on stdio) at the cost of one in-flight request at a time, which is
//     the right trade for "call an installer and report status" (§20).
//
// Versioning (the explicit migration concern in §17 Phase 11 and §21):
// ProtocolVersion is this build's own protocol version. SupportedVersions
// lists every version this build understands accepting from an extension's
// handshake. host.go's LoadExtension refuses to load an extension whose
// handshake declares a version not in that list, with a clear error,
// rather than guessing at wire-format compatibility -- so a future
// protocol change can bump ProtocolVersion, add the old version to
// SupportedVersions for a deprecation window (mirroring §10's API-alias
// strategy), and never silently misinterpret an old extension's bytes.
package pluginhost

import (
	"encoding/json"
	"fmt"
	"io"
)

// ProtocolVersion is the protocol version this VoltPanel build speaks and
// advertises no expectations beyond. Extensions built against this version
// should declare exactly this in their handshake.
const ProtocolVersion = 1

// SupportedVersions lists every protocolVersion value this build's
// LoadExtension will accept from an extension's handshake. Only version 1
// exists today; a future version bump should append here (and keep the
// old entry for a deprecation window) rather than replace it, so an
// already-installed extension doesn't break on the next VoltPanel update
// without warning.
var SupportedVersions = []int{1}

// IsSupportedVersion reports whether v is a protocol version this build
// understands.
func IsSupportedVersion(v int) bool {
	for _, sv := range SupportedVersions {
		if sv == v {
			return true
		}
	}
	return false
}

// Handshake is the single JSON line an extension must write to stdout
// before the daemon sends it any request -- see the package doc.
type Handshake struct {
	ProtocolVersion int      `json:"protocolVersion"`
	Kind            string   `json:"kind"`    // provider category: "runtime", "database", ...
	Name            string   `json:"name"`    // the specific provider's Kind(), e.g. "python"
	Methods         []string `json:"methods"` // method names the extension implements
}

// Request is one JSON line the daemon writes to an extension's stdin.
// Params is left as raw JSON so this package never needs per-method
// request shapes -- callers (host.go's ExternalProvider methods) marshal
// their own params and unmarshal the matching Response.Result.
type Request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is one JSON line an extension writes back to stdout, matched
// to a Request by ID. Exactly one of Result/Error is meaningful: a
// non-empty Error means the call failed and Result should be ignored.
type Response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// EncodeLine marshals v to JSON and writes it to w followed by a single
// newline -- the wire framing this whole protocol relies on (one JSON
// value per line, nothing else on that line).
func EncodeLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("pluginhost: encode line: %w", err)
	}
	b = append(b, '\n')
	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("pluginhost: write line: %w", err)
	}
	return nil
}

// DecodeHandshake parses one line of input as a Handshake.
func DecodeHandshake(line []byte) (Handshake, error) {
	var h Handshake
	if err := json.Unmarshal(line, &h); err != nil {
		return Handshake{}, fmt.Errorf("pluginhost: decode handshake: %w", err)
	}
	return h, nil
}

// DecodeRequest parses one line of input as a Request. Exported mainly so
// a non-Go extension implementation's own tests (or a Go-based one, like
// the dogfood sample's fallback) can validate they're producing/consuming
// the same shape this package does.
func DecodeRequest(line []byte) (Request, error) {
	var r Request
	if err := json.Unmarshal(line, &r); err != nil {
		return Request{}, fmt.Errorf("pluginhost: decode request: %w", err)
	}
	return r, nil
}

// DecodeResponse parses one line of input as a Response.
func DecodeResponse(line []byte) (Response, error) {
	var r Response
	if err := json.Unmarshal(line, &r); err != nil {
		return Response{}, fmt.Errorf("pluginhost: decode response: %w", err)
	}
	return r, nil
}
