// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package googleworkspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"

	"google.golang.org/api/iamcredentials/v1"
	"google.golang.org/api/option"
)

func TestSelectAuthenticationRoute(t *testing.T) {
	tests := []struct {
		name                 string
		source               authenticationSource
		adcType              googleoauth.CredentialsType
		serviceAccount       string
		subject              string
		allowDirectPrincipal bool
		want                 authenticationRoute
	}{
		{name: "access token impersonation", source: authenticationSourceAccessToken, serviceAccount: "service-account@example.com", subject: "admin@example.com", want: authenticationRouteAccessTokenImpersonation},
		{name: "access token missing service account", source: authenticationSourceAccessToken, subject: "admin@example.com", want: authenticationRouteInvalidImpersonation},
		{name: "access token direct Gmail principal", source: authenticationSourceAccessToken, subject: "user@example.com", allowDirectPrincipal: true, want: authenticationRouteAccessToken},
		{name: "access token Gmail impersonation", source: authenticationSourceAccessToken, serviceAccount: "service-account@example.com", subject: "user@example.com", allowDirectPrincipal: true, want: authenticationRouteAccessTokenImpersonation},
		{name: "access token direct", source: authenticationSourceAccessToken, want: authenticationRouteAccessToken},
		{name: "configured credentials", source: authenticationSourceCredentials, serviceAccount: "ignored@example.com", subject: "admin@example.com", want: authenticationRouteCredentials},
		{name: "service account ADC direct DWD", source: authenticationSourceADC, adcType: googleoauth.ServiceAccount, serviceAccount: "ignored@example.com", subject: "admin@example.com", want: authenticationRouteADCServiceAccountKey},
		{name: "authorized user ADC impersonation", source: authenticationSourceADC, adcType: googleoauth.AuthorizedUser, serviceAccount: "service-account@example.com", subject: "admin@example.com", want: authenticationRouteADCImpersonation},
		{name: "external account ADC impersonation", source: authenticationSourceADC, adcType: googleoauth.ExternalAccount, serviceAccount: "service-account@example.com", subject: "admin@example.com", want: authenticationRouteADCImpersonation},
		{name: "authorized user ADC missing service account", source: authenticationSourceADC, adcType: googleoauth.AuthorizedUser, subject: "admin@example.com", want: authenticationRouteInvalidImpersonation},
		{name: "external account ADC missing service account", source: authenticationSourceADC, adcType: googleoauth.ExternalAccount, subject: "admin@example.com", want: authenticationRouteInvalidImpersonation},
		{name: "external account authorized user ADC missing service account", source: authenticationSourceADC, adcType: googleoauth.ExternalAccountAuthorizedUser, subject: "admin@example.com", want: authenticationRouteInvalidImpersonation},
		{name: "impersonated service account ADC missing service account", source: authenticationSourceADC, adcType: googleoauth.ImpersonatedServiceAccount, subject: "admin@example.com", want: authenticationRouteInvalidImpersonation},
		{name: "unknown ADC missing service account", source: authenticationSourceADC, adcType: googleoauth.CredentialsType("future_credential"), subject: "admin@example.com", want: authenticationRouteInvalidImpersonation},
		{name: "JSON-less ADC missing service account", source: authenticationSourceADC, subject: "admin@example.com", want: authenticationRouteInvalidImpersonation},
		{name: "authorized user ADC direct Gmail principal", source: authenticationSourceADC, adcType: googleoauth.AuthorizedUser, subject: "user@example.com", allowDirectPrincipal: true, want: authenticationRouteADC},
		{name: "authorized user ADC Gmail impersonation", source: authenticationSourceADC, adcType: googleoauth.AuthorizedUser, serviceAccount: "service-account@example.com", subject: "user@example.com", allowDirectPrincipal: true, want: authenticationRouteADCImpersonation},
		{name: "ADC direct", source: authenticationSourceADC, adcType: googleoauth.ExternalAccount, serviceAccount: "unused@example.com", want: authenticationRouteADC},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectAuthenticationRoute(tt.source, tt.adcType, tt.serviceAccount, tt.subject, tt.allowDirectPrincipal)
			if got != tt.want {
				t.Fatalf("selectAuthenticationRoute() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestADCCredentialsType(t *testing.T) {
	tests := []struct {
		name    string
		creds   *googleoauth.Credentials
		want    googleoauth.CredentialsType
		wantErr bool
	}{
		{name: "service account", creds: &googleoauth.Credentials{JSON: []byte(`{"type":"service_account"}`)}, want: googleoauth.ServiceAccount},
		{name: "authorized user", creds: &googleoauth.Credentials{JSON: []byte(`{"type":"authorized_user"}`)}, want: googleoauth.AuthorizedUser},
		{name: "unknown", creds: &googleoauth.Credentials{JSON: []byte(`{"type":"future_credential"}`)}, want: googleoauth.CredentialsType("future_credential")},
		{name: "JSON-less", creds: &googleoauth.Credentials{}},
		{name: "invalid JSON", creds: &googleoauth.Credentials{JSON: []byte(`{`)}, wantErr: true},
		{name: "nil", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := adcCredentialsType(tt.creds)
			if (err != nil) != tt.wantErr {
				t.Fatalf("adcCredentialsType() error = %v, wantErr %t", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("adcCredentialsType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigLoadAndValidate_ADCServiceAccountKeyUsesDirectDWD(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", testFakeCredentialsPath)

	config := &apiClient{
		ServiceAccount:        "ignored@example.com",
		ImpersonatedUserEmail: "admin@example.com",
	}
	diags := config.loadAndValidate(context.Background())
	if err := checkDiags(diags); err != nil {
		t.Fatal(err)
	}
	if config.client == nil {
		t.Fatal("expected authenticated client to be configured")
	}
}

func TestConfigLoadAndValidate_AuthorizedUserADC(t *testing.T) {
	adcPath := writeAuthorizedUserADC(t, "quota-project")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", adcPath)
	t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", "")

	t.Run("impersonation", func(t *testing.T) {
		config := &apiClient{
			ServiceAccount:        "service-account@example.com",
			ImpersonatedUserEmail: "admin@example.com",
		}
		if err := checkDiags(config.loadAndValidate(context.Background())); err != nil {
			t.Fatal(err)
		}
		if config.client == nil {
			t.Fatal("expected authenticated client to be configured")
		}
	})

	t.Run("missing service account", func(t *testing.T) {
		config := &apiClient{ImpersonatedUserEmail: "admin@example.com"}
		err := checkDiags(config.loadAndValidate(context.Background()))
		if err == nil || !strings.Contains(err.Error(), "Set service_account") {
			t.Fatalf("expected actionable service_account error, got %v", err)
		}
		if config.client != nil {
			t.Fatal("expected validation to fail before configuring an Admin SDK client")
		}
	})

	t.Run("direct principal", func(t *testing.T) {
		config := &apiClient{}
		if err := checkDiags(config.loadAndValidate(context.Background())); err != nil {
			t.Fatal(err)
		}
		if config.client == nil {
			t.Fatal("expected authenticated client to be configured")
		}
	})
}

func TestADCImpersonationTokenSource(t *testing.T) {
	tests := []struct {
		name             string
		environmentQuota string
		wantQuota        string
	}{
		{name: "quota project from ADC JSON", wantQuota: "json-quota-project"},
		{name: "environment quota project takes precedence", environmentQuota: "environment-quota-project", wantQuota: "environment-quota-project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GOOGLE_CLOUD_QUOTA_PROJECT", tt.environmentQuota)

			const (
				sourceToken       = "source-access-token"
				targetPrincipal   = "service-account@example.com"
				subject           = "admin@example.com"
				finalToken        = "domain-wide-delegation-token"
				expectedSignedJWT = "header.payload.signature"
			)
			var refreshRequests, signJWTRequests, exchangeRequests int
			base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Path, ":signJwt") {
					signJWTRequests++
					if got := req.Header.Get("Authorization"); got != "Bearer "+sourceToken {
						t.Errorf("signJwt Authorization = %q, want Bearer source token", got)
					}
					if got := req.Header.Get("X-Goog-User-Project"); got != tt.wantQuota {
						t.Errorf("signJwt X-Goog-User-Project = %q, want %q", got, tt.wantQuota)
					}
					if !strings.Contains(req.URL.Path, "serviceAccounts/"+targetPrincipal+":signJwt") {
						t.Errorf("signJwt path = %q, want target service account %q", req.URL.Path, targetPrincipal)
					}

					var signRequest struct {
						Payload string `json:"payload"`
					}
					if err := json.NewDecoder(req.Body).Decode(&signRequest); err != nil {
						t.Fatalf("decode signJwt request: %v", err)
					}
					var claims struct {
						Issuer  string `json:"iss"`
						Subject string `json:"sub"`
						Scope   string `json:"scope"`
					}
					if err := json.Unmarshal([]byte(signRequest.Payload), &claims); err != nil {
						t.Fatalf("decode JWT claims: %v", err)
					}
					if claims.Issuer != targetPrincipal || claims.Subject != subject || claims.Scope != "scope-one scope-two" {
						t.Errorf("JWT claims = %+v", claims)
					}
					return jsonResponse(http.StatusOK, map[string]string{"keyId": "key-id", "signedJwt": expectedSignedJWT}), nil
				}

				if err := req.ParseForm(); err != nil {
					t.Fatalf("parse token request: %v", err)
				}
				switch req.Form.Get("grant_type") {
				case "refresh_token":
					refreshRequests++
					return jsonResponse(http.StatusOK, map[string]interface{}{"access_token": sourceToken, "token_type": "Bearer", "expires_in": 3600}), nil
				case "assertion":
					exchangeRequests++
					if got := req.Form.Get("assertion"); got != expectedSignedJWT {
						t.Errorf("token assertion = %q, want %q", got, expectedSignedJWT)
					}
					return jsonResponse(http.StatusOK, map[string]interface{}{"access_token": finalToken, "token_type": "Bearer", "expires_in": 3600}), nil
				default:
					return nil, fmt.Errorf("unexpected request: %s %s", req.Method, req.URL)
				}
			})

			credentialContext := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: base})
			source, err := googleoauth.CredentialsFromJSONWithParams(credentialContext, authorizedUserADCJSON("json-quota-project"), googleoauth.CredentialsParams{Scopes: []string{cloudPlatformScope}})
			if err != nil {
				t.Fatal(err)
			}
			sourceClient, err := adcImpersonationHTTPClient(context.Background(), source, base)
			if err != nil {
				t.Fatal(err)
			}
			config := &apiClient{
				ClientScopes:          []string{"scope-one", "scope-two"},
				ServiceAccount:        targetPrincipal,
				ImpersonatedUserEmail: subject,
			}
			tokenSource, err := config.impersonatedTokenSource(context.Background(), option.WithHTTPClient(sourceClient))
			if err != nil {
				t.Fatal(err)
			}
			token, err := tokenSource.Token()
			if err != nil {
				t.Fatal(err)
			}
			if token.AccessToken != finalToken {
				t.Fatalf("token = %q, want %q", token.AccessToken, finalToken)
			}
			if refreshRequests != 1 || signJWTRequests != 1 || exchangeRequests != 1 {
				t.Fatalf("request counts: refresh=%d signJwt=%d exchange=%d", refreshRequests, signJWTRequests, exchangeRequests)
			}
		})
	}
}

func TestGmailAPIClientAuthentication(t *testing.T) {
	t.Run("inherits access token and service account", func(t *testing.T) {
		parent := &apiClient{
			AccessToken:    "access-token",
			Credentials:    "credentials",
			ServiceAccount: "service-account@example.com",
		}
		child := parent.gmailAPIClient("user@example.com")
		if child.AccessToken != parent.AccessToken || child.Credentials != parent.Credentials || child.ServiceAccount != parent.ServiceAccount {
			t.Fatalf("Gmail client did not inherit authentication configuration: %+v", child)
		}
		if !child.allowDirectPrincipal {
			t.Fatal("expected internal Gmail subject to allow direct-principal authentication")
		}
	})

	t.Run("direct access token does not fall back to ADC", func(t *testing.T) {
		t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "missing.json"))
		child := (&apiClient{AccessToken: "access-token"}).gmailAPIClient("user@example.com")
		if err := checkDiags(child.loadAndValidate(context.Background())); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("direct authorized user ADC", func(t *testing.T) {
		t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", writeAuthorizedUserADC(t, ""))
		child := (&apiClient{}).gmailAPIClient("user@example.com")
		if err := checkDiags(child.loadAndValidate(context.Background())); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("provider impersonation still requires service account", func(t *testing.T) {
		parent := &apiClient{AccessToken: "access-token", ImpersonatedUserEmail: "admin@example.com"}
		child := parent.gmailAPIClient("user@example.com")
		if child.allowDirectPrincipal {
			t.Fatal("provider-level impersonation must not allow direct-principal authentication")
		}
		err := checkDiags(child.loadAndValidate(context.Background()))
		if err == nil || !strings.Contains(err.Error(), "service_account is required") {
			t.Fatalf("expected service_account error, got %v", err)
		}
	})

	t.Run("access token service account impersonation", func(t *testing.T) {
		parent := &apiClient{AccessToken: "access-token", ServiceAccount: "service-account@example.com"}
		child := parent.gmailAPIClient("user@example.com")
		if err := checkDiags(child.loadAndValidate(context.Background())); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("service account key ADC direct DWD", func(t *testing.T) {
		t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", testFakeCredentialsPath)
		child := (&apiClient{}).gmailAPIClient("user@example.com")
		if err := checkDiags(child.loadAndValidate(context.Background())); err != nil {
			t.Fatal(err)
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(statusCode int, value interface{}) *http.Response {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func authorizedUserADCJSON(quotaProject string) []byte {
	contents, err := json.Marshal(map[string]string{
		"type":             "authorized_user",
		"client_id":        "client-id.apps.googleusercontent.com",
		"client_secret":    "client-secret",
		"refresh_token":    "refresh-token",
		"quota_project_id": quotaProject,
	})
	if err != nil {
		panic(err)
	}
	return contents
}

func writeAuthorizedUserADC(t *testing.T, quotaProject string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "application_default_credentials.json")
	if err := os.WriteFile(path, authorizedUserADCJSON(quotaProject), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigLoadAndValidate_credsInvalidJSON(t *testing.T) {
	config := &apiClient{
		Credentials:           "{this is not json}",
		ImpersonatedUserEmail: "my-fake-email@example.com",
	}

	diags := config.loadAndValidate(context.Background())
	if !diags.HasError() {
		t.Fatalf("expected error, but got nil")
	}
}

func TestConfigLoadAndValidate_credsJSON(t *testing.T) {
	contents, err := os.ReadFile(testFakeCredentialsPath)
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	config := &apiClient{
		Credentials:           string(contents),
		ImpersonatedUserEmail: "my-fake-email@example.com",
	}

	diags := config.loadAndValidate(context.Background())
	err = checkDiags(diags)
	if err != nil {
		t.Fatal(err)
	}
}

func TestConfigLoadAndValidate_credsFromFile(t *testing.T) {
	config := &apiClient{
		Credentials:           testFakeCredentialsPath,
		ImpersonatedUserEmail: "my-fake-email@example.com",
	}

	diags := config.loadAndValidate(context.Background())
	err := checkDiags(diags)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAccConfigLoadAndValidate_credsFromEnv(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Network access not allowed; use TF_ACC=1 to enable")
	}

	testAccPreCheck(t)

	creds := getTestCredsFromEnv()
	config := &apiClient{
		Credentials:           creds,
		Customer:              os.Getenv("GOOGLEWORKSPACE_CUSTOMER_ID"),
		ImpersonatedUserEmail: os.Getenv("GOOGLEWORKSPACE_IMPERSONATED_USER_EMAIL"),
	}

	diags := config.loadAndValidate(context.Background())
	err := checkDiags(diags)
	if err != nil {
		t.Fatal(err)
	}

	diags = checkValidCreds(config)
	err = checkDiags(diags)
	if err != nil {
		t.Fatal(err)
	}
}

func TestConfigLoadAndValidate_credsNoImpersonation(t *testing.T) {
	config := &apiClient{
		Credentials: testFakeCredentialsPath,
	}

	diags := config.loadAndValidate(context.Background())
	err := checkDiags(diags)
	if err != nil {
		t.Fatal(err)
	}
}

func TestConfigOauthScopes_custom(t *testing.T) {
	config := &apiClient{
		Credentials:           testFakeCredentialsPath,
		ClientScopes:          []string{"https://www.googleapis.com/auth/admin/directory"},
		ImpersonatedUserEmail: "my-fake-email@example.com",
	}

	diags := config.loadAndValidate(context.Background())
	err := checkDiags(diags)
	if err != nil {
		t.Fatal(err)
	}

	if len(config.ClientScopes) != 1 {
		t.Fatalf("expected 1 scope, got %d scopes: %v", len(config.ClientScopes), config.ClientScopes)
	}
	if config.ClientScopes[0] != "https://www.googleapis.com/auth/admin/directory" {
		t.Fatalf("expected scope to be %q, got %q", "https://www.googleapis.com/auth/admin/directory", config.ClientScopes[0])
	}
}

func TestConfigLoadAndValidate_accessTokenInvalid(t *testing.T) {
	config := &apiClient{
		AccessToken:           "abcdefghijklmnopqrstuvwxyz",
		Customer:              os.Getenv("GOOGLEWORKSPACE_CUSTOMER_ID"),
		ImpersonatedUserEmail: os.Getenv("GOOGLEWORKSPACE_IMPERSONATED_USER_EMAIL"),
		ClientScopes:          []string{"https://www.googleapis.com/auth/admin.directory.domain"},
	}

	config.loadAndValidate(context.Background())
	diags := checkValidCreds(config)
	err := checkDiags(diags)
	if err == nil {
		t.Fatalf("expected error, but got nil")
	}
}

func TestConfigLoadAndValidate_accessToken(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Network access not allowed; use TF_ACC=1 to enable")
	}

	testAccPreCheck(t)

	creds := getTestCredsFromEnv()
	gcpConfig := &apiClient{
		Credentials:  creds,
		ClientScopes: []string{"https://www.googleapis.com/auth/cloud-platform"},
	}

	diags := gcpConfig.loadAndValidate(context.Background())
	err := checkDiags(diags)
	if err != nil {
		t.Fatal(err)
	}

	iamCredsService, err := iamcredentials.NewService(context.Background(), option.WithHTTPClient(gcpConfig.client))
	if err != nil {
		t.Fatal(err)
	}
	serviceAccount := fmt.Sprintf("projects/-/serviceAccounts/%s", os.Getenv("GOOGLEWORKSPACE_IMPERSONATED_SERVICE_ACCOUNT"))
	tokenRequest := &iamcredentials.GenerateAccessTokenRequest{
		Lifetime: "300s",
		Scope:    []string{"https://www.googleapis.com/auth/cloud-platform"},
	}
	at, err := iamCredsService.Projects.ServiceAccounts.GenerateAccessToken(serviceAccount, tokenRequest).Do()
	if err != nil {
		t.Fatal(err)
	}

	config := &apiClient{
		AccessToken:           at.AccessToken,
		Customer:              os.Getenv("GOOGLEWORKSPACE_CUSTOMER_ID"),
		ImpersonatedUserEmail: os.Getenv("GOOGLEWORKSPACE_IMPERSONATED_USER_EMAIL"),
		ServiceAccount:        os.Getenv("GOOGLEWORKSPACE_IMPERSONATED_SERVICE_ACCOUNT"),
	}

	diags = config.loadAndValidate(context.Background())
	err = checkDiags(diags)
	if err != nil {
		t.Fatal(err)
	}

	diags = checkValidCreds(config)
	err = checkDiags(diags)
	if err != nil {
		t.Fatal(err)
	}
}

// TestConfigLoadAndValidate_accessTokenOnly covers a scenario where:
// 1. A service account is given an Admin Role in Google Workspace directly (no impersonation used in this test)
// 2. That role gives it Admin API privileges to query the groups endpoint of the Admin API - `Groups Admin role`
// The provider will then only need to be configured with the customer ID and an access token for that service account
func TestConfigLoadAndValidate_accessTokenOnly(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("Network access not allowed; use TF_ACC=1 to enable")
	}

	testAccPreCheck(t)

	// Get access token for the service account
	// --- Use service account credentials to request the access token
	// --- Request the `/auth/admin.directory.group` scope as it matches privileges in the Groups Admin role
	credsFile := getTestCredsFromEnv()

	contents, _, err := pathOrContents(credsFile)
	if err != nil {
		t.Fatalf("could not get credentials: %s", err.Error())
	}

	credParams := googleoauth.CredentialsParams{
		Scopes: []string{"https://www.googleapis.com/auth/admin.directory.group"},
	}

	creds, err := googleoauth.CredentialsFromJSONWithParams(context.Background(), []byte(contents), credParams)
	if err != nil {
		t.Fatalf("could not get oauth2 credentials: %s", err.Error())
	}

	at, err := creds.TokenSource.Token()
	if err != nil {
		t.Fatalf("could not get token from oauth2 credentials: %s", err.Error())
	}

	// Configure the provider with the scoped access token from above + the customer ID
	config := &apiClient{
		AccessToken: at.AccessToken,
		Customer:    os.Getenv("GOOGLEWORKSPACE_CUSTOMER_ID"),
	}

	diags := config.loadAndValidate(context.Background())
	err = checkDiags(diags)
	if err != nil {
		t.Fatal(err)
	}

	diags = checkValidCredsGroupAdmin(config)
	err = checkDiags(diags)
	if err != nil {
		t.Fatal(err)
	}
}

func checkValidCreds(config *apiClient) diag.Diagnostics {
	var diags diag.Diagnostics

	directoryService, diags := config.NewDirectoryService()
	if diags.HasError() {
		return diags
	}

	_, err := directoryService.Customers.Get(config.Customer).Do()
	if err != nil {
		return diag.FromErr(err)
	}

	return diags
}

// checkValidCredsGroupAdmin makes an arbitary API call to check the auth is set correctly.
// It makes a groups-related API call, to be used when testing auth relted to service accounts
// given the Group Admin role directly (no impersonisation done during the auth)
func checkValidCredsGroupAdmin(config *apiClient) diag.Diagnostics {
	var diags diag.Diagnostics

	directoryService, diags := config.NewDirectoryService()
	if diags.HasError() {
		return diags
	}
	groupsService := directoryService.Groups
	_, err := groupsService.List().Customer(config.Customer).Do()
	if err != nil {
		return diag.FromErr(err)
	}

	return diags
}
