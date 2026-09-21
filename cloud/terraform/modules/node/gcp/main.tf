locals {
  base_labels = {
    "carbon-cloud" = "true"
    "org_id"       = var.org_id
    "node_id"      = var.node_id
    "node_type"    = var.node_type
  }
  # GCP label values must be lowercase.
  labels = { for k, v in merge(local.base_labels, var.tags) : lower(k) => lower(v) }
}

resource "google_compute_instance" "node" {
  name         = var.node_name
  machine_type = var.instance_type
  zone         = lookup(var.provider_extra, "zone", "${var.region}-a")

  boot_disk {
    initialize_params {
      image = var.image
      size  = var.disk_gb
      type  = lookup(var.provider_extra, "disk_type", "pd-balanced")
    }
  }

  network_interface {
    network    = lookup(var.provider_extra, "network", "default")
    subnetwork = lookup(var.provider_extra, "subnetwork", null)
    access_config {}
  }

  metadata = {
    "ssh-keys"  = "${lookup(var.provider_extra, "ssh_user", "carbon")}:${var.ssh_public_key}"
    "user-data" = var.cloud_init
  }

  labels = local.labels
}
