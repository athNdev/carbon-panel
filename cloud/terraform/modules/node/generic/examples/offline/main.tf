terraform {
  required_version = ">= 1.4"
  required_providers {
    null = {
      source  = "hashicorp/null"
      version = ">= 3.1, < 4.0"
    }
  }
}

variable "join_token" {
  type      = string
  sensitive = true
}

module "bootstrap" {
  source            = "../../../cloudinit"
  control_plane_url = "https://cloud.example.com"
  join_token        = var.join_token
  node_name         = "offline-01"
  node_type         = "small"
  agent_install_url = "https://releases.example.com/cloudnoded"
  format            = "shellscript"
}

module "node" {
  source            = "../.."
  org_id            = "org_offline"
  node_id           = "node_offline_01"
  node_name         = "offline-01"
  node_type         = "small"
  vcpu              = 2
  ram_mb            = 4096
  disk_gb           = 40
  region            = "onprem"
  instance_type     = "custom"
  ssh_public_key    = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOFFLINEEXAMPLEKEY offline@example"
  cloud_init        = module.bootstrap.user_data
  control_plane_url = "https://cloud.example.com"
  join_token        = var.join_token
  host              = "192.0.2.10"
}
