output "node_id" {
  description = "Echo of the input, for correlation."
  value       = var.node_id
}

output "public_ip" {
  description = "Empty when not applicable."
  value       = ""
}

output "private_ip" {
  description = "Empty when not applicable."
  value       = coalesce(try(proxmox_virtual_environment_vm.node.ipv4_addresses[1][0], ""), "")
}

output "hostname" {
  description = "FQDN or provider hostname."
  value       = proxmox_virtual_environment_vm.node.name
}

output "instance_id" {
  description = "Provider instance id."
  value       = tostring(proxmox_virtual_environment_vm.node.vm_id)
}

output "provider" {
  description = "Provider id string."
  value       = "proxmox"
}
