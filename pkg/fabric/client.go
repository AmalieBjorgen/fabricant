package fabric

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/amaliebjorgen/fabricant/pkg/auth"
)

const BaseURL = "https://api.fabric.microsoft.com/v1"

// Client is the REST client for Microsoft Fabric APIs.
type Client struct {
	auth       *auth.Authenticator
	httpClient *http.Client
}

// NewClient creates a new Fabric API client.
func NewClient(authenticator *auth.Authenticator) *Client {
	return &Client{
		auth:       authenticator,
		httpClient: &http.Client{},
	}
}

// doRequest performs a request against the Fabric API.
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}, out interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewBuffer(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, BaseURL+path, reqBody)
	if err != nil {
		return nil, err
	}

	token, err := c.auth.GetToken(ctx, []string{auth.FabricScope})
	if err != nil {
		return nil, fmt.Errorf("failed to get fabric auth token: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	// We don't defer Close here if returning early on error, but caller might need body? Actually, we'll read and decode here, so we CAN defer close.
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return resp, fmt.Errorf("fabric API error %d: %s", resp.StatusCode, string(b))
	}

	if out != nil && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusAccepted {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp, err
		}
	}
	return resp, nil
}

// GitProviderDetails holds the configuration for a workspace's git connection.
type GitProviderDetails struct {
	OrganizationName string `json:"organizationName"`
	ProjectName      string `json:"projectName"`
	RepositoryName   string `json:"repositoryName"`
	BranchName       string `json:"branchName"`
	DirectoryName    string `json:"directoryName"`
	GitProviderType  string `json:"gitProviderType"`
}

// Workspace represents a Fabric Workspace.
type Workspace struct {
	Id                 string              `json:"id"`
	DisplayName        string              `json:"displayName"`
	Description        string              `json:"description,omitempty"`
	Type               string              `json:"type"`
	CapacityId         string              `json:"capacityId,omitempty"`
	GitProviderDetails *GitProviderDetails `json:"gitProviderDetails,omitempty"`
}

// CreateWorkspaceRequest is the payload for creating a new workspace.
type CreateWorkspaceRequest struct {
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	CapacityId  string `json:"capacityId,omitempty"`
}

// GetWorkspace calls GET /workspaces/{workspaceId}
func (c *Client) GetWorkspace(ctx context.Context, id string) (*Workspace, error) {
	var ws Workspace
	_, err := c.doRequest(ctx, http.MethodGet, "/workspaces/"+id, nil, &ws)
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

// GetGitConnectionResponse represents the wrapper response for getting a git connection.
type GetGitConnectionResponse struct {
	GitProviderDetails *GitProviderDetails `json:"gitProviderDetails"`
}

// GetGitConnection calls GET /workspaces/{workspaceId}/git/connection
func (c *Client) GetGitConnection(ctx context.Context, id string) (*GetGitConnectionResponse, error) {
	var resp GetGitConnectionResponse
	_, err := c.doRequest(ctx, http.MethodGet, "/workspaces/"+id+"/git/connection", nil, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GitStatus represents the current sync status of the workspace with Git.
type GitStatus struct {
	RemoteCommitHash string `json:"remoteCommitHash"`
	WorkspaceHead    string `json:"workspaceHead"`
}

// GetGitStatus calls GET /workspaces/{workspaceId}/git/status
func (c *Client) GetGitStatus(ctx context.Context, id string) (*GitStatus, error) {
	var resp GitStatus
	// We might need to wait for the status to initialize or compute, it returns 200/202.
	_, err := c.doRequest(ctx, http.MethodGet, "/workspaces/"+id+"/git/status", nil, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// CreateWorkspace calls POST /workspaces
func (c *Client) CreateWorkspace(ctx context.Context, req CreateWorkspaceRequest) (*Workspace, error) {
	var ws Workspace
	_, err := c.doRequest(ctx, http.MethodPost, "/workspaces", req, &ws)
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

// ConnectToGitRequest connects a workspace to git.
type ConnectToGitRequest struct {
	GitProviderDetails *GitProviderDetails `json:"gitProviderDetails"`
}

// ConnectWorkspaceToGit links a workspace to a git repository and branch.
func (c *Client) ConnectWorkspaceToGit(ctx context.Context, workspaceId string, req ConnectToGitRequest) error {
	path := fmt.Sprintf("/workspaces/%s/git/connect", workspaceId)
	_, err := c.doRequest(ctx, http.MethodPost, path, req, nil)
	return err
}

// InitializeGitConnection initializes the git connection for a workspace.
func (c *Client) InitializeGitConnection(ctx context.Context, workspaceId string) error {
	path := fmt.Sprintf("/workspaces/%s/git/initializeConnection", workspaceId)
	req := map[string]interface{}{}
	_, err := c.doRequest(ctx, http.MethodPost, path, req, nil)
	return err
}

// UpdateWorkspaceFromGit updates the workspace items from the linked git branch. Returns operation ID empty string if not long-running.
func (c *Client) UpdateWorkspaceFromGit(ctx context.Context, workspaceId string, workspaceHead string, remoteCommitHash string) (string, error) {
	path := fmt.Sprintf("/workspaces/%s/git/updateFromGit", workspaceId)

	req := map[string]interface{}{
		"remoteCommitHash": remoteCommitHash,
		"conflictResolution": map[string]string{
			"conflictResolutionType":   "Workspace",
			"conflictResolutionPolicy": "PreferRemote",
		},
		"options": map[string]bool{
			"allowOverrideItems": true,
		},
	}
	// The API doc says workspaceHead is required if not empty. For a new workspace, we can just omit or pass empty string if it's new, but typically we must pass the current head.
	if workspaceHead != "" {
		req["workspaceHead"] = workspaceHead
	}

	resp, err := c.doRequest(ctx, http.MethodPost, path, req, nil)
	if err != nil {
		return "", err
	}

	operationId := resp.Header.Get("x-ms-operation-id")
	return operationId, nil
}

// Connection represents a connection to a data source, e.g., Lakehouse.
type Connection struct {
	Id          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// UpdateConnectionRequest allows patching a connection mapping.
type UpdateConnectionRequest struct {
	UpdateDetails map[string]interface{} `json:"updateDetails"`
}

// UpdateConnections is a placeholder to patch hardcoded Lakehouse connections.
// The actual API endpoint might require retrieving connections and patching them one by one.
func (c *Client) UpdateConnections(ctx context.Context, workspaceId string, mappings map[string]string) error {
	// TODO: Replace with the actual iterate and update logic depending on Lakehouse binding types in Fabric.
	// We'll iterate the items/connections and redirect them to the new workspace's lakehouse.
	// e.g., foreach conn in mapping: _, err = c.doRequest(ctx, "PATCH", "/connections/"+conn.Id, UpdateDetails, nil)
	return nil
}

// OperationStatus represents the response from the Fabric Operations API.
type OperationStatus struct {
	Status      string `json:"status"` // e.g. "NotStarted", "Running", "Succeeded", "Failed"
	CreatedTime string `json:"createdTime,omitempty"`
	LastUpdated string `json:"lastUpdatedTime,omitempty"`
	Error       struct {
		ErrorCode string `json:"errorCode"`
		Message   string `json:"message"`
	} `json:"error,omitempty"`
}

// GetOperationStatus calls GET /operations/{operationId}
func (c *Client) GetOperationStatus(ctx context.Context, operationId string) (*OperationStatus, error) {
	var resp OperationStatus
	// Note: Operations API is typically at the tenant or workspace root? Fabric usually exposes it at /operations/
	// Actually, Fabric Git API often returns a Location header or you poll /operations/{operationId}
	// The API for Microsoft Fabric operations is usually GET https://api.fabric.microsoft.com/v1/operations/{id}
	path := fmt.Sprintf("/operations/%s", operationId)
	_, err := c.doRequest(ctx, http.MethodGet, path, nil, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// WorkspaceListResponse represents the response containing an array of Workspaces.
type WorkspaceListResponse struct {
	Value []Workspace `json:"value"`
}

// ListWorkspaces calls GET /workspaces
func (c *Client) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	var resp WorkspaceListResponse
	_, err := c.doRequest(ctx, http.MethodGet, "/workspaces", nil, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Value, nil
}
