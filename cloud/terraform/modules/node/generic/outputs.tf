output "node_id" {
  description = "Echo of the input, for correlation."
  value       = var.node_id
}

output "public_ip" {
  description = "Empty when not applicable."
  value       = var.host
}

output "private_ip" {
  description = "Empty when not applicable."
  value       = ""
}

output "hostname" {
  description = "FQDN or provider hostname."
  value       = var.node_name
}

output "instance_id" {
  description = "Provider instance id."
  value       = "generic-${var.node_id}"
}

output "provider" {
  description = "Provider id string."
  value       = "generic"
}
