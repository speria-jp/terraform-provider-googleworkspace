#!/usr/bin/env bash

set -euo pipefail

readonly minimum_terraform_version="0.14.0"

fail() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

terraform_version_is_supported() {
  local version="${1#v}"
  local major minor patch

  version="${version%%-*}"
  IFS=. read -r major minor patch <<<"$version"

  [[ "$major" =~ ^[0-9]+$ ]] || return 1
  [[ "$minor" =~ ^[0-9]+$ ]] || return 1
  [[ "$patch" =~ ^[0-9]+$ ]] || return 1

  ((major > 0 || (major == 0 && minor >= 14)))
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "$1 command was not found."
}

require_environment() {
  local name="$1"
  [[ -n "${!name:-}" ]] || fail "$name is required."
}

absolute_credential_path() {
  local credential_path="$1"
  local credential_directory

  if [[ "$credential_path" == "~/"* ]]; then
    credential_path="${HOME}/${credential_path#"~/"}"
  fi

  [[ -f "$credential_path" ]] || fail "GOOGLEWORKSPACE_CREDENTIALS must point to a credential file."
  [[ -r "$credential_path" ]] || fail "GOOGLEWORKSPACE_CREDENTIALS is not readable."

  credential_directory="$(cd "$(dirname "$credential_path")" && pwd -P)"
  printf '%s/%s\n' "$credential_directory" "$(basename "$credential_path")"
}

main() {
  local repository_root example_directory bin_directory provider_binary cli_config
  local terraform_version credential_path

  require_command go
  require_command terraform
  require_environment DOMAIN
  require_environment GOOGLEWORKSPACE_CREDENTIALS
  require_environment GOOGLEWORKSPACE_CUSTOMER_ID
  require_environment GOOGLEWORKSPACE_IMPERSONATED_USER_EMAIL

  unset TF_CLI_ARGS TF_CLI_ARGS_plan TF_CLI_ARGS_validate TF_CLI_ARGS_version

  terraform_version="$(terraform version | sed -n '1s/^Terraform v//p')"
  terraform_version_is_supported "$terraform_version" ||
    fail "Terraform ${minimum_terraform_version} or newer is required."

  credential_path="$(absolute_credential_path "$GOOGLEWORKSPACE_CREDENTIALS")"
  export GOOGLEWORKSPACE_CREDENTIALS="$credential_path"
  unset GOOGLE_OAUTH_ACCESS_TOKEN

  repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
  example_directory="${repository_root}/examples/data-sources/googleworkspace_domain"
  bin_directory="${repository_root}/bin"
  provider_binary="${bin_directory}/terraform-provider-googleworkspace"
  cli_config="${bin_directory}/googleworkspace-dev.tfrc"

  mkdir -p "$bin_directory"

  printf '==> Building the local googleworkspace provider\n'
  (cd "$repository_root" && go build -o "$provider_binary" .)

  {
    printf 'provider_installation {\n'
    printf '  dev_overrides {\n'
    printf '    "hashicorp/googleworkspace" = "%s"\n' "$bin_directory"
    printf '  }\n'
    printf '}\n'
  } >"$cli_config"

  export TF_CLI_CONFIG_FILE="$cli_config"
  export TF_IN_AUTOMATION=1

  terraform -chdir="$example_directory" validate
  terraform -chdir="$example_directory" plan -input=false -var="domain_name=${DOMAIN}"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
