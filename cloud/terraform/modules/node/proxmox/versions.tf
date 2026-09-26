terraform {
  required_version = ">= 1.4"
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = ">= 0.60, < 1.0"
    }
  }
}
