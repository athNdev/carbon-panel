package routing

import (
	"bufio"
	"strings"
	"testing"
)

func TestRESP_FormatAndParse(t *testing.T) {
	cmd := formatRESPCommand("SET", "foo", "bar")
	expected := "*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"
	if cmd != expected {
		t.Fatalf("expected %q, got %q", expected, cmd)
	}

	reader := bufio.NewReader(strings.NewReader(expected))
	val, err := parseRESP(reader)
	if err != nil {
		t.Fatalf("failed to parse RESP: %v", err)
	}

	arr, ok := val.([]any)
	if !ok || len(arr) != 3 {
		t.Fatalf("expected array of 3 items, got %v", val)
	}
	if arr[0] != "SET" || arr[1] != "foo" || arr[2] != "bar" {
		t.Fatalf("unexpected items: %v", arr)
	}
}
