// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package googleworkspace

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/hashicorp/go-cleanhttp"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"

	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"

	directory "google.golang.org/api/admin/directory/v1"
	"google.golang.org/api/chromepolicy/v1"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/groupssettings/v1"
	"google.golang.org/api/impersonate"
	"google.golang.org/api/option"
	"google.golang.org/api/transport"
	googlehttptransport "google.golang.org/api/transport/http"
)

const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

type authenticationSource int

const (
	authenticationSourceAccessToken authenticationSource = iota
	authenticationSourceCredentials
	authenticationSourceADC
)

type authenticationRoute int

const (
	authenticationRouteAccessToken authenticationRoute = iota
	authenticationRouteAccessTokenImpersonation
	authenticationRouteCredentials
	authenticationRouteADC
	authenticationRouteADCServiceAccountKey
	authenticationRouteADCImpersonation
	authenticationRouteInvalidImpersonation
)

type apiClient struct {
	client *http.Client

	AccessToken           string
	ClientScopes          []string
	Credentials           string
	Customer              string
	ImpersonatedUserEmail string
	ServiceAccount        string
	UserAgent             string

	allowDirectPrincipal bool
}

func selectAuthenticationRoute(source authenticationSource, adcType googleoauth.CredentialsType, serviceAccount, subject string, allowDirectPrincipal bool) authenticationRoute {
	switch source {
	case authenticationSourceAccessToken:
		if subject == "" {
			return authenticationRouteAccessToken
		}
		if serviceAccount != "" {
			return authenticationRouteAccessTokenImpersonation
		}
		if allowDirectPrincipal {
			return authenticationRouteAccessToken
		}
		return authenticationRouteInvalidImpersonation
	case authenticationSourceCredentials:
		return authenticationRouteCredentials
	case authenticationSourceADC:
		if adcType == googleoauth.ServiceAccount {
			return authenticationRouteADCServiceAccountKey
		}
		if subject == "" {
			return authenticationRouteADC
		}
		if serviceAccount != "" {
			return authenticationRouteADCImpersonation
		}
		if allowDirectPrincipal {
			return authenticationRouteADC
		}
		return authenticationRouteInvalidImpersonation
	default:
		return authenticationRouteInvalidImpersonation
	}
}

func adcCredentialsType(creds *googleoauth.Credentials) (googleoauth.CredentialsType, error) {
	if creds == nil {
		return "", fmt.Errorf("application default credentials are nil")
	}
	if len(creds.JSON) == 0 {
		return "", nil
	}

	var metadata struct {
		Type googleoauth.CredentialsType `json:"type"`
	}
	if err := json.Unmarshal(creds.JSON, &metadata); err != nil {
		return "", fmt.Errorf("could not determine the Application Default Credentials type: %w", err)
	}
	return metadata.Type, nil
}

func (c *apiClient) impersonatedTokenSource(ctx context.Context, opts ...option.ClientOption) (oauth2.TokenSource, error) {
	return impersonate.CredentialsTokenSource(ctx, impersonate.CredentialsConfig{
		TargetPrincipal: c.ServiceAccount,
		Scopes:          c.ClientScopes,
		Subject:         c.ImpersonatedUserEmail,
	}, opts...)
}

func adcImpersonationHTTPClient(ctx context.Context, source *googleoauth.Credentials, base http.RoundTripper) (*http.Client, error) {
	transport, err := googlehttptransport.NewTransport(ctx, base, option.WithCredentials(source))
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: transport}, nil
}

func (c *apiClient) loadAndValidate(ctx context.Context) diag.Diagnostics {
	var diags diag.Diagnostics

	if len(c.ClientScopes) == 0 {
		c.ClientScopes = DefaultClientScopes
	}

	if c.AccessToken != "" {
		contents, _, err := pathOrContents(c.AccessToken)
		if err != nil {
			return diag.FromErr(err)
		}
		token := &oauth2.Token{AccessToken: contents}
		route := selectAuthenticationRoute(authenticationSourceAccessToken, "", c.ServiceAccount, c.ImpersonatedUserEmail, c.allowDirectPrincipal)

		switch route {
		case authenticationRouteAccessTokenImpersonation:
			log.Printf("[INFO] Authenticating using the configured access token, impersonating service account %q as %q", c.ServiceAccount, c.ImpersonatedUserEmail)
			log.Printf("[INFO]   -- Scopes: %s", c.ClientScopes)

			tokenSource, err := c.impersonatedTokenSource(ctx, option.WithTokenSource(oauth2.StaticTokenSource(token)))
			if err != nil {
				return diag.FromErr(err)
			}
			return c.SetupClient(ctx, &googleoauth.Credentials{TokenSource: tokenSource})
		case authenticationRouteInvalidImpersonation:
			diags = append(diags, diag.Diagnostic{
				Severity: diag.Error,
				Summary:  "service_account is required to impersonate a user with the access_token authentication.",
			})
			return diags
		default:
			log.Printf("[INFO] Authenticating directly as the principal of the configured access token")
			return c.SetupClient(ctx, &googleoauth.Credentials{TokenSource: oauth2.StaticTokenSource(token)})
		}
	}

	if c.Credentials != "" {
		contents, _, err := pathOrContents(c.Credentials)
		if err != nil {
			return diag.FromErr(err)
		}

		credParams := googleoauth.CredentialsParams{
			Scopes:  c.ClientScopes,
			Subject: c.ImpersonatedUserEmail,
		}

		creds, err := googleoauth.CredentialsFromJSONWithParams(ctx, []byte(contents), credParams)
		if err != nil {
			return diag.FromErr(err)
		}

		log.Printf("[INFO] Authenticating using configured service account key credentials as %q", c.ImpersonatedUserEmail)
		return c.SetupClient(ctx, creds)
	}

	directParams := googleoauth.CredentialsParams{
		Scopes:  c.ClientScopes,
		Subject: c.ImpersonatedUserEmail,
	}
	adcParams := directParams
	if c.ServiceAccount != "" && c.ImpersonatedUserEmail != "" {
		adcParams = googleoauth.CredentialsParams{Scopes: []string{cloudPlatformScope}}
	}

	creds, err := googleoauth.FindDefaultCredentialsWithParams(ctx, adcParams)
	if err != nil {
		return diag.FromErr(err)
	}
	adcType, err := adcCredentialsType(creds)
	if err != nil {
		return diag.FromErr(err)
	}
	route := selectAuthenticationRoute(authenticationSourceADC, adcType, c.ServiceAccount, c.ImpersonatedUserEmail, c.allowDirectPrincipal)

	switch route {
	case authenticationRouteADCServiceAccountKey:
		if c.ImpersonatedUserEmail != "" && adcParams.Subject == "" {
			creds, err = googleoauth.CredentialsFromJSONWithParams(ctx, creds.JSON, directParams)
			if err != nil {
				return diag.FromErr(err)
			}
		}
		log.Printf("[INFO] Authenticating using Application Default Credentials from a service account key as %q", c.ImpersonatedUserEmail)
		return c.SetupClient(ctx, creds)
	case authenticationRouteADCImpersonation:
		log.Printf("[INFO] Authenticating using Application Default Credentials, impersonating service account %q as %q", c.ServiceAccount, c.ImpersonatedUserEmail)
		log.Printf("[INFO]   -- Scopes: %s", c.ClientScopes)
		sourceClient, err := adcImpersonationHTTPClient(ctx, creds, http.DefaultTransport)
		if err != nil {
			return diag.FromErr(err)
		}
		tokenSource, err := c.impersonatedTokenSource(ctx, option.WithHTTPClient(sourceClient))
		if err != nil {
			return diag.FromErr(err)
		}
		return c.SetupClient(ctx, &googleoauth.Credentials{TokenSource: tokenSource})
	case authenticationRouteInvalidImpersonation:
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary: "impersonated_user_email cannot be applied directly to these Application Default Credentials. " +
				"Set service_account for domain-wide delegation, remove impersonated_user_email when intentionally authenticating as the ADC principal, or use service account key credentials.",
		})
		return diags
	default:
		log.Printf("[INFO] Authenticating directly as the principal of Application Default Credentials")
		return c.SetupClient(ctx, creds)
	}
}

func (c *apiClient) SetupClient(ctx context.Context, creds *googleoauth.Credentials) diag.Diagnostics {
	var diags diag.Diagnostics

	cleanCtx := context.WithValue(ctx, oauth2.HTTPClient, cleanhttp.DefaultClient())

	// 1. MTLS TRANSPORT/CLIENT - sets up proper auth headers
	client, _, err := transport.NewHTTPClient(cleanCtx, option.WithTokenSource(creds.TokenSource))
	if err != nil {
		return diag.FromErr(err)
	}

	// 2. Logging Transport - ensure we log HTTP requests to admin APIs.
	scrubbedLoggingTransport := NewTransportWithScrubbedLogs("Google Workspace", client.Transport)

	// 3. Retry Transport - retries common temporary errors
	// Keep order for wrapping logging so we log each retried request as well.
	// This value should be used if needed to create shallow copies with additional retry predicates.
	// See ClientWithAdditionalRetries
	retryTransport := NewTransportWithDefaultRetries(scrubbedLoggingTransport)

	// Set final transport value.
	client.Transport = retryTransport

	c.client = client
	return diags
}

func (c *apiClient) NewChromePolicyService() (*chromepolicy.Service, diag.Diagnostics) {
	var diags diag.Diagnostics

	log.Printf("[INFO] Instantiating Google Admin Chrome Policy service")

	chromePolicyService, err := chromepolicy.NewService(context.Background(), option.WithHTTPClient(c.client))
	if err != nil {
		return nil, diag.FromErr(err)
	}

	if chromePolicyService == nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Directory Service could not be created.",
		})

		return nil, diags
	}

	return chromePolicyService, diags
}

func (c *apiClient) NewDirectoryService() (*directory.Service, diag.Diagnostics) {
	var diags diag.Diagnostics

	log.Printf("[INFO] Instantiating Google Admin Directory service")

	directoryService, err := directory.NewService(context.Background(), option.WithHTTPClient(c.client))
	if err != nil {
		return nil, diag.FromErr(err)
	}

	if directoryService == nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Directory Service could not be created.",
		})

		return nil, diags
	}

	return directoryService, diags
}
func (c *apiClient) NewGmailService(ctx context.Context, userId string) (*gmail.Service, diag.Diagnostics) {
	var diags diag.Diagnostics

	log.Printf("[INFO] Instantiating Google Admin Gmail service")

	log.Printf("[INFO] Creating Google Admin Gmail client for %q", userId)
	newClient := c.gmailAPIClient(userId)
	diags = newClient.loadAndValidate(ctx)
	if diags.HasError() {
		return nil, diags
	}

	gmailService, err := gmail.NewService(ctx, option.WithHTTPClient(newClient.client))
	if err != nil {
		return nil, diag.FromErr(err)
	}

	if gmailService == nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Gmail Service could not be created.",
		})

		return nil, diags
	}

	return gmailService, diags
}

func (c *apiClient) gmailAPIClient(userID string) *apiClient {
	return &apiClient{
		AccessToken:           c.AccessToken,
		Credentials:           c.Credentials,
		ClientScopes:          c.ClientScopes,
		Customer:              c.Customer,
		UserAgent:             c.UserAgent,
		ImpersonatedUserEmail: userID,
		ServiceAccount:        c.ServiceAccount,
		allowDirectPrincipal:  c.ImpersonatedUserEmail == "",
	}
}

func (c *apiClient) NewGroupsSettingsService() (*groupssettings.Service, diag.Diagnostics) {
	var diags diag.Diagnostics

	log.Printf("[INFO] Instantiating Google Admin Groups Settings service")

	groupsSettingsService, err := groupssettings.NewService(context.Background(), option.WithHTTPClient(c.client))
	if err != nil {
		return nil, diag.FromErr(err)
	}

	if groupsSettingsService == nil {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Error,
			Summary:  "Groups Settings Service could not be created.",
		})

		return nil, diags
	}

	return groupsSettingsService, diags
}
