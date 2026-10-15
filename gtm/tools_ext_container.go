package gtm

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	tagmanager "google.golang.org/api/tagmanager/v2"
)

// ---- Accounts and users -----------------------------------------------------

type AccountRef struct {
	AccountID string `json:"accountId" jsonschema:"description:The GTM account ID"`
}

type UserPermissionIDInput struct {
	AccountID        string `json:"accountId" jsonschema:"description:The GTM account ID"`
	UserPermissionID string `json:"userPermissionId" jsonschema:"description:The user permission ID, from list_user_permissions"`
}

type UserPermissionBodyInput struct {
	AccountID string         `json:"accountId" jsonschema:"description:The GTM account ID"`
	Body      map[string]any `json:"body" jsonschema:"description:UserPermission as JSON: emailAddress, accountAccess {permission: noAccess|user|admin}, containerAccess [{containerId, permission: noAccess|read|edit|approve|publish}]"`
}

type UpdateUserPermissionInput struct {
	AccountID        string         `json:"accountId" jsonschema:"description:The GTM account ID"`
	UserPermissionID string         `json:"userPermissionId" jsonschema:"description:The user permission ID, from list_user_permissions"`
	Body             map[string]any `json:"body" jsonschema:"description:Fields to change: accountAccess and/or containerAccess. containerAccess REPLACES the user's container list, so send the full list"`
}

type DeleteUserPermissionInput struct {
	AccountID        string `json:"accountId" jsonschema:"description:The GTM account ID"`
	UserPermissionID string `json:"userPermissionId" jsonschema:"description:The user permission ID"`
	Confirm          bool   `json:"confirm" jsonschema:"description:Must be true to confirm. Removes the user's access to the account and all its containers."`
}

func registerAccountsExt(server *mcp.Server) {
	const scopeNote = " Needs the tagmanager.manage.users scope (enable it with GTM_EXTRA_SCOPES and authorise it in Workspace Admin domain-wide delegation)."

	registerExt(server, "get_account", "Get one GTM account's details.",
		func(ctx context.Context, in AccountRef) (*Result, error) {
			if err := need("accountId", in.AccountID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			a, err := read(ctx, func() (*tagmanager.Account, error) {
				return s.Accounts.Get("accounts/" + in.AccountID).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			return okData("Account retrieved", a)
		})

	registerExt(server, "list_user_permissions", "List who has access to a GTM account and to which containers, with their permission levels (account: noAccess/user/admin; container: noAccess/read/edit/approve/publish)."+scopeNote,
		func(ctx context.Context, in AccountRef) (*Result, error) {
			if err := need("accountId", in.AccountID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			var perms []*tagmanager.UserPermission
			_, err = read(ctx, func() (struct{}, error) {
				perms = nil
				return struct{}{}, s.Accounts.UserPermissions.List("accounts/"+in.AccountID).Context(ctx).Pages(ctx, func(r *tagmanager.ListUserPermissionsResponse) error {
					perms = append(perms, r.UserPermission...)
					return nil
				})
			})
			if err != nil {
				return nil, err
			}
			return okData(fmt.Sprintf("%d users", len(perms)), map[string]any{"userPermissions": perms})
		})

	registerExt(server, "get_user_permission", "Get one user's permissions on an account."+scopeNote,
		func(ctx context.Context, in UserPermissionIDInput) (*Result, error) {
			if err := need("accountId", in.AccountID, "userPermissionId", in.UserPermissionID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			u, err := read(ctx, func() (*tagmanager.UserPermission, error) {
				return s.Accounts.UserPermissions.Get("accounts/" + in.AccountID + "/user_permissions/" + in.UserPermissionID).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			return okData("User permission retrieved", u)
		})

	registerExt(server, "create_user_permission", "Grant a user access to a GTM account and/or containers. The user must have a Google account."+scopeNote,
		func(ctx context.Context, in UserPermissionBodyInput) (*Result, error) {
			if err := need("accountId", in.AccountID); err != nil {
				return nil, err
			}
			var u tagmanager.UserPermission
			if err := fromBody(in.Body, &u); err != nil {
				return nil, err
			}
			if u.EmailAddress == "" {
				return nil, fmt.Errorf("body.emailAddress is required")
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			created, err := s.Accounts.UserPermissions.Create("accounts/"+in.AccountID, &u).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("User permission created", created)
		})

	registerExt(server, "update_user_permission", "Change a user's account or container permissions. containerAccess replaces the existing container list."+scopeNote,
		func(ctx context.Context, in UpdateUserPermissionInput) (*Result, error) {
			if err := need("accountId", in.AccountID, "userPermissionId", in.UserPermissionID); err != nil {
				return nil, err
			}
			var change tagmanager.UserPermission
			if err := fromBody(in.Body, &change); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			path := "accounts/" + in.AccountID + "/user_permissions/" + in.UserPermissionID
			cur, err := read(ctx, func() (*tagmanager.UserPermission, error) {
				return s.Accounts.UserPermissions.Get(path).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			if _, ok := in.Body["accountAccess"]; ok {
				cur.AccountAccess = change.AccountAccess
			}
			if _, ok := in.Body["containerAccess"]; ok {
				cur.ContainerAccess = change.ContainerAccess
			}
			u, err := s.Accounts.UserPermissions.Update(path, cur).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("User permission updated", u)
		})

	registerExt(server, "delete_user_permission", "Remove a user's access to a GTM account. Requires confirm: true."+scopeNote,
		func(ctx context.Context, in DeleteUserPermissionInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "delete_user_permission"); err != nil {
				return nil, err
			}
			if err := need("accountId", in.AccountID, "userPermissionId", in.UserPermissionID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			if err := s.Accounts.UserPermissions.Delete("accounts/" + in.AccountID + "/user_permissions/" + in.UserPermissionID).Context(ctx).Do(); err != nil {
				return nil, mapGoogleError(err)
			}
			return okMsg("User permission %s removed", in.UserPermissionID)
		})
}

// ---- Containers -------------------------------------------------------------

type LookupContainerInput struct {
	DestinationID string `json:"destinationId" jsonschema:"description:The container's public ID (GTM-XXXXXXX) or other destination ID to look up"`
}

type ContainerSnippetInput struct {
	ContainerRef
}

type MoveTagIDInput struct {
	ContainerRef
	TagID                           string `json:"tagId" jsonschema:"description:The tag ID (for example a G- or AW- ID) to move out of this container"`
	TagName                         string `json:"tagName,omitempty" jsonschema:"description:Name for the new tag in the destination (optional)"`
	CopySettings                    bool   `json:"copySettings,omitempty" jsonschema:"description:Copy the tag settings along"`
	CopyUsers                       bool   `json:"copyUsers,omitempty" jsonschema:"description:Copy user permissions along"`
	CopyTermsOfService              bool   `json:"copyTermsOfService,omitempty" jsonschema:"description:Copy the accepted terms of service"`
	AllowUserPermissionFeatureUpdate bool  `json:"allowUserPermissionFeatureUpdate,omitempty" jsonschema:"description:Allow user permission feature updates"`
	Confirm                         bool   `json:"confirm" jsonschema:"description:Must be true to confirm. Moves a Google tag ID out of the container."`
}

type CombineContainersInput struct {
	ContainerRef
	SourceContainerID string `json:"sourceContainerId" jsonschema:"description:The container to merge INTO this one (the source is combined into the target)"`
	SettingSource     string `json:"settingSource,omitempty" jsonschema:"description:Which container's settings win: OK, CURRENT or BACKUP"`
	AllowUserPermissionFeatureUpdate bool `json:"allowUserPermissionFeatureUpdate,omitempty" jsonschema:"description:Allow user permission feature updates"`
	Confirm           bool   `json:"confirm" jsonschema:"description:Must be true to confirm. Merging containers is hard to undo."`
}

type DestinationIDInput struct {
	ContainerRef
	DestinationLinkID string `json:"destinationLinkId" jsonschema:"description:The destination link ID, from list_destinations"`
}

type LinkDestinationInput struct {
	ContainerRef
	DestinationID                    string `json:"destinationId" jsonschema:"description:The destination ID to link (for example a G- or AW- ID)"`
	AllowUserPermissionFeatureUpdate bool   `json:"allowUserPermissionFeatureUpdate,omitempty" jsonschema:"description:Allow user permission feature updates"`
}

func registerContainersExt(server *mcp.Server) {
	registerExt(server, "get_container", "Get one container's details (public ID, usage context, domain names, tagging server URLs, fingerprint).",
		func(ctx context.Context, in ContainerRef) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			c, err := read(ctx, func() (*tagmanager.Container, error) {
				return s.Accounts.Containers.Get(p).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			return okData("Container retrieved", c)
		})

	registerExt(server, "lookup_container", "Find a container by its public ID (GTM-XXXXXXX) without knowing the account. Returns the account and container IDs.",
		func(ctx context.Context, in LookupContainerInput) (*Result, error) {
			if err := need("destinationId", in.DestinationID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			c, err := read(ctx, func() (*tagmanager.Container, error) {
				return s.Accounts.Containers.Lookup().DestinationId(in.DestinationID).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			return okData("Container found", c)
		})

	registerExt(server, "get_container_snippet", "Get the install snippet (the code to paste on the site) for a container.",
		func(ctx context.Context, in ContainerSnippetInput) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			r, err := read(ctx, func() (*tagmanager.GetContainerSnippetResponse, error) {
				return s.Accounts.Containers.Snippet(p).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			return okData("Snippet retrieved", r)
		})

	registerExt(server, "move_tag_id", "Move a Google tag ID out of this container into its own new container. Requires confirm: true.",
		func(ctx context.Context, in MoveTagIDInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "move_tag_id"); err != nil {
				return nil, err
			}
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			if err := need("tagId", in.TagID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			call := s.Accounts.Containers.MoveTagId(p).TagId(in.TagID).Context(ctx)
			if in.TagName != "" {
				call = call.TagName(in.TagName)
			}
			call = call.CopySettings(in.CopySettings).CopyUsers(in.CopyUsers).CopyTermsOfService(in.CopyTermsOfService).
				AllowUserPermissionFeatureUpdate(in.AllowUserPermissionFeatureUpdate)
			c, err := call.Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Tag ID moved", c)
		})

	registerExt(server, "combine_containers", "Merge a source container into this container. Requires confirm: true. Hard to undo; check both containers first.",
		func(ctx context.Context, in CombineContainersInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "combine_containers"); err != nil {
				return nil, err
			}
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			if err := need("sourceContainerId", in.SourceContainerID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			call := s.Accounts.Containers.Combine(p).ContainerId(in.SourceContainerID).
				AllowUserPermissionFeatureUpdate(in.AllowUserPermissionFeatureUpdate).Context(ctx)
			if in.SettingSource != "" {
				call = call.SettingSource(in.SettingSource)
			}
			c, err := call.Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Containers combined", c)
		})

	registerExt(server, "list_destinations", "List the destinations (Google tag IDs such as GA4 or Ads) linked to a container.",
		func(ctx context.Context, in ContainerRef) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			r, err := read(ctx, func() (*tagmanager.ListDestinationsResponse, error) {
				return s.Accounts.Containers.Destinations.List(p).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			return okData(fmt.Sprintf("%d destinations", len(r.Destination)), map[string]any{"destinations": r.Destination})
		})

	registerExt(server, "get_destination", "Get one linked destination.",
		func(ctx context.Context, in DestinationIDInput) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			if err := need("destinationLinkId", in.DestinationLinkID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			d, err := read(ctx, func() (*tagmanager.Destination, error) {
				return s.Accounts.Containers.Destinations.Get(p + "/destinations/" + in.DestinationLinkID).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			return okData("Destination retrieved", d)
		})

	registerExt(server, "link_destination", "Link a destination (a Google tag ID such as G-XXXX or AW-XXXX) to a container.",
		func(ctx context.Context, in LinkDestinationInput) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			if err := need("destinationId", in.DestinationID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			d, err := s.Accounts.Containers.Destinations.Link(p).DestinationId(in.DestinationID).
				AllowUserPermissionFeatureUpdate(in.AllowUserPermissionFeatureUpdate).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Destination linked", d)
		})
}

// ---- Environments -----------------------------------------------------------

type EnvironmentIDInput struct {
	ContainerRef
	EnvironmentID string `json:"environmentId" jsonschema:"description:The environment ID"`
}

type EnvironmentBodyInput struct {
	ContainerRef
	Body map[string]any `json:"body" jsonschema:"description:Environment resource as JSON: name, type (user), description, url, enableDebug. See the GTM API Environment reference"`
}

type UpdateEnvironmentInput struct {
	ContainerRef
	EnvironmentID string         `json:"environmentId" jsonschema:"description:The environment ID"`
	Body          map[string]any `json:"body" jsonschema:"description:Fields to change, as an Environment resource. Merged over the current environment"`
}

type DeleteEnvironmentInput struct {
	ContainerRef
	EnvironmentID string `json:"environmentId" jsonschema:"description:The environment ID"`
	Confirm       bool   `json:"confirm" jsonschema:"description:Must be true to confirm deletion"`
}

func envPath(container, id string) string { return container + "/environments/" + id }

func registerEnvironmentsExt(server *mcp.Server) {
	registerExt(server, "list_environments", "List a container's environments (Live, Latest and any custom environments).",
		func(ctx context.Context, in ContainerRef) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			var envs []*tagmanager.Environment
			_, err = read(ctx, func() (struct{}, error) {
				envs = nil
				return struct{}{}, s.Accounts.Containers.Environments.List(p).Context(ctx).Pages(ctx, func(r *tagmanager.ListEnvironmentsResponse) error {
					envs = append(envs, r.Environment...)
					return nil
				})
			})
			if err != nil {
				return nil, err
			}
			return okData(fmt.Sprintf("%d environments", len(envs)), map[string]any{"environments": envs})
		})

	registerExt(server, "get_environment", "Get one environment, including its authorization code.",
		func(ctx context.Context, in EnvironmentIDInput) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			if err := need("environmentId", in.EnvironmentID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			e, err := read(ctx, func() (*tagmanager.Environment, error) {
				return s.Accounts.Containers.Environments.Get(envPath(p, in.EnvironmentID)).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			return okData("Environment retrieved", e)
		})

	registerExt(server, "create_environment", "Create a custom environment (for staging or QA) on a container.",
		func(ctx context.Context, in EnvironmentBodyInput) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			var e tagmanager.Environment
			if err := fromBody(in.Body, &e); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			created, err := s.Accounts.Containers.Environments.Create(p, &e).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Environment created", created)
		})

	registerExt(server, "update_environment", "Update a custom environment. Fields in body are merged over the current environment.",
		func(ctx context.Context, in UpdateEnvironmentInput) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			if err := need("environmentId", in.EnvironmentID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			ep := envPath(p, in.EnvironmentID)
			cur, err := read(ctx, func() (*tagmanager.Environment, error) {
				return s.Accounts.Containers.Environments.Get(ep).Context(ctx).Do()
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
			var merged tagmanager.Environment
			if err := fromBody(curMap, &merged); err != nil {
				return nil, err
			}
			e, err := s.Accounts.Containers.Environments.Update(ep, &merged).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Environment updated", e)
		})

	registerExt(server, "delete_environment", "Delete a custom environment. Requires confirm: true.",
		func(ctx context.Context, in DeleteEnvironmentInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "delete_environment"); err != nil {
				return nil, err
			}
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			if err := need("environmentId", in.EnvironmentID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			if err := s.Accounts.Containers.Environments.Delete(envPath(p, in.EnvironmentID)).Context(ctx).Do(); err != nil {
				return nil, mapGoogleError(err)
			}
			return okMsg("Environment %s deleted", in.EnvironmentID)
		})

	registerExt(server, "reauthorize_environment", "Regenerate an environment's authorization code. The old code stops working.",
		func(ctx context.Context, in EnvironmentIDInput) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			if err := need("environmentId", in.EnvironmentID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			e, err := s.Accounts.Containers.Environments.Reauthorize(envPath(p, in.EnvironmentID), &tagmanager.Environment{}).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Environment reauthorized", e)
		})
}

// ---- Versions ---------------------------------------------------------------

type VersionIDInput struct {
	ContainerRef
	VersionID string `json:"versionId" jsonschema:"description:The container version ID"`
}

type ListVersionHeadersInput struct {
	ContainerRef
	IncludeDeleted bool `json:"includeDeleted,omitempty" jsonschema:"description:Include deleted versions"`
}

type UpdateVersionInput struct {
	ContainerRef
	VersionID   string `json:"versionId" jsonschema:"description:The container version ID"`
	Name        string `json:"name,omitempty" jsonschema:"description:New version name (optional)"`
	Description string `json:"description,omitempty" jsonschema:"description:New version description (optional)"`
}

type DeleteVersionInput struct {
	ContainerRef
	VersionID string `json:"versionId" jsonschema:"description:The container version ID"`
	Confirm   bool   `json:"confirm" jsonschema:"description:Must be true to confirm. A deleted version can be restored with undelete_version."`
}

type SetLatestVersionInput struct {
	ContainerRef
	VersionID string `json:"versionId" jsonschema:"description:The container version ID to mark as the latest"`
	Confirm   bool   `json:"confirm" jsonschema:"description:Must be true to confirm. Changes what the Latest environment serves."`
}

func versionPath(container, id string) string { return container + "/versions/" + id }

func registerVersionsExt(server *mcp.Server) {
	registerExt(server, "get_version", "Get one container version in full (all tags, triggers, variables, folders and templates it contains).",
		func(ctx context.Context, in VersionIDInput) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			if err := need("versionId", in.VersionID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			v, err := read(ctx, func() (*tagmanager.ContainerVersion, error) {
				return s.Accounts.Containers.Versions.Get(versionPath(p, in.VersionID)).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			return okData("Version retrieved", v)
		})

	registerExt(server, "get_live_version", "Get the container version that is currently published (live) in full.",
		func(ctx context.Context, in ContainerRef) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			v, err := read(ctx, func() (*tagmanager.ContainerVersion, error) {
				return s.Accounts.Containers.Versions.Live(p).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			return okData("Live version retrieved", v)
		})

	registerExt(server, "get_latest_version_header", "Get the header (ID, name, counts) of the most recently created container version.",
		func(ctx context.Context, in ContainerRef) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			h, err := read(ctx, func() (*tagmanager.ContainerVersionHeader, error) {
				return s.Accounts.Containers.VersionHeaders.Latest(p).Context(ctx).Do()
			})
			if err != nil {
				return nil, err
			}
			return okData("Latest version header retrieved", h)
		})

	registerExt(server, "list_version_headers", "List version headers (ID, name, entity counts, deleted flag) for a container, optionally including deleted versions.",
		func(ctx context.Context, in ListVersionHeadersInput) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			var hs []*tagmanager.ContainerVersionHeader
			_, err = read(ctx, func() (struct{}, error) {
				hs = nil
				return struct{}{}, s.Accounts.Containers.VersionHeaders.List(p).IncludeDeleted(in.IncludeDeleted).Context(ctx).Pages(ctx, func(r *tagmanager.ListContainerVersionsResponse) error {
					hs = append(hs, r.ContainerVersionHeader...)
					return nil
				})
			})
			if err != nil {
				return nil, err
			}
			return okData(fmt.Sprintf("%d versions", len(hs)), map[string]any{"versionHeaders": hs})
		})

	registerExt(server, "update_version", "Rename a container version or change its description.",
		func(ctx context.Context, in UpdateVersionInput) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			if err := need("versionId", in.VersionID); err != nil {
				return nil, err
			}
			if in.Name == "" && in.Description == "" {
				return nil, fmt.Errorf("provide name and/or description")
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			vp := versionPath(p, in.VersionID)
			cur, err := read(ctx, func() (*tagmanager.ContainerVersion, error) {
				return s.Accounts.Containers.Versions.Get(vp).Context(ctx).Do()
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
			v, err := s.Accounts.Containers.Versions.Update(vp, cur).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Version updated", v)
		})

	registerExt(server, "delete_version", "Delete a container version. Requires confirm: true. Restore with undelete_version. The live version cannot be deleted.",
		func(ctx context.Context, in DeleteVersionInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "delete_version"); err != nil {
				return nil, err
			}
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			if err := need("versionId", in.VersionID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			if err := s.Accounts.Containers.Versions.Delete(versionPath(p, in.VersionID)).Context(ctx).Do(); err != nil {
				return nil, mapGoogleError(err)
			}
			return okMsg("Version %s deleted", in.VersionID)
		})

	registerExt(server, "undelete_version", "Restore a deleted container version.",
		func(ctx context.Context, in VersionIDInput) (*Result, error) {
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			if err := need("versionId", in.VersionID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			v, err := s.Accounts.Containers.Versions.Undelete(versionPath(p, in.VersionID)).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Version restored", v)
		})

	registerExt(server, "set_latest_version", "Mark a version as the container's Latest version (what the Latest environment serves). Does not publish it. Requires confirm: true.",
		func(ctx context.Context, in SetLatestVersionInput) (*Result, error) {
			if err := needConfirm(in.Confirm, "set_latest_version"); err != nil {
				return nil, err
			}
			p, err := ctPath(in.AccountID, in.ContainerID)
			if err != nil {
				return nil, err
			}
			if err := need("versionId", in.VersionID); err != nil {
				return nil, err
			}
			s, err := svc(ctx)
			if err != nil {
				return nil, err
			}
			v, err := s.Accounts.Containers.Versions.SetLatest(versionPath(p, in.VersionID)).Context(ctx).Do()
			if err != nil {
				return nil, mapGoogleError(err)
			}
			return okData("Latest version set", v)
		})
}
