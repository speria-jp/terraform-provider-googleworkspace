#!/usr/bin/env bash

set +x
set -euo pipefail

readonly minimum_terraform_version="0.14.0"
readonly default_gcloud_token_host="https://oauth2.googleapis.com/token"
readonly default_gcloud_mtls_token_host="https://oauth2.mtls.googleapis.com/token"

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

gcloud_without_logs() {
  CLOUDSDK_CORE_DISABLE_FILE_LOGGING=1 \
    gcloud --quiet --no-log-http --verbosity=error "$@"
}

gcloud_property_value() {
  local property="$1"
  local value

  if ! value="$(gcloud_without_logs config get-value "$property" 2>/dev/null)"; then
    fail "Could not read gcloud ${property}."
  fi
  if [[ "$value" == "(unset)" ]]; then
    value=""
  fi

  printf '%s\n' "$value"
}

require_gcloud_property_unset() {
  local property="$1"
  [[ -z "$(gcloud_property_value "$property")" ]] ||
    fail "gcloud ${property} must be unset."
}

require_gcloud_property_value() {
  local property="$1"
  local expected="$2"
  [[ "$(gcloud_property_value "$property")" == "$expected" ]] ||
    fail "gcloud ${property} must be ${expected}."
}

main() {
  local repository_root example_directory bin_directory provider_binary cli_config
  local terraform_version active_account access_token

  require_command go
  require_command terraform
  require_command gcloud
  require_environment DOMAIN
  require_environment GOOGLEWORKSPACE_SERVICE_ACCOUNT_EMAIL
  require_environment GOOGLEWORKSPACE_CUSTOMER_ID
  require_environment GOOGLEWORKSPACE_IMPERSONATED_USER_EMAIL

  unset \
    TF_CLI_ARGS \
    TF_CLI_ARGS_plan \
    TF_CLI_ARGS_validate \
    TF_CLI_ARGS_version \
    TF_REATTACH_PROVIDERS \
    TF_LOG \
    TF_LOG_CORE \
    TF_LOG_PROVIDER \
    TF_LOG_PATH \
    TF_LOG_PATH_MASK \
    TF_LOG_SDK \
    TF_LOG_SDK_HELPER_RESOURCE \
    TF_LOG_SDK_HELPER_SCHEMA \
    TF_LOG_SDK_PROTO_DATA_DIR \
    TF_ACC_LOG \
    TF_ACC_LOG_PATH \
    GOOGLE_OAUTH_ACCESS_TOKEN \
    GOOGLEWORKSPACE_CREDENTIALS \
    GOOGLEWORKSPACE_CLOUD_KEYFILE_JSON \
    GOOGLE_CREDENTIALS \
    GOOGLE_APPLICATION_CREDENTIALS \
    CLOUDSDK_AUTH_ACCESS_TOKEN

  terraform_version="$(terraform version | sed -n '1s/^Terraform v//p')"
  terraform_version_is_supported "$terraform_version" ||
    fail "Terraform ${minimum_terraform_version} or newer is required."

  require_gcloud_property_unset auth/impersonate_service_account
  require_gcloud_property_unset auth/access_token_file
  require_gcloud_property_unset auth/credential_file_override
  require_gcloud_property_value auth/token_host "$default_gcloud_token_host"
  require_gcloud_property_value auth/mtls_token_host "$default_gcloud_mtls_token_host"

  active_account="$(
    gcloud_without_logs auth list \
      --filter=status:ACTIVE \
      --format='value(account)' \
      --limit=1
  )"
  [[ -n "$active_account" ]] || fail "gcloud has no active account. Run gcloud auth login."
  [[ "$active_account" != *.gserviceaccount.com ]] ||
    fail "gcloud active account must be a user account authenticated with gcloud auth login."

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

  access_token="$(gcloud_without_logs auth print-access-token "$active_account")"
  [[ -n "$access_token" ]] || fail "gcloud returned an empty access token."
  export GOOGLE_OAUTH_ACCESS_TOKEN="$access_token"
  trap 'unset GOOGLE_OAUTH_ACCESS_TOKEN; access_token=' EXIT

  terraform -chdir="$example_directory" plan \
    -input=false \
    -var="domain_name=${DOMAIN}" \
    -var="service_account_email=${GOOGLEWORKSPACE_SERVICE_ACCOUNT_EMAIL}"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
