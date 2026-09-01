# Terraform Provider Google Workspace

[![Releases](https://img.shields.io/github/release/speria-jp/terraform-provider-googleworkspace.svg)](https://github.com/speria-jp/terraform-provider-googleworkspace/releases)
[![License](https://img.shields.io/github/license/speria-jp/terraform-provider-googleworkspace.svg)](https://github.com/speria-jp/terraform-provider-googleworkspace/blob/main/LICENSE)
[![Unit tests](https://github.com/speria-jp/terraform-provider-googleworkspace/actions/workflows/test.yml/badge.svg)](https://github.com/speria-jp/terraform-provider-googleworkspace/actions/workflows/test.yml)

This Terraform provider manages users, groups, domains, and other resources in Google Workspace.
It is a community-maintained fork of the archived
[`hashicorp/terraform-provider-googleworkspace`](https://github.com/hashicorp/terraform-provider-googleworkspace)
provider and is not an official HashiCorp product.

## Maintainers

This provider is maintained by [speria-jp](https://github.com/speria-jp).

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/install) >= 0.13.x
- [Go](https://go.dev/doc/install) >= 1.27.0

## Using the provider

Declare the community provider source and a compatible version constraint:

```hcl
terraform {
  required_providers {
    googleworkspace = {
      source  = "speria-jp/googleworkspace"
      version = "~> 0.8"
    }
  }
}
```

After the first Registry release, see the
[Google Workspace Provider documentation](https://registry.terraform.io/providers/speria-jp/googleworkspace/latest/docs)
for configuration and resource documentation.

To upgrade an existing installation within its configured version constraints, run:

```sh
terraform init -upgrade
```

## Developing the provider

Clone the repository, enter it, and build the provider:

```sh
make build
```

To add a dependency, update the Go modules and commit both module files:

```sh
go get github.com/author/dependency
go mod tidy
```

Run unit tests with `make test`. Acceptance tests use a real Google Workspace organization and
must only be run when their external effects are explicitly intended.

Documentation under `docs/` is generated from `templates/` and `examples/` with:

```sh
make generate
```

See the [contribution guidelines](https://github.com/speria-jp/terraform-provider-googleworkspace/blob/main/.github/CONTRIBUTING.md)
for local provider installation and contribution details. Please report problems through
[GitHub Issues](https://github.com/speria-jp/terraform-provider-googleworkspace/issues).

## Attribution

This project retains the history and MPL-2.0 license of HashiCorp's archived provider.
Special thanks to [Chase](https://github.com/DeviaVir) for the original
`DeviaVir/terraform-provider-gsuite` provider that inspired it.
