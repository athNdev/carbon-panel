# Local Authelia OIDC stack

Development-only OpenID Connect provider used to exercise `auth.oidc` locally.

## First run

The TLS private key is **not** committed. Generate the self-signed certificate
once before starting the compose stack:

```sh
./gen-dev-tls.sh
docker compose -f oidc/authelia/docker-compose.yaml up
```

## Credentials

`config/users_database.yml` ships default development users and must not be used
outside a local sandbox — change the password hashes (and the matching client
secret in `configuration.yml`) for any shared environment.
