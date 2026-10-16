# mcp-gtm — orientation

Fork of [paolobietolini/gtm-mcp-server](https://github.com/paolobietolini/gtm-mcp-server). Go MCP server wrapping the Google Tag Manager API. Deployed at `mcp.gtm.pathfindermarketing.com.au` (via `pmin-mcpinfrastructure`), droplet path `/opt/pmin-mcpinfrastructure/repos/mcp-gtm`.

## Auth — one scope list feeds two flows

`auth/google.go`'s `GoogleScopes` var is the **single source of truth** for every scope this server ever requests — it drives both:

- The interactive OAuth2 flow (`GoogleProvider` in `auth/google.go`)
- The service-account / Domain-Wide Delegation (DWD) flow (`NewServiceAccountTokenSource` in `auth/service_account.go`), used when `GOOGLE_SERVICE_ACCOUNT_KEY_JSON` + `impersonateSubject` are set — required for GTM, which rejects service-account emails as GTM users and only accepts DWD impersonation of a real Workspace user.

**Gotcha:** DWD requires the scope to be authorized in **two separate places**, and they're easy to get out of sync:

1. Google Workspace Admin Console → Security → API controls → Domain-wide delegation — authorizing the scope string against the service account's Client ID. **UI-only — there is no public API to read or write this list**, so it can never be verified programmatically, only by someone with Admin Console access clicking through.
2. This repo's `GoogleScopes` list — what the JWT config actually *requests* when minting a token (`service_account.go:24`).

Authorizing a scope in (1) without adding it to (2) is a silent no-op: Workspace Admin shows the scope as authorized, but the running server never asks for it, so tokens come back without that permission. Always update `GoogleScopes` and redeploy — Workspace Admin authorization alone does nothing.

Once both are in place, DWD tokens pick up the new scope automatically on the next mint (no per-user re-consent). The interactive OAuth flow is different: an existing user's refresh token was issued under the old scope set, so *that* flow does need the user to re-consent after a scope change.

## Tools requiring `tagmanager.publish`

`create_version` (`gtm/tool_version.go`) works under `tagmanager.edit.containers` alone. `publish_version` additionally requires `tagmanager.publish` — a distinct GTM API scope, not implied by `edit.containers`.

## Folders

- `add_to_folder` (`gtm/folders.go`) finds a folder by exact name (capitals matter, outer spaces trimmed), creates it if missing, then moves tags/triggers/variables in with `move_entities_to_folder`. The create tools have no `parentFolderId` input on purpose: this one tool is the only way to assign folders. It is workspace-only and never publishes.
- `get_folder_entities` does NOT call Google's `folders.entities` endpoint — it returns 404 for folders that exist. It lists the workspace's tags/triggers/variables and filters by `parentFolderId` instead. Don't "simplify" it back to the endpoint.
- The `list_tags`/`get_tag` summaries still omit `parentFolderId` (triggers include it); use `get_folder_entities` to check folder membership.

## Extended tools (`gtm/tools_ext_*.go`, added 2026-10-15)

Brings the server to full Tag Manager API v2 coverage (about 110 tools). Built with `registerExt` and the helpers in `gtm/ext_helpers.go`: each tool returns `{success, message, data}`, where `data` is the raw API object.

- **Workspaces:** `get_workspace`, `update_workspace`, `delete_workspace`, `sync_workspace`, `resolve_conflict`, `quick_preview`.
- **Folders:** `create_folder`, `update_folder`, `delete_folder`, `revert_folder` (use `add_to_folder` to create and fill in one step).
- **Reverts:** `revert_tag`, `revert_trigger`, `revert_variable`, `revert_template`, `revert_client`, `revert_transformation`, `revert_built_in_variable`.
- **Zones and gtag configs:** `*_zone` and `*_gtag_config` (list, get, create, update, delete; zones also `revert_zone`).
- **Containers:** `get_container`, `lookup_container` (by public GTM- ID), `get_container_snippet`, `move_tag_id`, `combine_containers`, plus `list_destinations`, `get_destination`, `link_destination`.
- **Environments:** `list_environments`, `get_environment`, `create_environment`, `update_environment`, `delete_environment`, `reauthorize_environment`.
- **Versions:** `get_version`, `get_live_version`, `get_latest_version_header`, `list_version_headers`, `update_version`, `delete_version`, `undelete_version`, `set_latest_version`.
- **Accounts and users:** `get_account` and `list/get/create/update/delete_user_permission`. The user tools can grant container access without the GTM UI, but only for an identity that already has admin rights on that account; they cannot lift a Read-only grant on a container the impersonated user cannot administer.

Rules for these tools:

- Anything destructive or live-affecting takes `confirm: true` (a required field, so the schema rejects the call if it is omitted): deletes, `move_tag_id`, `combine_containers`, `set_latest_version`, `reauthorize_environment`, `link_destination`, every `revert_*`, and `create_user_permission` / `update_user_permission` / `delete_user_permission`.
- Every ID that is spliced into an API path (`accountId`, `folderId`, `userPermissionId` and so on) must match `^[0-9A-Za-z_-]+$` (`needID`, and `wsPath` / `ctPath`), so a value like `1/../2` cannot redirect a call to another resource. Use `needID`, not `need`, for any new ID field.
- Body-style tools (zones, gtag configs, environments, user permissions) reject unknown fields, so a misspelt property fails loudly. `update_*` tools merge the supplied fields over the current object.
- `update_user_permission` replaces `containerAccess` wholesale: send the user's full container list.

### Opt-in scopes: `GTM_EXTRA_SCOPES`

The default scope list is unchanged. Three scopes the new tools need are opt-in through the `GTM_EXTRA_SCOPES` env var (comma-separated short names `manage.users`, `delete.containers`, `edit.containerversions`, or full `tagmanager.*` URLs): user permissions need `manage.users`, `delete_container` is documented by Google as needing `delete.containers` (not verified here), and version update/delete/undelete/set_latest need `edit.containerversions`.

**Do not set a scope here until it is authorised in Workspace Admin → Security → API controls → Domain-wide delegation.** Under DWD an unauthorised scope makes *every* token mint fail with `unauthorized_client`, so all GTM tools go down, not only the ones that need it. Order of operations: authorise in Workspace Admin first, then set `GTM_EXTRA_SCOPES` on the droplet and redeploy.

### Tests

No Go toolchain is installed on the Windows machines. Use a portable Go (zip from go.dev) or build on the droplet. `go test ./gtm/` covers tool registration, schema flattening and the confirm guards. `go test ./auth/` passes: the OAuth metadata and the 401 response both advertise `/oauth/token`, the only token route the server serves. After a redeploy run `node scripts/test-session-persistence.js <base-url> <bearer-token>` (an unknown session id must return 404).
