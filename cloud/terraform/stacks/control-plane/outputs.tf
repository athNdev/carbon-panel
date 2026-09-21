output "control_plane_url" {
  description = "Public URL tenants and nodes dial."
  value       = var.control_plane_url
}

output "control_plane_hosts" {
  description = "Hosts the control plane was installed on."
  value       = [for h in var.control_plane_hosts : h.host]
  sensitive   = true
}

output "database_dsn" {
  description = "PostgreSQL DSN for cloudcontrold. Sensitive."
  value       = local.database_url
  sensitive   = true
}

output "state_bucket" {
  description = "S3-compatible bucket holding provisioner state."
  value       = var.state_bucket
}
