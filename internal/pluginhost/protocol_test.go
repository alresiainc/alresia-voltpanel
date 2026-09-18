package pluginhost

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEncodeDecodeRequestRoundTrip(t *testing.T) {
	req := Request{ID: "abc-123", Method: "DetectInstalled", Params: json.RawMessage(`{"foo":"bar"}`)}

	var buf bytes.Buffer
	if err := EncodeLine(&buf, req); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 || buf.Bytes()[buf.Len()-1] != '\n' {
		t.Fatalf("expected EncodeLine to terminate with a newline, got %q", buf.String())
	}

	line := bytes.TrimRight(buf.Bytes(), "\n")
	got, err := DecodeRequest(line)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != req.ID || got.Method != req.Method || string(got.Params) != string(req.Params) {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, req)
	}
}

func TestEncodeDecodeResponseRoundTrip(t *testing.T) {
	resp := Response{ID: "xyz", Result: json.RawMessage(`[{"version":"3.13.0"}]`)}

	var buf bytes.Buffer
	if err := EncodeLine(&buf, resp); err != nil {
		t.Fatal(err)
	}
	line := bytes.TrimRight(buf.Bytes(), "\n")
	got, err := DecodeResponse(line)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != resp.ID || string(got.Result) != string(resp.Result) {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, resp)
	}
	if got.Error != "" {
		t.Fatalf("expected no error field, got %q", got.Error)
	}
}

func TestEncodeDecodeResponseWithError(t *testing.T) {
	resp := Response{ID: "id-1", Error: "boom"}
	var buf bytes.Buffer
	if err := EncodeLine(&buf, resp); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeResponse(bytes.TrimRight(buf.Bytes(), "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Error != "boom" {
		t.Fatalf("expected error %q, got %q", "boom", got.Error)
	}
	if len(got.Result) != 0 {
		t.Fatalf("expected empty result alongside an error, got %q", got.Result)
	}
}

func TestEncodeDecodeHandshakeRoundTrip(t *testing.T) {
	h := Handshake{ProtocolVersion: ProtocolVersion, Kind: "runtime", Name: "python-demo", Methods: []string{"DetectInstalled"}}
	var buf bytes.Buffer
	if err := EncodeLine(&buf, h); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeHandshake(bytes.TrimRight(buf.Bytes(), "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ProtocolVersion != h.ProtocolVersion || got.Kind != h.Kind || got.Name != h.Name || len(got.Methods) != len(h.Methods) || got.Methods[0] != h.Methods[0] {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, h)
	}
}

func TestDecodeResponseRejectsGarbage(t *testing.T) {
	if _, err := DecodeResponse([]byte("not json")); err == nil {
		t.Fatal("expected an error decoding garbage input")
	}
}

func TestDecodeHandshakeRejectsGarbage(t *testing.T) {
	if _, err := DecodeHandshake([]byte("{not valid json")); err == nil {
		t.Fatal("expected an error decoding garbage input")
	}
}

func TestIsSupportedVersion(t *testing.T) {
	if !IsSupportedVersion(ProtocolVersion) {
		t.Fatalf("expected the build's own ProtocolVersion (%d) to be supported", ProtocolVersion)
	}
	if IsSupportedVersion(9999) {
		t.Fatal("expected an unknown future protocol version to be reported as unsupported")
	}
	if IsSupportedVersion(0) {
		t.Fatal("expected version 0 (a handshake that omitted protocolVersion) to be reported as unsupported")
	}
}
