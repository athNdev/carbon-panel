package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/athNdev/carbon-panel/internal/worldstore"
)

func TestJSONLWorkerBindsIdentityAndHandlesUploadRead(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "server")
	journal := filepath.Join(base, "journal")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	id := worldstore.Identity{Server: "srv", Node: "node", Daemon: "daemon", Owner: "owner", Generation: 2}
	s, err := worldstore.NewLocal(id, root, journal)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id = s.Identity()

	var input bytes.Buffer
	enc := json.NewEncoder(&input)
	if err := enc.Encode(request{Op: "upload", Identity: id, Path: "region/r.0.0.mca", Data: []byte("region-data")}); err != nil {
		t.Fatal(err)
	}
	if err := enc.Encode(request{Op: "read", Identity: id, Path: "region/r.0.0.mca"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := serve(strings.NewReader(input.String()), &output, s); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(&output)
	var uploaded, read response
	if err := dec.Decode(&uploaded); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&read); err != nil {
		t.Fatal(err)
	}
	if !uploaded.OK || !read.OK || string(read.Data) != "region-data" {
		t.Fatalf("responses: %#v %#v", uploaded, read)
	}
}
