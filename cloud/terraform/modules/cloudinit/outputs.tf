output "user_data" {
  description = "Rendered bootstrap. Sensitive: embeds the join token."
  value       = var.format == "shellscript" ? local.shell_user_data : local.cloud_config_user_data
  sensitive   = true
}

output "content_type" {
  description = "MIME type of user_data."
  value       = var.format == "shellscript" ? "text/x-shellscript" : "text/cloud-config"
}
