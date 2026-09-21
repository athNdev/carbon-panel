locals {
  base_tags = [
    "carbon-cloud",
    "org:${var.org_id}",
    "node:${var.node_id}",
    "type:${var.node_type}",
  ]
  extra_tags = [for k, v in var.tags : "${k}:${v}"]
}

resource "digitalocean_ssh_key" "node" {
  name       = "${var.org_id}-${var.node_id}"
  public_key = var.ssh_public_key
}

resource "digitalocean_droplet" "node" {
  name      = var.node_name
  image     = var.image != "" ? var.image : "ubuntu-24-04-x64"
  region    = var.region
  size      = var.instance_type
  ssh_keys  = [digitalocean_ssh_key.node.fingerprint]
  user_data = var.cloud_init
  tags      = concat(local.base_tags, local.extra_tags)
}
