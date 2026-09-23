terraform {
  required_version = ">= 1.4"
  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = ">= 1.45"
    }
  }
}

provider "hcloud" {
  token = var.hcloud_token
}

variable "hcloud_token" {
  type      = string
  sensitive = true
}

variable "join_token" {
  type      = string
  sensitive = true
}

module "bootstrap" {
  source            = "../../../../cloudinit"
  control_plane_url = "https://cloud.example.com"
  join_token        = var.join_token
  node_name         = "hetzner-01"
  node_type         = "small"
  agent_install_url = "https://releases.example.com/cloudnoded"
  format            = "cloud-config"
}

module "node" {
  source            = "../.."
  org_id            = "org_offline"
  node_id           = "node_hetzner_01"
  node_name         = "hetzner-01"
  node_type         = "small"
  vcpu              = 2
  ram_mb            = 4096
  disk_gb           = 40
  region            = "fsn1"
  instance_type     = "cpx11"
  ssh_public_key    = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOFFLINEEXAMPLEKEY offline@example"
  cloud_init        = module.bootstrap.user_data
  control_plane_url = "https://cloud.example.com"
  join_token        = var.join_token
}
