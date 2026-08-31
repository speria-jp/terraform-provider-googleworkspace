# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

provider "googleworkspace" {
  oauth_scopes = [
    "https://www.googleapis.com/auth/admin.directory.domain.readonly",
  ]
}
