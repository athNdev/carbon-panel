# Proxmox VE via the bpg/proxmox provider (Terraform Registry).
#
# The Go provisioner uploads var.cloud_init as a cloud-init snippet to a
# snippet datastore and passes its file id via
# provider_extra["user_data_file_id"]. Credentials come from
# PROXMOX_VE_* env vars, never from variables here.
locals {
  base_tags = ["carbon-cloud", "org-${var.org_id}", "node-${var.node_id}", "type-${var.node_type}"]
  vm_id     = lookup(var.provider_extra, "vm_id", "0")
}

resource "proxmox_virtual_environment_vm" "node" {
  name      = var.node_name
  node_name = lookup(var.provider_extra, "target_node", "pve")
  vm_id     = local.vm_id == "0" ? null : tonumber(local.vm_id)

  cpu {
    cores = var.vcpu
  }

  memory {
    dedicated = var.ram_mb
  }

  disk {
    datastore_id = lookup(var.provider_extra, "datastore_id", "local-lvm")
    size         = var.disk_gb
    interface    = "scsi0"
  }

  network_device {
    bridge = lookup(var.provider_extra, "bridge", "vmbr0")
  }

  agent {
    enabled = true
  }

  initialization {
    datastore_id      = lookup(var.provider_extra, "cloudinit_datastore_id", "local")
    user_data_file_id = lookup(var.provider_extra, "user_data_file_id", null)

    ip_config {
      ipv4 {
        address = lookup(var.provider_extra, "ipv4_address", "dhcp")
        gateway = lookup(var.provider_extra, "ipv4_gateway", null)
      }
    }
  }

  tags = concat(local.base_tags, [for k, v in var.tags : "${k}-${v}"])
}
