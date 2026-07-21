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
