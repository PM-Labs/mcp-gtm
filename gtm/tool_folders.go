package gtm

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ListFoldersInput is the input for list_folders tool.
type ListFoldersInput struct {
	AccountID   string `json:"accountId" jsonschema:"description:The GTM account ID"`
	ContainerID string `json:"containerId" jsonschema:"description:The GTM container ID"`
	WorkspaceID string `json:"workspaceId" jsonschema:"description:The GTM workspace ID"`
}

// ListFoldersOutput is the output for list_folders tool.
type ListFoldersOutput struct {
	Folders []Folder `json:"folders"`
}

// GetFolderEntitiesInput is the input for get_folder_entities tool.
type GetFolderEntitiesInput struct {
	AccountID   string `json:"accountId" jsonschema:"description:The GTM account ID"`
	ContainerID string `json:"containerId" jsonschema:"description:The GTM container ID"`
	WorkspaceID string `json:"workspaceId" jsonschema:"description:The GTM workspace ID"`
	FolderID    string `json:"folderId" jsonschema:"description:The folder ID"`
}

// GetFolderEntitiesOutput is the output for get_folder_entities tool.
type GetFolderEntitiesOutput struct {
	Entities FolderEntities `json:"entities"`
}

func registerListFolders(server *mcp.Server) {
	handler := func(ctx context.Context, req *mcp.CallToolRequest, input ListFoldersInput) (*mcp.CallToolResult, ListFoldersOutput, error) {
		wc, err := resolveWorkspace(ctx, input.AccountID, input.ContainerID, input.WorkspaceID)
		if err != nil {
			return nil, ListFoldersOutput{}, err
		}

		folders, err := wc.Client.ListFolders(ctx, wc.AccountID, wc.ContainerID, wc.WorkspaceID)
		if err != nil {
			return nil, ListFoldersOutput{}, err
		}

		return nil, ListFoldersOutput{Folders: folders}, nil
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_folders",
		Description: "List all folders (trigger groups) in a GTM workspace. Folders help organize tags, triggers, and variables.",
	}, handler)
}

func registerGetFolderEntities(server *mcp.Server) {
	handler := func(ctx context.Context, req *mcp.CallToolRequest, input GetFolderEntitiesInput) (*mcp.CallToolResult, GetFolderEntitiesOutput, error) {
		wc, err := resolveWorkspace(ctx, input.AccountID, input.ContainerID, input.WorkspaceID)
		if err != nil {
			return nil, GetFolderEntitiesOutput{}, err
		}

		entities, err := wc.Client.GetFolderEntities(ctx, wc.AccountID, wc.ContainerID, wc.WorkspaceID, input.FolderID)
		if err != nil {
			return nil, GetFolderEntitiesOutput{}, err
		}

		return nil, GetFolderEntitiesOutput{Entities: *entities}, nil
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_folder_entities",
		Description: "Get the tags, triggers, and variables inside a specific folder.",
	}, handler)
}

// AddToFolderInput is the input for add_to_folder tool.
type AddToFolderInput struct {
	AccountID   string   `json:"accountId" jsonschema:"description:The GTM account ID"`
	ContainerID string   `json:"containerId" jsonschema:"description:The GTM container ID"`
	WorkspaceID string   `json:"workspaceId" jsonschema:"description:The GTM workspace ID"`
	FolderName  string   `json:"folderName" jsonschema:"description:Folder name (exact match\\, capitals matter). Created if no folder has this name."`
	TagIDs      []string `json:"tagIds,omitempty" jsonschema:"description:Tag IDs to move into the folder (optional)"`
	TriggerIDs  []string `json:"triggerIds,omitempty" jsonschema:"description:Trigger IDs to move into the folder (optional)"`
	VariableIDs []string `json:"variableIds,omitempty" jsonschema:"description:Variable IDs to move into the folder (optional)"`
}

// AddToFolderOutput is the output for add_to_folder tool.
type AddToFolderOutput struct {
	Success bool              `json:"success"`
	Result  AddToFolderResult `json:"result"`
	Message string            `json:"message"`
}

func registerAddToFolder(server *mcp.Server) {
	handler := func(ctx context.Context, req *mcp.CallToolRequest, input AddToFolderInput) (*mcp.CallToolResult, AddToFolderOutput, error) {
		wc, err := resolveWorkspace(ctx, input.AccountID, input.ContainerID, input.WorkspaceID)
		if err != nil {
			return nil, AddToFolderOutput{}, err
		}

		res, err := wc.Client.AddToFolder(ctx, wc.AccountID, wc.ContainerID, wc.WorkspaceID, input.FolderName, input.TagIDs, input.TriggerIDs, input.VariableIDs)
		if err != nil {
			return nil, AddToFolderOutput{}, err
		}

		msg := "Moved items into existing folder. Workspace change only; publish a version to make it live."
		if res.FolderCreated {
			msg = "Created the folder and moved items into it. Workspace change only; publish a version to make it live."
		}
		return nil, AddToFolderOutput{Success: true, Result: *res, Message: msg}, nil
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_to_folder",
		Description: "Put tags, triggers and variables into a folder, creating the folder first if no folder has that exact name. Safe to repeat. An item lives in one folder, so moving it removes it from its old one.",
	}, handler)
}
