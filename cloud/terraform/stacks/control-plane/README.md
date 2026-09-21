# Control-plane stack

Provider-agnostic: compute is operator-supplied hosts (via
`modules/node/generic`, no cloud credentials), the database is an
**external managed PostgreSQL** passed in as variables, and object
storage for Terraform state is an S3-compatible bucket configured via
`backend.hcl`. Planned by a human, not the Go provisioner.

## Managed database

This stack creates no database server. Use your provider's managed
PostgreSQL (RDS, Cloud SQL, Hetzner, DigitalOcean Managed Databases,
or self-hosted) and pass `db_host`, `db_port`, `db_name`, `db_user`,
`db_password` (via `TF_VAR_db_password`).

## Usage

```sh
cp terraform.tfvars.example terraform.tfvars
cp backend.hcl.example backend.hcl
# edit both, then:
tofu init -backend-config=backend.hcl
tofu plan -out=tfplan
# review the plan, then:
tofu apply tfplan
```

Validates with no credentials: `tofu validate` after `init` touches
no cloud API.
