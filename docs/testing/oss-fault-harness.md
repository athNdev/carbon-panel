# OSS Docker fault harness

The fault harness provides a reusable Engine HTTP proxy and a guarded real-daemon preflight/cleanup CLI. It is intended for explicitly disposable integration environments; it does not provision Docker daemons, install Carbon Panel, or claim application recovery behavior by itself.

## Protocol fault proxy

`internal/testharness.NewProxy(target, rules)` binds only to `127.0.0.1` and forwards Docker Engine HTTP requests to the supplied HTTP(S) endpoint. Match rules by method and versioned path prefix. The supported faults are:

| Kind | Wire behavior | What the caller can infer |
| --- | --- | --- |
| `drop-before` | Closes the client connection without forwarding the request | This attempt was not sent to the daemon through this proxy. |
| `delay-before` | Waits before forwarding; caller cancellation aborts the attempt | A timed out attempt was not forwarded if cancellation happened during the delay. |
| `drop-after` | Forwards the request, consumes the daemon response, then closes the client connection without a response | The operation may have committed; the client result is ambiguous. |
| `delay-after` | Forwards the request and waits for its response, then delays response delivery | A client timeout during delivery is ambiguous because the daemon already replied. |
| `pull-error-stream` | Preserves the HTTP response and appends a Docker JSON error frame to an image-pull progress stream | Exercises errors carried inside HTTP 200 stream bodies. |
| `disconnect-once` | Drops the first matching request; later matching requests pass through | Lets a caller exercise reconnect behavior on a subsequent request. |

For example, a client under test can use the proxy URL as its Docker host and target a disposable Engine API endpoint:

```go
proxy, err := testharness.NewProxy("http://127.0.0.1:2375", []testharness.Rule{
    {Method: http.MethodPost, PathPrefix: "/v1.56/containers/create", Kind: testharness.DropAfter},
})
if err != nil { return err }
defer proxy.Close()
// Point the Moby client under test at proxy.URL().
```

The helper does not retry requests, reinterpret daemon errors, or claim that every caller reconnects correctly. Integration tests should assert both the client-visible result and daemon-side state after ambiguous faults. Its pull fault uses a valid newline-delimited Docker JSON error record; exact behavior still depends on the Engine API/client version used by the test.

## Real-daemon qualification and ownership

The CLI uses the repository-pinned Moby client/API dependency, requires two explicit Engine endpoints, and checks each daemon's ID and `DockerRootDir`. It refuses identical daemon IDs or roots. Endpoint strings are not written to disk because they may contain credentials. Supply stable `install-id`, `node-id`, and a unique `run-id`; the manifest records those identities and every exact resource ID eligible for cleanup.

```powershell
go run ./cmd/carbon-fault-harness init `
  --endpoint-a tcp://127.0.0.1:23751 `
  --endpoint-b tcp://127.0.0.1:23752 `
  --install-id mine224-install-01 `
  --node-id mine224-node-01 `
  --manifest .\artifacts\mine224-run.json
```

`init` is preflight only and reports `resources=0`; it does not mean a case passed. The `run` command applies the same explicit endpoint qualification, writes the ownership manifest, then exits nonzero because no real-daemon cases are registered yet. In particular, it never reports success with zero cases:

```powershell
go run ./cmd/carbon-fault-harness run `
  --endpoint-a tcp://127.0.0.1:23751 `
  --endpoint-b tcp://127.0.0.1:23752 `
  --install-id mine224-install-01 `
  --node-id mine224-node-01 `
  --manifest .\artifacts\mine224-run.json
```

When a future case creates a resource, add it to the manifest only after the daemon returns its exact ID and the resource has all three labels: `io.carbon-panel.fault-harness.run`, `io.carbon-panel.fault-harness.install`, and `io.carbon-panel.fault-harness.node`. Resource kinds accepted for cleanup are container, network, and volume. Cleanup verifies the current daemon ID/root and inspects each exact resource ID to confirm all ownership labels before removing it. Containers are force-removed without deleting anonymous volumes; networks and named volumes are removed by exact ID/name only. There is no list-and-delete, prune, broad label sweep, or image/volume cascade.

```powershell
go run ./cmd/carbon-fault-harness cleanup `
  --endpoint-a tcp://127.0.0.1:23751 `
  --endpoint-b tcp://127.0.0.1:23752 `
  --manifest .\artifacts\mine224-run.json
```

The caller must retain the manifest and explicitly pass the same endpoints. Cleanup stops on the first identity, inspection, ownership, or removal error so a failed run remains inspectable. The CLI currently has no install, node-enrollment, app workflow, resource-creation, or game-join cases; those remain unverified.

## Verification boundaries

The portable proxy tests use loopback `httptest` Engine-shaped handlers. They verify pre-forward drop, post-mutation response loss, caller timeout before and after mutation, pull error stream framing, and reconnect attempt forwarding. These tests verify proxy behavior only; they are not Docker-daemon or Carbon Panel recovery acceptance.

The MINE-224 infrastructure acceptance still requires two independently identified disposable daemons with isolated data roots, a disposable Panel install/node and labeled resources, actual cases for the requested lifecycle and world/data journeys, before/after application-state checks, and evidence from exact manifest cleanup. Never point the harness at operator worlds or databases.
