package gtm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	tagmanager "google.golang.org/api/tagmanager/v2"
)

// Result is the common output of the extended tools: a status, a message and, for
// tools that return an API object, that object as generic JSON.
type Result struct {
	Success bool           `json:"success"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

// registerExt registers a tool whose handler returns a *Result. It keeps each extended
// tool to its input type plus one function.
func registerExt[In any](server *mcp.Server, name, description string, fn func(ctx context.Context, in In) (*Result, error)) {
	handler := func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Result, error) {
		res, err := fn(ctx, in)
		if err != nil {
			return nil, Result{}, err
		}
		return nil, *res, nil
	}
	mcp.AddTool(server, &mcp.Tool{Name: name, Description: description}, handler)
}

// svc returns an authenticated Tag Manager service for the request.
func svc(ctx context.Context) (*tagmanager.Service, error) {
	c, err := getClient(ctx)
	if err != nil {
		return nil, err
	}
	return c.Service, nil
}

// toMap converts an API object into generic JSON for tool output.
func toMap(v any) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// okData builds a successful Result carrying an API object.
func okData(message string, v any) (*Result, error) {
	m, err := toMap(v)
	if err != nil {
		return nil, err
	}
	return &Result{Success: true, Message: message, Data: m}, nil
}

// okMsg builds a successful Result with no data.
func okMsg(format string, args ...any) (*Result, error) {
	return &Result{Success: true, Message: fmt.Sprintf(format, args...)}, nil
}

// fromBody decodes a caller-supplied JSON object into an API struct. Unknown fields are
// rejected so a misspelt property fails loudly instead of being silently dropped.
func fromBody(body map[string]any, dst any) error {
	if len(body) == 0 {
		return fmt.Errorf("body is required")
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid body: %w", err)
	}
	return nil
}

// need returns an error naming the first empty required field.
func need(pairs ...string) error {
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			return fmt.Errorf("%s is required", pairs[i])
		}
	}
	return nil
}

// needConfirm is the safety guard for destructive or live-affecting tools.
func needConfirm(confirm bool, action string) error {
	if !confirm {
		return fmt.Errorf("%s requires confirm: true. This is a safety guard against accidental changes", action)
	}
	return nil
}

// read runs an idempotent call with the shared retry/backoff and maps Google errors.
func read[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	out, err := retryWithBackoff(ctx, 3, fn)
	if err != nil {
		var zero T
		return zero, mapGoogleError(err)
	}
	return out, nil
}

// wsPath validates workspace IDs and returns the workspace path.
func wsPath(account, container, workspace string) (string, error) {
	if err := ValidateWorkspacePath(account, container, workspace); err != nil {
		return "", err
	}
	return BuildWorkspacePath(account, container, workspace), nil
}

// ctPath validates container IDs and returns the container path.
func ctPath(account, container string) (string, error) {
	if err := ValidateContainerPath(account, container); err != nil {
		return "", err
	}
	return BuildContainerPath(account, container), nil
}

// WorkspaceRef identifies a workspace. Embedded by the extended tools' input types.
type WorkspaceRef struct {
	AccountID   string `json:"accountId" jsonschema:"description:The GTM account ID"`
	ContainerID string `json:"containerId" jsonschema:"description:The GTM container ID"`
	WorkspaceID string `json:"workspaceId" jsonschema:"description:The GTM workspace ID"`
}

// ContainerRef identifies a container. Embedded by the extended tools' input types.
type ContainerRef struct {
	AccountID   string `json:"accountId" jsonschema:"description:The GTM account ID"`
	ContainerID string `json:"containerId" jsonschema:"description:The GTM container ID"`
}
