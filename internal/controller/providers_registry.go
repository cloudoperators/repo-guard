package controller

import externalprovider "github.com/cloudoperators/repo-guard/internal/external-provider"

var (
	LDAPGroupProviders   = make(map[string]externalprovider.ExternalProvider)
	GenericHTTPProviders map[string]externalprovider.ExternalProvider
	StaticProviders      map[string]externalprovider.ExternalProvider
)

func init() {
	LDAPGroupProviders = make(map[string]externalprovider.ExternalProvider)
	GenericHTTPProviders = make(map[string]externalprovider.ExternalProvider)
	StaticProviders = make(map[string]externalprovider.ExternalProvider)
}
