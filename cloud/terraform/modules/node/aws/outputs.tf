output "node_id" {
  description = "Echo of the input, for correlation."
  value       = var.node_id
}

output "public_ip" {
  description = "Empty when not applicable."
  value       = coalesce(aws_instance.node.public_ip, "")
}

output "private_ip" {
  description = "Empty when not applicable."
  value       = coalesce(aws_instance.node.private_ip, "")
}

output "hostname" {
  description = "FQDN or provider hostname."
  value       = coalesce(aws_instance.node.public_dns, aws_instance.node.private_dns)
}

output "instance_id" {
  description = "Provider instance id."
  value       = aws_instance.node.id
}

output "provider" {
  description = "Provider id string."
  value       = "aws"
}
