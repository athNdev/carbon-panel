# Server lifecycle operations

The panel records a journal row before it creates, starts, stops, restarts,
deletes, or migrates a server container. The row holds a per-server lock until
the mutation finishes or startup has observed the container.

A repeat of the same client `request_id` for the same server and action does
not run that mutation twice. A new attempt after a failure uses a new
`request_id`.

## When a server stays locked

If the panel restarts while a mutation is in progress, startup reads the
container from Docker and releases the lock when that read succeeds. If the
daemon cannot be reached, the lock stays and the panel retries the read until
the daemon answers. Nothing is deleted while the outcome is unknown.

## List and release a lock

Both commands open the panel database and exit. They do not start the API and
they do not change containers.

```sh
carbon-panel -config config.yaml -operations list
carbon-panel -config config.yaml -operations resolve -operation-id <id> -operation-note "container was already stopped"
```

`resolve` is the operator escape hatch when the daemon will not return and the
container has been checked by hand. It records the note, marks the row
recovered, and clears the lock. It does not stop, remove, or prune containers.

Finished rows are deleted after 30 days. A row that still holds a lock is kept.
