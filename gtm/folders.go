package gtm

import (
	"context"
	"fmt"
	"strings"

	tagmanager "google.golang.org/api/tagmanager/v2"
)

// Folder is a simplified representation of a GTM folder.
type Folder struct {
	FolderID string `json:"folderId"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Notes    string `json:"notes,omitempty"`
}

// FolderEntities contains the entities within a folder.
type FolderEntities struct {
	Tags      []string `json:"tags,omitempty"`
	Triggers  []string `json:"triggers,omitempty"`
	Variables []string `json:"variables,omitempty"`
}

// ListFolders returns all folders in a workspace.
func (c *Client) ListFolders(ctx context.Context, accountID, containerID, workspaceID string) ([]Folder, error) {
	folders, err := c.allFolders(ctx, BuildWorkspacePath(accountID, containerID, workspaceID))
	if err != nil {
		return nil, err
	}
	return toFolders(folders), nil
}

// allFolders returns every folder in a workspace, following pagination.
func (c *Client) allFolders(ctx context.Context, parent string) ([]*tagmanager.Folder, error) {
	var folders []*tagmanager.Folder
	_, err := retryWithBackoff(ctx, 3, func() (struct{}, error) {
		folders = nil // a retry restarts the listing from page one
		return struct{}{}, c.Service.Accounts.Containers.Workspaces.Folders.List(parent).Context(ctx).
			Pages(ctx, func(r *tagmanager.ListFoldersResponse) error {
				folders = append(folders, r.Folder...)
				return nil
			})
	})
	if err != nil {
		return nil, mapGoogleError(err)
	}
	return folders, nil
}

// GetFolderEntities returns the names of the tags, triggers and variables in a folder.
//
// It does not call the Tag Manager folders.entities endpoint, which answers 404 for
// folders that exist. Instead it lists the workspace's tags, triggers and variables
// and keeps the ones whose parentFolderId matches.
func (c *Client) GetFolderEntities(ctx context.Context, accountID, containerID, workspaceID, folderID string) (*FolderEntities, error) {
	parent := BuildWorkspacePath(accountID, containerID, workspaceID)

	folders, err := c.allFolders(ctx, parent)
	if err != nil {
		return nil, err
	}
	found := false
	for _, f := range folders {
		if f.FolderId == folderID {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: folder %s does not exist in this workspace", ErrNotFound, folderID)
	}

	entities := &FolderEntities{}
	ws := c.Service.Accounts.Containers.Workspaces

	if _, err := retryWithBackoff(ctx, 3, func() (struct{}, error) {
		entities.Tags = nil
		return struct{}{}, ws.Tags.List(parent).Context(ctx).Pages(ctx, func(r *tagmanager.ListTagsResponse) error {
			for _, t := range r.Tag {
				if t.ParentFolderId == folderID {
					entities.Tags = append(entities.Tags, t.Name)
				}
			}
			return nil
		})
	}); err != nil {
		return nil, mapGoogleError(err)
	}

	if _, err := retryWithBackoff(ctx, 3, func() (struct{}, error) {
		entities.Triggers = nil
		return struct{}{}, ws.Triggers.List(parent).Context(ctx).Pages(ctx, func(r *tagmanager.ListTriggersResponse) error {
			for _, t := range r.Trigger {
				if t.ParentFolderId == folderID {
					entities.Triggers = append(entities.Triggers, t.Name)
				}
			}
			return nil
		})
	}); err != nil {
		return nil, mapGoogleError(err)
	}

	if _, err := retryWithBackoff(ctx, 3, func() (struct{}, error) {
		entities.Variables = nil
		return struct{}{}, ws.Variables.List(parent).Context(ctx).Pages(ctx, func(r *tagmanager.ListVariablesResponse) error {
			for _, v := range r.Variable {
				if v.ParentFolderId == folderID {
					entities.Variables = append(entities.Variables, v.Name)
				}
			}
			return nil
		})
	}); err != nil {
		return nil, mapGoogleError(err)
	}

	return entities, nil
}

// AddToFolderResult reports what add_to_folder did.
type AddToFolderResult struct {
	FolderID       string `json:"folderId"`
	FolderName     string `json:"folderName"`
	FolderCreated  bool   `json:"folderCreated"`
	TagsMoved      int    `json:"tagsMoved"`
	TriggersMoved  int    `json:"triggersMoved"`
	VariablesMoved int    `json:"variablesMoved"`
}

// AddToFolder moves tags, triggers and variables into the folder with the given name,
// creating the folder first if no folder has exactly that name. If several folders share
// the name, the first one listed is used. Workspace-only: nothing is versioned or published.
func (c *Client) AddToFolder(ctx context.Context, accountID, containerID, workspaceID, folderName string, tagIDs, triggerIDs, variableIDs []string) (*AddToFolderResult, error) {
	name := strings.TrimSpace(folderName)
	if err := ValidateAddToFolderInput(name, tagIDs, triggerIDs, variableIDs); err != nil {
		return nil, err
	}
	parent := BuildWorkspacePath(accountID, containerID, workspaceID)
	folders := c.Service.Accounts.Containers.Workspaces.Folders

	existing, err := c.allFolders(ctx, parent)
	if err != nil {
		return nil, err
	}
	var folder *tagmanager.Folder
	for _, f := range existing {
		if strings.TrimSpace(f.Name) == name {
			folder = f
			break
		}
	}

	created := false
	if folder == nil {
		folder, err = retryWithBackoff(ctx, 3, func() (*tagmanager.Folder, error) {
			return folders.Create(parent, &tagmanager.Folder{Name: name}).Context(ctx).Do()
		})
		if err != nil {
			return nil, mapGoogleError(err)
		}
		created = true
	}

	_, err = retryWithBackoff(ctx, 3, func() (struct{}, error) {
		return struct{}{}, folders.MoveEntitiesToFolder(folder.Path, folder).
			TagId(tagIDs...).TriggerId(triggerIDs...).VariableId(variableIDs...).Context(ctx).Do()
	})
	if err != nil {
		mapped := mapGoogleError(err)
		if created {
			return nil, fmt.Errorf("%w (the folder %q, id %s, was created by this call and has been left in place)", mapped, name, folder.FolderId)
		}
		return nil, mapped
	}

	return &AddToFolderResult{
		FolderID:       folder.FolderId,
		FolderName:     name,
		FolderCreated:  created,
		TagsMoved:      len(tagIDs),
		TriggersMoved:  len(triggerIDs),
		VariablesMoved: len(variableIDs),
	}, nil
}

func toFolders(folders []*tagmanager.Folder) []Folder {
	result := make([]Folder, 0, len(folders))
	for _, f := range folders {
		result = append(result, Folder{
			FolderID: f.FolderId,
			Name:     f.Name,
			Path:     f.Path,
			Notes:    f.Notes,
		})
	}
	return result
}
