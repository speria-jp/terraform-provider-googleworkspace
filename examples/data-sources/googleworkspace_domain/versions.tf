# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0

terraform {
  required_version = ">= 0.14.0"

  required_providers {
    googleworkspace = {
      source = "hashicorp/googleworkspace"
    }
  }
}
