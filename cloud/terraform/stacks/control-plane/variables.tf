# Everything is a variable: no vendor is baked in. Planned by a human.

variable "environment" {
  description = "Deployment name, e.g. prod or staging."
  type        = string
}

variable "region" {
  description = "Informational region label for tagging."
  type        = string
  default     = "onprem"
}

variable "control_plane_url" {
  description = "Public URL tenants and nodes dial (e.g. https://cloud.example.com)."
  type        = string
}

variable "control_plane_version" {
  description = "cloudcontrold version to install."
  type        = string
  default     = "latest"
}

variable "control_plane_install_url" {
  description = "URL the bootstrap downloads the cloudcontrold binary from."
  type        = string
}

variable "control_plane_sha256" {
  description = "Expected SHA-256 of the control-plane binary. Empty skips verification."
  type        = string
  default     = ""
}

variable "control_plane_hosts" {
  description = "BYO hosts the control plane runs on. Key material stays sensitive."
  type = list(object({
    host            = string
    ssh_user        = optional(string, "root")
    ssh_port        = optional(number, 22)
    ssh_private_key = optional(string, "")
  }))
  sensitive = true
}

variable "db_host" {
  description = "Managed PostgreSQL host (RDS, Cloud SQL, or external). The stack never creates a database server: use your provider's managed Postgres and pass the connection here."
  type        = string
}

variable "db_port" {
  description = "Managed PostgreSQL port."
  type        = number
  default     = 5432
}

variable "db_name" {
  description = "Database name."
  type        = string
  default     = "carbon_cloud"
}

variable "db_user" {
  description = "Database user."
  type        = string
}

variable "db_password" {
  description = "Database password. Feed via TF_VAR_db_password."
  type        = string
  sensitive   = true
}

variable "state_bucket" {
  description = "S3-compatible bucket holding per-node Terraform state for the provisioner."
  type        = string
}

variable "state_bucket_region" {
  description = "Region of the state bucket."
  type        = string
  default     = "auto"
}

variable "allowed_ssh_cidr" {
  description = "CIDR allowed to reach control-plane hosts over SSH (operator documentation; enforced outside this stack)."
  type        = string
  default     = ""
}

variable "tags" {
  description = "Extra tags/labels."
  type        = map(string)
  default     = {}
}
