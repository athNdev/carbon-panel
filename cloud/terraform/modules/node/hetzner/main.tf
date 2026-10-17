locals {
  base_labels = {
    "carbon-cloud" = "true"
    "org_id"       = var.org_id
    "node_id"      = var.node_id
    "node_type"    = var.node_type
  }
}

resource "hcloud_ssh_key" "node" {
  name       = "${var.org_id}-${var.node_id}"
  public_key = var.ssh_public_key
  labels     = local.base_labels
}

resource "hcloud_server" "node" {
  name        = var.node_name
  server_type = var.instance_type
  image       = var.image != "" ? var.image : "ubuntu-24.04"
  location    = var.region
  ssh_keys    = [hcloud_ssh_key.node.id]
  user_data   = var.cloud_init
  labels      = merge(local.base_labels, var.tags)

  public_net {
    ipv4_enabled = true
    ipv6_enabled = lookup(var.provider_extra, "ipv6_enabled", "true") == "true"
  }
}
