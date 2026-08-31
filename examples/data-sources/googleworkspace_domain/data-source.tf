# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

variable "domain_name" {
  description = "Google Workspace domain to read."
  type        = string
}

data "googleworkspace_domain" "example" {
  domain_name = var.domain_name
}

output "domain_verified" {
  value = data.googleworkspace_domain.example.verified
}
