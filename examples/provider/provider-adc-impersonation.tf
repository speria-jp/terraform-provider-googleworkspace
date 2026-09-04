# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

# Auth method: Keyless domain-wide delegation from Application Default Credentials
provider "googleworkspace" {
  customer_id             = "A01b123xz"
  service_account         = "terraform@example-project.iam.gserviceaccount.com"
  impersonated_user_email = "admin@example.com"
  oauth_scopes = [
    "https://www.googleapis.com/auth/admin.directory.user",
    "https://www.googleapis.com/auth/admin.directory.userschema",
    # include scopes as needed
  ]
}
