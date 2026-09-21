locals {
  base_tags = {
    "carbon-cloud" = "true"
    "org_id"       = var.org_id
    "node_id"      = var.node_id
    "node_type"    = var.node_type
  }
}

resource "aws_key_pair" "node" {
  key_name_prefix = "${var.node_name}-"
  public_key      = var.ssh_public_key
  tags            = merge(local.base_tags, var.tags)
}

resource "aws_instance" "node" {
  ami                         = var.image
  instance_type               = var.instance_type
  key_name                    = aws_key_pair.node.key_name
  availability_zone           = lookup(var.provider_extra, "availability_zone", "${var.region}a")
  subnet_id                   = lookup(var.provider_extra, "subnet_id", null)
  user_data                   = var.cloud_init
  user_data_replace_on_change = true

  root_block_device {
    volume_size = var.disk_gb
    volume_type = lookup(var.provider_extra, "volume_type", "gp3")
  }

  tags = merge(local.base_tags, var.tags, { Name = var.node_name })
}
