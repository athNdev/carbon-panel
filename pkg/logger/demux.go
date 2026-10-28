// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package logger

import (
	"bufio"
	"bytes"
	"io"

	stdcopy "github.com/moby/moby/api/pkg/stdcopy"
)

// DockerLogLine is one line from a non-TTY Docker log stream.
// Stream is "stdout" or "stderr".
type DockerLogLine struct {
	Stream string
	Text   string
}

// CopyDockerLogs demultiplexes a non-TTY Docker log stream.
// stdout and stderr are separate writers. The signature has no protobuf type.
func CopyDockerLogs(stdout, stderr io.Writer, src io.Reader) (int64, error) {
	return stdcopy.StdCopy(stdout, stderr, src)
}

// ReadDockerLogLines reads a complete non-TTY Docker log stream into lines.
func ReadDockerLogLines(src io.Reader) ([]DockerLogLine, error) {
	var stdout, stderr bytes.Buffer
	_, err := CopyDockerLogs(&stdout, &stderr, src)
	if err != nil {
		return nil, err
	}
	var lines []DockerLogLine
	collect := func(stream string, buf *bytes.Buffer) error {
		scanner := bufio.NewScanner(buf)
		for scanner.Scan() {
			lines = append(lines, DockerLogLine{Stream: stream, Text: scanner.Text()})
		}
		return scanner.Err()
	}
	if err := collect("stdout", &stdout); err != nil {
		return nil, err
	}
	if err := collect("stderr", &stderr); err != nil {
		return nil, err
	}
	return lines, nil
}
