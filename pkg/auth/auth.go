package auth

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// Authenticator provides tokens for accessing Azure & Fabric resources.
type Authenticator struct {
	cred *azidentity.AzureCLICredential
}

// NewAuthenticator creates a new authenticator using the az cli context.
func NewAuthenticator() (*Authenticator, error) {
	cred, err := azidentity.NewAzureCLICredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure CLI credential: %w", err)
	}
	return &Authenticator{
		cred: cred,
	}, nil
}

// GetToken returns an OAuth token for the requested scopes.
func (a *Authenticator) GetToken(ctx context.Context, scopes []string) (azcore.AccessToken, error) {
	return a.cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: scopes})
}

// DevOpsScope is the target scope for Azure DevOps REST APIs.
const DevOpsScope = "499b84ac-1321-427f-aa17-267ca6975798/.default"

// FabricScope is the target scope for Power BI and Fabric REST APIs.
const FabricScope = "https://api.fabric.microsoft.com/.default"

// Also could be "https://analysis.windows.net/powerbi/api/.default", they often use the same token space.
