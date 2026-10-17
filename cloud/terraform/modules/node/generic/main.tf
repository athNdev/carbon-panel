# Works with no cloud credentials: takes an existing SSH-reachable host
# and only runs the bootstrap. Provisioners run on apply, so plan is
# fully offline (only the null provider download needs network, once).
resource "null_resource" "bootstrap" {
  triggers = {
    node_id    = var.node_id
    node_name  = var.node_name
    cloud_init = sha256(var.cloud_init)
  }

  connection {
    type        = "ssh"
    host        = var.host
    user        = var.ssh_user
    port        = var.ssh_port
    private_key = var.ssh_private_key != "" ? var.ssh_private_key : null
  }

  provisioner "file" {
    content     = var.cloud_init
    destination = "/tmp/carbon-cloud-bootstrap.sh"
  }

  provisioner "remote-exec" {
    inline = [
      "chmod +x /tmp/carbon-cloud-bootstrap.sh",
      "sudo -E /tmp/carbon-cloud-bootstrap.sh",
    ]
  }
}
