terraform {
  required_version = ">= 1.4"
  required_providers {
    null = {
      source  = "hashicorp/null"
      version = ">= 3.1, < 4.0"
    }
  }
}
