variable "control_plane_url" {
  description = "Endpoint the node agent must dial (e.g. https://cloud.example.com)."
  type        = string
}

variable "join_token" {
  description = "Single-use join token for this node. Never logged or written world-readable."
  type        = string
  sensitive   = true
}

variable "node_name" {
  description = "DNS-safe node name."
  type        = string
}

variable "node_type" {
  description = "Node type (nano/small/medium/large/xlarge/custom)."
  type        = string
}

variable "agent_version" {
  description = "cloudnoded version to install."
  type        = string
  default     = "latest"
}

variable "agent_install_url" {
  description = "URL the bootstrap downloads the cloudnoded binary from."
  type        = string
}

variable "agent_sha256" {
  description = "Expected SHA-256 of the agent binary. Empty skips verification."
  type        = string
  default     = ""
}

variable "extra_runcmd" {
  description = "Extra commands appended to the bootstrap (run last, as root)."
  type        = list(string)
  default     = []
}

variable "format" {
  description = "Bootstrap format: cloud-config for cloud instances, shellscript for generic SSH hosts."
  type        = string
  default     = "cloud-config"

  validation {
    condition     = contains(["cloud-config", "shellscript"], var.format)
    error_message = "format must be cloud-config or shellscript."
  }
}
