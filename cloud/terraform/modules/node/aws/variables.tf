variable "org_id" {
  description = "Tenant id, used for naming and tagging."
  type        = string
}

variable "node_id" {
  description = "Control-plane node id, used for naming and tagging."
  type        = string
}

variable "node_name" {
  description = "DNS-safe name, unique per org."
  type        = string
}

variable "node_type" {
  description = "One of nano/small/medium/large/xlarge/custom."
  type        = string

  validation {
    condition     = contains(["nano", "small", "medium", "large", "xlarge", "custom"], var.node_type)
    error_message = "node_type must be one of nano/small/medium/large/xlarge/custom."
  }
}

variable "vcpu" {
  description = "Effective requested vCPU."
  type        = number
}

variable "ram_mb" {
  description = "Effective requested RAM in MiB."
  type        = number
}

variable "disk_gb" {
  description = "Effective requested disk in GiB."
  type        = number
}

variable "region" {
  description = "Provider region."
  type        = string
}

variable "instance_type" {
  description = "Provider machine type; may be custom."
  type        = string
}

variable "ssh_public_key" {
  description = "Key injected for the node agent's bootstrap."
  type        = string
}

variable "cloud_init" {
  description = "Rendered user-data from modules/cloudinit."
  type        = string
  sensitive   = true
}

variable "control_plane_url" {
  description = "Endpoint the agent must dial."
  type        = string
}

variable "join_token" {
  description = "Single-use token (sensitive)."
  type        = string
  sensitive   = true
}

variable "tags" {
  description = "Extra tags/labels."
  type        = map(string)
  default     = {}
}

variable "image" {
  description = "Optional OS image override."
  type        = string
  default     = ""
}

variable "provider_extra" {
  description = "Provider-specific escape hatch."
  type        = map(string)
  default     = {}
}
