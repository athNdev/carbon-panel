output "node_id" {
  description = "Echo of the input, for correlation."
  value       = var.node_id
}

output "public_ip" {
  description = "Empty when not applicable."
  value       = digitalocean_droplet.node.ipv4_address
}

output "private_ip" {
  description = "Empty when not applicable."
  value       = coalesce(digitalocean_droplet.node.ipv4_address_private, "")
}

output "hostname" {
  description = "FQDN or provider hostname."
  value       = digitalocean_droplet.node.name
}

output "instance_id" {
  description = "Provider instance id."
  value       = tostring(digitalocean_droplet.node.id)
}

output "provider" {
  description = "Provider id string."
  value       = "digitalocean"
}
