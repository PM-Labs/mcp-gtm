package gtm

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	tagmanager "google.golang.org/api/tagmanager/v2"
)

// ---- Workspaces -------------------------------------------------------------

type GetWorkspaceInput struct {
	WorkspaceRef
}

type UpdateWorkspaceInput struct {
	WorkspaceRef
	Name        string `json:"name,omitempty" jsonschema:"description:New workspace name (optional)"`
	Description string `json:"description,omitempty" jsonschema:"description:New workspace description (optional)"`
}

type DeleteWorkspaceInput struct {
	WorkspaceRef
	Confirm bool `json:"confirm" jsonschema:"description:Must be true to confirm deletion. Deletes the workspace and every unpublished change in it."`
}

type SyncWorkspaceInput struct {
	WorkspaceRef
}

type ResolveConflictInput struct {
	WorkspaceRef
	Entity      map[string]any `json:"entity" jsonschema:"description:The entity to keep, as returned in a sync mergeConflict: an object with changeStatus plus one of tag, trigger, variable, folder, client, transformation, template or zone"`
	Fingerprint string         `json:"fingerprint,omitempty" jsonschema:"description:Fingerprint of the workspace entity being resolved (optional)"`
}

type QuickPreviewInput struct {
	WorkspaceRef
}

func registerWorkspaceExt(server *mcp.Server) {
	registerExt(server, "get_workspace", "Get one workspace's details (name, description, fingerprint).",
		func(ctx context.Context, in GetWorkspaceInput) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			w, err := read(ctx, func() (*tagmanager.Workspace, error) {
				return s.Accounts.Containers.Workspaces.Get(p).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			return okData("Workspace retrieved", w)
		})

	registerExt(server, "update_workspace", "Rename a workspace or change its description.",
		func(ctx context.Context, in UpdateWorkspaceInput) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if in.Name == "" && in.Description == "" {
				return nil, fmt.Errorf("provide name and/or description")
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			cur, err := read(ctx, func() (*tagmanager.Workspace, error) {
				return s.Accounts.Containers.Workspaces.Get(p).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			if in.Name != "" {
				cur.Name = in.Name
			}
			if in.Description != "" {
				cur.Description = in.Description
			}
			w, err := s.Accounts.Containers.Workspaces.Update(p, cur).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Workspace updated", w)
		})

	registerExt(server, "delete_workspace", "Delete a workspace and all of its unpublished changes. Requires confirm: true. The workspace's changes are lost unless first saved with create_version.",
		func(ctx context.Context, in DeleteWorkspaceInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "delete_workspace"); err != nil {
				return nil, err
			}
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			if err := s.Accounts.Containers.Workspaces.Delete(p).Context(ctx).Do(); err != nil {
				return nil, mapGoogleError(err)
			}
			return okMsg("Workspace %s deleted", in.WorkspaceID)
		})

	registerExt(server, "sync_workspace", "Sync a workspace with the latest container version. Returns any merge conflicts to resolve with resolve_conflict.",
		func(ctx context.Context, in SyncWorkspaceInput) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			r, err := s.Accounts.Containers.Workspaces.Sync(p).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Workspace synced", r)
		})

	registerExt(server, "resolve_conflict", "Resolve a workspace merge conflict by choosing the entity to keep, using the entity shape returned by sync_workspace.",
		func(ctx context.Context, in ResolveConflictInput) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			var e tagmanager.Entity
			if err := fromBody(in.Entity, &e); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			call := s.Accounts.Containers.Workspaces.ResolveConflict(p, &e).Context(ctx)
			if in.Fingerprint != "" {
				call = call.Fingerprint(in.Fingerprint)
			}
			if err := call.Do(); err != nil {
				return nil, mapGoogleError(err)
			}
			return okMsg("Conflict resolved")
		})

	registerExt(server, "quick_preview", "Create a quick-preview version of the workspace (compiles it without saving a version) and report compiler errors. Useful to validate a workspace before create_version.",
		func(ctx context.Context, in QuickPreviewInput) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			r, err := s.Accounts.Containers.Workspaces.QuickPreview(p).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Quick preview compiled", r)
		})
}

// ---- Folders ----------------------------------------------------------------

type CreateFolderInput struct {
	WorkspaceRef
	Name  string `json:"name" jsonschema:"description:Folder name"`
	Notes string `json:"notes,omitempty" jsonschema:"description:Folder notes (optional)"`
}

type RevertFolderInput struct {
	WorkspaceRef
	FolderID string `json:"folderId" jsonschema:"description:The folder ID"`
	Confirm  bool   `json:"confirm" jsonschema:"description:Must be true to confirm. Discards the workspace changes to it."`
}

type UpdateFolderInput struct {
	WorkspaceRef
	FolderID string `json:"folderId" jsonschema:"description:The folder ID"`
	Name     string `json:"name,omitempty" jsonschema:"description:New folder name (optional)"`
	Notes    string `json:"notes,omitempty" jsonschema:"description:New folder notes (optional)"`
}

type DeleteFolderInput struct {
	WorkspaceRef
	FolderID string `json:"folderId" jsonschema:"description:The folder ID"`
	Confirm  bool   `json:"confirm" jsonschema:"description:Must be true to confirm. Entities inside the folder are kept and move to the root."`
}

func folderPath(ws, id string) string { return ws + "/folders/" + id }

func registerFoldersExt(server *mcp.Server) {
	registerExt(server, "create_folder", "Create an empty folder in a workspace. To create a folder and move entities into it in one step, use add_to_folder.",
		func(ctx context.Context, in CreateFolderInput) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := need("name", in.Name); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			f, err := s.Accounts.Containers.Workspaces.Folders.Create(p, &tagmanager.Folder{Name: in.Name, Notes: in.Notes}).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Folder created", f)
		})

	registerExt(server, "update_folder", "Rename a folder or change its notes.",
		func(ctx context.Context, in UpdateFolderInput) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := needID("folderId", in.FolderID); err != nil {
				return nil, err
			}
			if in.Name == "" && in.Notes == "" {
				return nil, fmt.Errorf("provide name and/or notes")
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			fp := folderPath(p, in.FolderID)
			cur, err := read(ctx, func() (*tagmanager.Folder, error) {
				return s.Accounts.Containers.Workspaces.Folders.Get(fp).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			if in.Name != "" {
				cur.Name = in.Name
			}
			if in.Notes != "" {
				cur.Notes = in.Notes
			}
			f, err := s.Accounts.Containers.Workspaces.Folders.Update(fp, cur).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Folder updated", f)
		})

	registerExt(server, "delete_folder", "Delete a folder. Its tags, triggers and variables are kept and move out of the folder. Requires confirm: true.",
		func(ctx context.Context, in DeleteFolderInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "delete_folder"); err != nil {
				return nil, err
			}
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := needID("folderId", in.FolderID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			if err := s.Accounts.Containers.Workspaces.Folders.Delete(folderPath(p, in.FolderID)).Context(ctx).Do(); err != nil {
				return nil, mapGoogleError(err)
			}
			return okMsg("Folder %s deleted", in.FolderID)
		})

	registerExt(server, "revert_folder", "Revert a folder's workspace changes back to the last saved version.",
		func(ctx context.Context, in RevertFolderInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "revert_folder"); err != nil {
				return nil, err
			}
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := needID("folderId", in.FolderID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			r, err := s.Accounts.Containers.Workspaces.Folders.Revert(folderPath(p, in.FolderID)).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Folder reverted", r)
		})
}

// ---- Zones ------------------------------------------------------------------

type ZoneIDInput struct {
	WorkspaceRef
	ZoneID string `json:"zoneId" jsonschema:"description:The zone ID"`
}

type ZoneBodyInput struct {
	WorkspaceRef
	Body map[string]any `json:"body" jsonschema:"description:Zone resource as JSON (name, boundary, childContainer, typeRestriction, notes). See the GTM API Zone reference"`
}

type UpdateZoneInput struct {
	WorkspaceRef
	ZoneID string         `json:"zoneId" jsonschema:"description:The zone ID"`
	Body   map[string]any `json:"body" jsonschema:"description:Fields to change, as a Zone resource. Merged over the current zone"`
}

type DeleteZoneInput struct {
	WorkspaceRef
	ZoneID  string `json:"zoneId" jsonschema:"description:The zone ID"`
	Confirm bool   `json:"confirm" jsonschema:"description:Must be true to confirm deletion"`
}

type RevertZoneInput struct {
	WorkspaceRef
	ZoneID  string `json:"zoneId" jsonschema:"description:The zone ID"`
	Confirm bool   `json:"confirm" jsonschema:"description:Must be true to confirm. Discards the workspace changes to it."`
}

func zonePath(ws, id string) string { return ws + "/zones/" + id }

func registerZonesExt(server *mcp.Server) {
	registerExt(server, "list_zones", "List the zones in a workspace.",
		func(ctx context.Context, in WorkspaceRef) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			var zones []*tagmanager.Zone
			_, err = read(ctx, func() (struct{}, error) {
				zones = nil
				return struct{}{}, s.Accounts.Containers.Workspaces.Zones.List(p).Context(ctx).Pages(ctx, func(r *tagmanager.ListZonesResponse) error {
					zones = append(zones, r.Zone...)
					return nil
				})
			})
			if err != nil {
				return nil, err
			}
			return okData(fmt.Sprintf("%d zones", len(zones)), map[string]any{"zones": zones})
		})

	registerExt(server, "get_zone", "Get one zone.",
		func(ctx context.Context, in ZoneIDInput) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := needID("zoneId", in.ZoneID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			z, err := read(ctx, func() (*tagmanager.Zone, error) {
				return s.Accounts.Containers.Workspaces.Zones.Get(zonePath(p, in.ZoneID)).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			return okData("Zone retrieved", z)
		})

	registerExt(server, "create_zone", "Create a zone (Tag Manager 360 feature) in a workspace.",
		func(ctx context.Context, in ZoneBodyInput) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			var z tagmanager.Zone
			if err := fromBody(in.Body, &z); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			created, err := s.Accounts.Containers.Workspaces.Zones.Create(p, &z).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Zone created", created)
		})

	registerExt(server, "update_zone", "Update a zone. Fields in body are merged over the current zone.",
		func(ctx context.Context, in UpdateZoneInput) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := needID("zoneId", in.ZoneID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			zp := zonePath(p, in.ZoneID)
			cur, err := read(ctx, func() (*tagmanager.Zone, error) {
				return s.Accounts.Containers.Workspaces.Zones.Get(zp).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			curMap, err := toMap(cur)
			if err != nil {
				return nil, err
			}
			for k, v := range in.Body {
				curMap[k] = v
			}
			var merged tagmanager.Zone
			if err := fromBody(curMap, &merged); err != nil {
				return nil, err
			}
			z, err := s.Accounts.Containers.Workspaces.Zones.Update(zp, &merged).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Zone updated", z)
		})

	registerExt(server, "delete_zone", "Delete a zone. Requires confirm: true.",
		func(ctx context.Context, in DeleteZoneInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "delete_zone"); err != nil {
				return nil, err
			}
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := needID("zoneId", in.ZoneID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			if err := s.Accounts.Containers.Workspaces.Zones.Delete(zonePath(p, in.ZoneID)).Context(ctx).Do(); err != nil {
				return nil, mapGoogleError(err)
			}
			return okMsg("Zone %s deleted", in.ZoneID)
		})

	registerExt(server, "revert_zone", "Revert a zone's workspace changes back to the last saved version.",
		func(ctx context.Context, in RevertZoneInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "revert_zone"); err != nil {
				return nil, err
			}
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := needID("zoneId", in.ZoneID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			r, err := s.Accounts.Containers.Workspaces.Zones.Revert(zonePath(p, in.ZoneID)).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Zone reverted", r)
		})
}

// ---- Google tag configs -----------------------------------------------------

type GtagConfigIDInput struct {
	WorkspaceRef
	GtagConfigID string `json:"gtagConfigId" jsonschema:"description:The Google tag config ID"`
}

type GtagConfigBodyInput struct {
	WorkspaceRef
	Body map[string]any `json:"body" jsonschema:"description:GtagConfig resource as JSON (type, parameter list). See the GTM API GtagConfig reference"`
}

type UpdateGtagConfigInput struct {
	WorkspaceRef
	GtagConfigID string         `json:"gtagConfigId" jsonschema:"description:The Google tag config ID"`
	Body         map[string]any `json:"body" jsonschema:"description:Fields to change, as a GtagConfig resource. Merged over the current config"`
}

type DeleteGtagConfigInput struct {
	WorkspaceRef
	GtagConfigID string `json:"gtagConfigId" jsonschema:"description:The Google tag config ID"`
	Confirm      bool   `json:"confirm" jsonschema:"description:Must be true to confirm deletion"`
}

func gtagPath(ws, id string) string { return ws + "/gtag_config/" + id }

func registerGtagConfigExt(server *mcp.Server) {
	registerExt(server, "list_gtag_configs", "List the Google tag (gtag) configs in a workspace.",
		func(ctx context.Context, in WorkspaceRef) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			var cfgs []*tagmanager.GtagConfig
			_, err = read(ctx, func() (struct{}, error) {
				cfgs = nil
				return struct{}{}, s.Accounts.Containers.Workspaces.GtagConfig.List(p).Context(ctx).Pages(ctx, func(r *tagmanager.ListGtagConfigResponse) error {
					cfgs = append(cfgs, r.GtagConfig...)
					return nil
				})
			})
			if err != nil {
				return nil, err
			}
			return okData(fmt.Sprintf("%d gtag configs", len(cfgs)), map[string]any{"gtagConfigs": cfgs})
		})

	registerExt(server, "get_gtag_config", "Get one Google tag config.",
		func(ctx context.Context, in GtagConfigIDInput) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := needID("gtagConfigId", in.GtagConfigID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			g, err := read(ctx, func() (*tagmanager.GtagConfig, error) {
				return s.Accounts.Containers.Workspaces.GtagConfig.Get(gtagPath(p, in.GtagConfigID)).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			return okData("Gtag config retrieved", g)
		})

	registerExt(server, "create_gtag_config", "Create a Google tag config in a workspace.",
		func(ctx context.Context, in GtagConfigBodyInput) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			var g tagmanager.GtagConfig
			if err := fromBody(in.Body, &g); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			created, err := s.Accounts.Containers.Workspaces.GtagConfig.Create(p, &g).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Gtag config created", created)
		})

	registerExt(server, "update_gtag_config", "Update a Google tag config. Fields in body are merged over the current config.",
		func(ctx context.Context, in UpdateGtagConfigInput) (*Result, error) {
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := needID("gtagConfigId", in.GtagConfigID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			gp := gtagPath(p, in.GtagConfigID)
			cur, err := read(ctx, func() (*tagmanager.GtagConfig, error) {
				return s.Accounts.Containers.Workspaces.GtagConfig.Get(gp).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			curMap, err := toMap(cur)
			if err != nil {
				return nil, err
			}
			for k, v := range in.Body {
				curMap[k] = v
			}
			var merged tagmanager.GtagConfig
			if err := fromBody(curMap, &merged); err != nil {
				return nil, err
			}
			g, err := s.Accounts.Containers.Workspaces.GtagConfig.Update(gp, &merged).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Gtag config updated", g)
		})

	registerExt(server, "delete_gtag_config", "Delete a Google tag config. Requires confirm: true.",
		func(ctx context.Context, in DeleteGtagConfigInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "delete_gtag_config"); err != nil {
				return nil, err
			}
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := needID("gtagConfigId", in.GtagConfigID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			if err := s.Accounts.Containers.Workspaces.GtagConfig.Delete(gtagPath(p, in.GtagConfigID)).Context(ctx).Do(); err != nil {
				return nil, mapGoogleError(err)
			}
			return okMsg("Gtag config %s deleted", in.GtagConfigID)
		})
}

// ---- Reverts ----------------------------------------------------------------

// RevertInput identifies the workspace entity to revert. Which ID field is read depends
// on the tool (tagId for revert_tag, and so on); the others are ignored.
type RevertTagInput struct {
	WorkspaceRef
	TagID       string `json:"tagId" jsonschema:"description:The tag ID"`
	Fingerprint string `json:"fingerprint,omitempty" jsonschema:"description:Fingerprint of the entity version to revert (optional)"`
	Confirm     bool   `json:"confirm" jsonschema:"description:Must be true to confirm. Discards the workspace changes to it."`
}
type RevertTriggerInput struct {
	WorkspaceRef
	TriggerID   string `json:"triggerId" jsonschema:"description:The trigger ID"`
	Fingerprint string `json:"fingerprint,omitempty" jsonschema:"description:Fingerprint of the entity version to revert (optional)"`
	Confirm     bool   `json:"confirm" jsonschema:"description:Must be true to confirm. Discards the workspace changes to it."`
}
type RevertVariableInput struct {
	WorkspaceRef
	VariableID  string `json:"variableId" jsonschema:"description:The variable ID"`
	Fingerprint string `json:"fingerprint,omitempty" jsonschema:"description:Fingerprint of the entity version to revert (optional)"`
	Confirm     bool   `json:"confirm" jsonschema:"description:Must be true to confirm. Discards the workspace changes to it."`
}
type RevertTemplateInput struct {
	WorkspaceRef
	TemplateID  string `json:"templateId" jsonschema:"description:The custom template ID"`
	Fingerprint string `json:"fingerprint,omitempty" jsonschema:"description:Fingerprint of the entity version to revert (optional)"`
	Confirm     bool   `json:"confirm" jsonschema:"description:Must be true to confirm. Discards the workspace changes to it."`
}
type RevertClientInput struct {
	WorkspaceRef
	ClientID    string `json:"clientId" jsonschema:"description:The client ID (server-side containers)"`
	Fingerprint string `json:"fingerprint,omitempty" jsonschema:"description:Fingerprint of the entity version to revert (optional)"`
	Confirm     bool   `json:"confirm" jsonschema:"description:Must be true to confirm. Discards the workspace changes to it."`
}
type RevertTransformationInput struct {
	WorkspaceRef
	TransformationID string `json:"transformationId" jsonschema:"description:The transformation ID (server-side containers)"`
	Fingerprint      string `json:"fingerprint,omitempty" jsonschema:"description:Fingerprint of the entity version to revert (optional)"`
	Confirm          bool   `json:"confirm" jsonschema:"description:Must be true to confirm. Discards the workspace changes to it."`
}
type RevertBuiltInVariableInput struct {
	WorkspaceRef
	Type    string `json:"type" jsonschema:"description:Built-in variable type, for example pageUrl or clickElement"`
	Confirm bool   `json:"confirm" jsonschema:"description:Must be true to confirm. Discards the workspace changes to it."`
}

func registerRevertsExt(server *mcp.Server) {
	const note = " Discards the workspace's unsaved changes to it and restores the last saved version."

	registerExt(server, "revert_tag", "Revert a tag."+note,
		func(ctx context.Context, in RevertTagInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "revert_tag"); err != nil {
				return nil, err
			}
			_, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := needID("tagId", in.TagID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			call := s.Accounts.Containers.Workspaces.Tags.Revert(BuildTagPath(in.AccountID, in.ContainerID, in.WorkspaceID, in.TagID)).Context(ctx)
			if in.Fingerprint != "" {
				call = call.Fingerprint(in.Fingerprint)
			}
			r, err := call.Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Tag reverted", r)
		})

	registerExt(server, "revert_trigger", "Revert a trigger."+note,
		func(ctx context.Context, in RevertTriggerInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "revert_trigger"); err != nil {
				return nil, err
			}
			if _, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID); err != nil {
				return nil, err
			}
			if err := needID("triggerId", in.TriggerID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			call := s.Accounts.Containers.Workspaces.Triggers.Revert(BuildTriggerPath(in.AccountID, in.ContainerID, in.WorkspaceID, in.TriggerID)).Context(ctx)
			if in.Fingerprint != "" {
				call = call.Fingerprint(in.Fingerprint)
			}
			r, err := call.Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Trigger reverted", r)
		})

	registerExt(server, "revert_variable", "Revert a variable."+note,
		func(ctx context.Context, in RevertVariableInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "revert_variable"); err != nil {
				return nil, err
			}
			if _, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID); err != nil {
				return nil, err
			}
			if err := needID("variableId", in.VariableID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			call := s.Accounts.Containers.Workspaces.Variables.Revert(BuildVariablePath(in.AccountID, in.ContainerID, in.WorkspaceID, in.VariableID)).Context(ctx)
			if in.Fingerprint != "" {
				call = call.Fingerprint(in.Fingerprint)
			}
			r, err := call.Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Variable reverted", r)
		})

	registerExt(server, "revert_template", "Revert a custom template."+note,
		func(ctx context.Context, in RevertTemplateInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "revert_template"); err != nil {
				return nil, err
			}
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := needID("templateId", in.TemplateID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			call := s.Accounts.Containers.Workspaces.Templates.Revert(p + "/templates/" + in.TemplateID).Context(ctx)
			if in.Fingerprint != "" {
				call = call.Fingerprint(in.Fingerprint)
			}
			r, err := call.Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Template reverted", r)
		})

	registerExt(server, "revert_client", "Revert a client (server-side containers)."+note,
		func(ctx context.Context, in RevertClientInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "revert_client"); err != nil {
				return nil, err
			}
			if _, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID); err != nil {
				return nil, err
			}
			if err := needID("clientId", in.ClientID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			call := s.Accounts.Containers.Workspaces.Clients.Revert(BuildClientPath(in.AccountID, in.ContainerID, in.WorkspaceID, in.ClientID)).Context(ctx)
			if in.Fingerprint != "" {
				call = call.Fingerprint(in.Fingerprint)
			}
			r, err := call.Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Client reverted", r)
		})

	registerExt(server, "revert_transformation", "Revert a transformation (server-side containers)."+note,
		func(ctx context.Context, in RevertTransformationInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "revert_transformation"); err != nil {
				return nil, err
			}
			if _, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID); err != nil {
				return nil, err
			}
			if err := needID("transformationId", in.TransformationID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			call := s.Accounts.Containers.Workspaces.Transformations.Revert(BuildTransformationPath(in.AccountID, in.ContainerID, in.WorkspaceID, in.TransformationID)).Context(ctx)
			if in.Fingerprint != "" {
				call = call.Fingerprint(in.Fingerprint)
			}
			r, err := call.Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Transformation reverted", r)
		})

	registerExt(server, "revert_built_in_variable", "Revert a built-in variable to its last saved enabled/disabled state.",
		func(ctx context.Context, in RevertBuiltInVariableInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "revert_built_in_variable"); err != nil {
				return nil, err
			}
			p, err := wsPath(in.AccountID, in.ContainerID, in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := need("type", in.Type); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			r, err := s.Accounts.Containers.Workspaces.BuiltInVariables.Revert(p).Type(in.Type).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Built-in variable reverted", r)
		})
}
