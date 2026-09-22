package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestDeliverBlocksLoopback is the security regression test for the webhook
// SSRF: the SSRF-guarded client must refuse loopback (and other reserved)
// targets rather than POSTing to them.
func TestDeliverBlocksLoopback(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res := Deliver(context.Background(), Config{URL: srv.URL, TimeoutMs: 2000}, &Payload{Event: "probe"})
	t.Logf("loopback delivery: success=%v err=%q hits=%d", res.Success, res.ErrorMessage, hits)

	if hits != 0 {
		t.Fatalf("webhook delivery reached loopback (%d hits); SSRF guard missing", hits)
	}
	if res.Success {
		t.Fatalf("expected loopback delivery to fail, got success")
	}
}

func TestDeliverBlocksMetadataAddress(t *testing.T) {
	res := Deliver(context.Background(), Config{URL: "http://169.254.169.254/latest/meta-data/", TimeoutMs: 1500}, &Payload{Event: "probe"})
	if res.Success {
		t.Fatalf("metadata address delivery must fail")
	}
	t.Logf("metadata delivery refused: %q", res.ErrorMessage)
}
