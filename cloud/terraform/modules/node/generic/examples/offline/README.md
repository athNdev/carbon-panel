# Offline plan example

Plans with no SSH connection and no cloud credentials. The only
network call is the one-time `null` provider download during `init`.

```sh
tofu -chdir=cloud/terraform/modules/node/generic/examples/offline init
tofu -chdir=cloud/terraform/modules/node/generic/examples/offline plan
```

The `terraform.tfvars` token is an obvious fake for the offline plan
only. Real tokens arrive via `TF_VAR_join_token`.
