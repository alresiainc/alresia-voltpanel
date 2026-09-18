package docker

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
)

// demuxStream reads a Docker container-logs or exec-start response body and
// returns the payload bytes with stdout/stderr interleaved in original
// order, stripped of framing.
//
// When the target container/exec wasn't allocated a TTY, the Engine API
// multiplexes stdout and stderr into frames of an 8-byte header
// (1 byte stream type, 3 reserved zero bytes, 4-byte big-endian payload
// size) followed by that many payload bytes. When a TTY *was* allocated,
// there's no framing at all -- it's a raw byte stream. We detect which case
// we're in by peeking the first 8 bytes: a real frame header always starts
// with stream type 0, 1, or 2 followed by three zero bytes, which is
// vanishingly unlikely to occur at the start of arbitrary raw TTY output.
func demuxStream(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(r)
	head, err := br.Peek(8)
	if err != nil {
		// Fewer than 8 bytes total (including none at all) -- nothing to
		// demux either way, just return what's there.
		return io.ReadAll(br)
	}
	if !looksLikeFrameHeader(head) {
		return io.ReadAll(br)
	}

	var out bytes.Buffer
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(br, header); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}
		size := binary.BigEndian.Uint32(header[4:8])
		if size == 0 {
			continue
		}
		if _, err := io.CopyN(&out, br, int64(size)); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
	}
	return out.Bytes(), nil
}

func looksLikeFrameHeader(head []byte) bool {
	if len(head) < 8 {
		return false
	}
	return head[0] <= 2 && head[1] == 0 && head[2] == 0 && head[3] == 0
}
