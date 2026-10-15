package gtm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// extToolNames are the tools added by tools_ext_*.go.
var extToolNames = []string{
	"get_workspace", "update_workspace", "delete_workspace", "sync_workspace", "resolve_conflict", "quick_preview",
	"create_folder", "update_folder", "delete_folder", "revert_folder",
	"list_zones", "get_zone", "create_zone", "update_zone", "delete_zone", "revert_zone",
	"list_gtag_configs", "get_gtag_config", "create_gtag_config", "update_gtag_config", "delete_gtag_config",
	"revert_tag", "revert_trigger", "revert_variable", "revert_template", "revert_client", "revert_transformation", "revert_built_in_variable",
	"get_account", "list_user_permissions", "get_user_permission", "create_user_permission", "update_user_permission", "delete_user_permission",
	"get_container", "lookup_container", "get_container_snippet", "move_tag_id", "combine_containers",
	"list_destinations", "get_destination", "link_destination",
	"list_environments", "get_environment", "create_environment", "update_environment", "delete_environment", "reauthorize_environment",
	"get_version", "get_live_version", "get_latest_version_header", "list_version_headers", "update_version", "delete_version", "undelete_version", "set_latest_version",
}

// connect starts the real tool set behind an in-memory MCP transport.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	RegisterTools(server)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, st, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func TestExtToolsRegisteredWithFlatSchemas(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		if _, dup := byName[tool.Name]; dup {
			t.Errorf("tool %q registered twice", tool.Name)
		}
		byName[tool.Name] = tool
	}
	for _, n := range extToolNames {
		tool, ok := byName[n]
		if !ok {
			t.Errorf("tool %q not registered", n)
			continue
		}
		schema, _ := json.Marshal(tool.InputSchema)
		s := string(schema)
		// Embedded WorkspaceRef/ContainerRef must flatten into top-level properties.
		if strings.Contains(s, `"WorkspaceRef"`) || strings.Contains(s, `"ContainerRef"`) {
			t.Errorf("%s: embedded ref not flattened in schema: %s", n, s)
		}
	}
	for _, n := range []string{"get_workspace", "revert_tag", "create_zone"} {
		s, _ := json.Marshal(byName[n].InputSchema)
		for _, f := range []string{`"accountId"`, `"containerId"`, `"workspaceId"`} {
			if !strings.Contains(string(s), f) {
				t.Errorf("%s: schema missing %s: %s", n, f, s)
			}
		}
	}
}

func TestDestructiveToolsRequireConfirm(t *testing.T) {
	cs := connect(t)
	ws := map[string]any{"accountId": "1", "containerId": "2", "workspaceId": "3"}
	ct := map[string]any{"accountId": "1", "containerId": "2"}
	with := func(base map[string]any, k, v string) map[string]any {
		m := map[string]any{}
		for a, b := range base {
			m[a] = b
		}
		m[k] = v
		return m
	}
	cases := map[string]map[string]any{
		"delete_workspace":       ws,
		"delete_folder":          with(ws, "folderId", "9"),
		"delete_zone":            with(ws, "zoneId", "9"),
		"delete_gtag_config":     with(ws, "gtagConfigId", "9"),
		"delete_environment":     with(ct, "environmentId", "9"),
		"delete_version":         with(ct, "versionId", "9"),
		"set_latest_version":     with(ct, "versionId", "9"),
		"move_tag_id":            with(ct, "tagId", "G-1"),
		"combine_containers":     with(ct, "sourceContainerId", "5"),
		"delete_user_permission": {"accountId": "1", "userPermissionId": "9"},
	}
	for name, args := range cases {
		// Omitting confirm is rejected by the input schema before the handler runs.
		if _, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args}); err == nil || !strings.Contains(err.Error(), "confirm") {
			t.Errorf("%s: omitted confirm should be rejected by the schema, got %v", name, err)
		}
		// confirm:false reaches the handler's own guard.
		withFalse := map[string]any{"confirm": false}
		for k, v := range args {
			withFalse[k] = v
		}
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: withFalse})
		if err != nil {
			t.Errorf("%s: protocol error: %v", name, err)
			continue
		}
		if !res.IsError {
			t.Errorf("%s: expected an error with confirm=false", name)
			continue
		}
		text := ""
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				text += tc.Text
			}
		}
		if !strings.Contains(text, "confirm: true") {
			t.Errorf("%s: error should explain the confirm guard, got %q", name, text)
		}
	}
}

func TestFromBodyRejectsUnknownFields(t *testing.T) {
	var dst struct {
		Name string `json:"name"`
	}
	if err := fromBody(map[string]any{"name": "x"}, &dst); err != nil || dst.Name != "x" {
		t.Fatalf("valid body failed: %v %+v", err, dst)
	}
	if err := fromBody(map[string]any{"nmae": "x"}, &dst); err == nil {
		t.Error("misspelt field should be rejected")
	}
	if err := fromBody(nil, &dst); err == nil {
		t.Error("empty body should be rejected")
	}
}
