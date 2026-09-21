terraform {
  required_version = ">= 1.4"
  required_providers {
    hcloud = {
      source  = "hetznercloud/hcloud"
      version = ">= 1.45, < 2.0"
    }
  }
}
