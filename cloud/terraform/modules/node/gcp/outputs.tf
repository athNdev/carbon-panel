output "node_id" {
  description = "Echo of the input, for correlation."
  value       = var.node_id
}

output "public_ip" {
  description = "Empty when not applicable."
  value       = coalesce(try(google_compute_instance.node.network_interface[0].access_config[0].nat_ip, ""), "")
}

output "private_ip" {
  description = "Empty when not applicable."
  value       = coalesce(google_compute_instance.node.network_interface[0].network_ip, "")
}

output "hostname" {
  description = "FQDN or provider hostname."
  value       = google_compute_instance.node.name
}

output "instance_id" {
  description = "Provider instance id."
  value       = google_compute_instance.node.instance_id
}

output "provider" {
  description = "Provider id string."
  value       = "gcp"
}
