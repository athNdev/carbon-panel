output "node_id" {
  description = "Echo of the input, for correlation."
  value       = var.node_id
}

output "public_ip" {
  description = "Empty when not applicable."
  value       = hcloud_server.node.ipv4_address
}

output "private_ip" {
  description = "Empty when not applicable."
  value       = ""
}

output "hostname" {
  description = "FQDN or provider hostname."
  value       = hcloud_server.node.name
}

output "instance_id" {
  description = "Provider instance id."
  value       = tostring(hcloud_server.node.id)
}

output "provider" {
  description = "Provider id string."
  value       = "hetzner"
}
