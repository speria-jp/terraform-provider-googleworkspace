# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

variable "service_account_email" {
  description = "Google Cloud service account used for domain-wide delegation."
  type        = string
}

provider "googleworkspace" {
  service_account = var.service_account_email

  oauth_scopes = [
    "https://www.googleapis.com/auth/admin.directory.domain.readonly",
  ]
}
