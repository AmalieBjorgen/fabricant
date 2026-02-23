package devops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/amaliebjorgen/fabricant/pkg/auth"
)

// Client is the REST client for Azure DevOps APIs.
type Client struct {
	auth       *auth.Authenticator
	httpClient *http.Client
}

// NewClient creates a new Azure DevOps API client.
func NewClient(authenticator *auth.Authenticator) *Client {
	return &Client{
		auth:       authenticator,
		httpClient: &http.Client{},
	}
}

// doRequest performs a request against the Azure DevOps REST API.
func (c *Client) doRequest(ctx context.Context, organization, method, path string, body interface{}, out interface{}) error {
	baseURL := fmt.Sprintf("https://dev.azure.com/%s", organization)

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewBuffer(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, reqBody)
	if err != nil {
		return err
	}

	token, err := c.auth.GetToken(ctx, []string{auth.DevOpsScope})
	if err != nil {
		return fmt.Errorf("getting devops token: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("devops API error %d: %s", resp.StatusCode, string(b))
	}

	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return err
		}
	}
	return nil
}

// GitRef represents a git reference (branch, tag).
type GitRef struct {
	Name     string `json:"name"`
	ObjectId string `json:"objectId"`
}

// GitRefsResponse is the response wrapper for refs list.
type GitRefsResponse struct {
	Count int      `json:"count"`
	Value []GitRef `json:"value"`
}

// GetBranchObjectId gets the latest commit ID (objectId) for a specific branch.
func (c *Client) GetBranchObjectId(ctx context.Context, org, project, repo, branchName string) (string, error) {
	// The API filter requires just the branch name prefix without "refs/heads/"
	filterName := strings.TrimPrefix(branchName, "refs/heads/")
	path := fmt.Sprintf("/%s/_apis/git/repositories/%s/refs?filter=heads/%s&api-version=7.1", project, repo, filterName)

	var res GitRefsResponse
	err := c.doRequest(ctx, org, http.MethodGet, path, nil, &res)
	if err != nil {
		return "", err
	}

	if res.Count == 0 {
		return "", fmt.Errorf("branch %s not found in repo %s", branchName, repo)
	}

	return res.Value[0].ObjectId, nil
}

// CreateBranchRequest represents an update refs payload.
type GitRefUpdate struct {
	Name        string `json:"name"`        // The branch to create (e.g. refs/heads/feature/xxx)
	OldObjectId string `json:"oldObjectId"` // 0000000000000000000000000000000000000000 for new branch
	NewObjectId string `json:"newObjectId"` // The commit ID to point to
}

// CreateBranch creates a new git branch based on a commit ID.
func (c *Client) CreateBranch(ctx context.Context, org, project, repo, newBranchName, baseObjectId string) error {
	fullBranchName := newBranchName
	if !strings.HasPrefix(fullBranchName, "refs/heads/") {
		fullBranchName = "refs/heads/" + fullBranchName
	}

	updates := []GitRefUpdate{
		{
			Name:        fullBranchName,
			OldObjectId: "0000000000000000000000000000000000000000",
			NewObjectId: baseObjectId,
		},
	}

	path := fmt.Sprintf("/%s/_apis/git/repositories/%s/refs?api-version=7.1", project, repo)

	// Since we are creating a ref, ADO responds with an array of GitRefUpdateResult.
	// We'll just ignore the body for now, but ensure it completes parsing.
	return c.doRequest(ctx, org, http.MethodPost, path, updates, nil)
}
