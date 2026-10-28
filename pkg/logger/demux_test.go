// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package logger

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func dockerFrame(stream byte, payload string) []byte {
	frame := make([]byte, 8+len(payload))
	frame[0] = stream
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload)))
	copy(frame[8:], payload)
	return frame
}

func TestReadDockerLogLines(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(dockerFrame(1, "hello\n"))
	buf.Write(dockerFrame(2, "warn\n"))
	lines, err := ReadDockerLogLines(&buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines: %#v", lines)
	}
	if lines[0].Stream != "stdout" || lines[0].Text != "hello" {
		t.Fatalf("stdout: %#v", lines[0])
	}
	if lines[1].Stream != "stderr" || lines[1].Text != "warn" {
		t.Fatalf("stderr: %#v", lines[1])
	}
}
