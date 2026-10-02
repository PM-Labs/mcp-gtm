package gtm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"google.golang.org/api/option"
	tagmanager "google.golang.org/api/tagmanager/v2"
)

const testWS = "accounts/1/containers/2/workspaces/3"

// fakeGTM is a minimal in-memory stand-in for the GTM folder, tag, trigger and variable endpoints.
type fakeGTM struct {
	mu       sync.Mutex
	folders  []*tagmanager.Folder
	nextID   int
	parent   map[string]string // "tag:5" -> folderId
	names    map[string]string // "tag:5" -> name
	entCalls int               // calls to the broken :entities endpoint
	createN  int
	failMove bool
	pageSize int
}

func newFake() *fakeGTM {
	return &fakeGTM{nextID: 100, parent: map[string]string{}, names: map[string]string{
		"tag:5": "ZZZ Tag", "trigger:6": "ZZZ Trigger", "variable:7": "ZZZ Variable",
	}}
}

func (f *fakeGTM) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		p := strings.TrimPrefix(r.URL.Path, "/tagmanager/v2/")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(p, ":entities"):
			f.entCalls++
			http.Error(w, `{"error":{"code":404,"message":"Not found or permission denied."}}`, 404)
		case strings.HasSuffix(p, ":move_entities_to_folder"):
			if f.failMove {
				http.Error(w, `{"error":{"code":404,"message":"entity missing"}}`, 404)
				return
			}
			fid := strings.TrimSuffix(p[strings.LastIndex(p, "/")+1:], ":move_entities_to_folder")
			q := r.URL.Query()
			for kind, key := range map[string]string{"tag": "tagId", "trigger": "triggerId", "variable": "variableId"} {
				for _, id := range q[key] {
					f.parent[kind+":"+id] = fid
				}
			}
			w.Write([]byte(`{}`))
		case strings.HasSuffix(p, "/folders") && r.Method == http.MethodPost:
			var in tagmanager.Folder
			json.NewDecoder(r.Body).Decode(&in)
			f.nextID++
			f.createN++
			id := itoa(f.nextID)
			nf := &tagmanager.Folder{FolderId: id, Name: in.Name, Path: testWS + "/folders/" + id}
			f.folders = append(f.folders, nf)
			json.NewEncoder(w).Encode(nf)
		case strings.HasSuffix(p, "/folders"):
			json.NewEncoder(w).Encode(tagmanager.ListFoldersResponse{Folder: f.folders})
		case strings.HasSuffix(p, "/tags"):
			var out []*tagmanager.Tag
			for k, n := range f.names {
				if strings.HasPrefix(k, "tag:") {
					out = append(out, &tagmanager.Tag{TagId: k[4:], Name: n, ParentFolderId: f.parent[k]})
				}
			}
			json.NewEncoder(w).Encode(tagmanager.ListTagsResponse{Tag: out})
		case strings.HasSuffix(p, "/triggers"):
			var out []*tagmanager.Trigger
			for k, n := range f.names {
				if strings.HasPrefix(k, "trigger:") {
					out = append(out, &tagmanager.Trigger{TriggerId: k[8:], Name: n, ParentFolderId: f.parent[k]})
				}
			}
			json.NewEncoder(w).Encode(tagmanager.ListTriggersResponse{Trigger: out})
		case strings.HasSuffix(p, "/variables"):
			var out []*tagmanager.Variable
			for k, n := range f.names {
				if strings.HasPrefix(k, "variable:") {
					out = append(out, &tagmanager.Variable{VariableId: k[9:], Name: n, ParentFolderId: f.parent[k]})
				}
			}
			json.NewEncoder(w).Encode(tagmanager.ListVariablesResponse{Variable: out})
		default:
			http.Error(w, "unexpected "+r.Method+" "+p, 500)
		}
	})
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func testClient(t *testing.T, f *fakeGTM) *Client {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	svc, err := tagmanager.NewService(context.Background(), option.WithEndpoint(srv.URL), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	return &Client{Service: svc}
}

func add(c *Client, name string, tags, trig, vars []string) (*AddToFolderResult, error) {
	return c.AddToFolder(context.Background(), "1", "2", "3", name, tags, trig, vars)
}

// A1 + G1: folder created, all three kinds land inside it, get_folder_entities sees them.
func TestAddToFolder_CreatesFolderAndMoves(t *testing.T) {
	f := newFake()
	c := testClient(t, f)
	res, err := add(c, "ZZZ Folder", []string{"5"}, []string{"6"}, []string{"7"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.FolderCreated || res.TagsMoved != 1 || res.TriggersMoved != 1 || res.VariablesMoved != 1 {
		t.Fatalf("unexpected result %+v", res)
	}
	ent, err := c.GetFolderEntities(context.Background(), "1", "2", "3", res.FolderID)
	if err != nil {
		t.Fatal(err)
	}
	want := &FolderEntities{Tags: []string{"ZZZ Tag"}, Triggers: []string{"ZZZ Trigger"}, Variables: []string{"ZZZ Variable"}}
	if !reflect.DeepEqual(ent, want) {
		t.Fatalf("entities %+v, want %+v", ent, want)
	}
	if f.entCalls != 0 {
		t.Fatal("must not call the broken :entities endpoint")
	}
}

// A2: repeat call reuses the folder.
func TestAddToFolder_RepeatIsIdempotent(t *testing.T) {
	f := newFake()
	c := testClient(t, f)
	first, _ := add(c, "ZZZ Folder", []string{"5"}, nil, nil)
	second, err := add(c, "ZZZ Folder", []string{"5"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if second.FolderCreated || second.FolderID != first.FolderID || f.createN != 1 {
		t.Fatalf("second=%+v creates=%d", second, f.createN)
	}
}

// A3: capitals matter; outer spaces don't.
func TestAddToFolder_NameMatching(t *testing.T) {
	f := newFake()
	c := testClient(t, f)
	a, _ := add(c, "ZZZ Folder", []string{"5"}, nil, nil)
	same, _ := add(c, "  ZZZ Folder  ", []string{"5"}, nil, nil)
	other, _ := add(c, "zzz folder", []string{"5"}, nil, nil)
	if same.FolderID != a.FolderID || same.FolderCreated {
		t.Fatalf("outer spaces should match: %+v", same)
	}
	if other.FolderID == a.FolderID || !other.FolderCreated {
		t.Fatalf("different capitals should create a new folder: %+v", other)
	}
}

// A4: an item lives in one folder.
func TestAddToFolder_MovesBetweenFolders(t *testing.T) {
	f := newFake()
	c := testClient(t, f)
	a, _ := add(c, "A", []string{"5"}, nil, nil)
	b, _ := add(c, "B", []string{"5"}, nil, nil)
	inA, _ := c.GetFolderEntities(context.Background(), "1", "2", "3", a.FolderID)
	inB, _ := c.GetFolderEntities(context.Background(), "1", "2", "3", b.FolderID)
	if len(inA.Tags) != 0 || len(inB.Tags) != 1 {
		t.Fatalf("A=%+v B=%+v", inA, inB)
	}
}

// A5 + A6: bad input creates nothing.
func TestAddToFolder_ValidationCreatesNothing(t *testing.T) {
	f := newFake()
	c := testClient(t, f)
	if _, err := add(c, "ZZZ Folder", nil, nil, nil); err == nil || !strings.Contains(err.Error(), "nothing to move") {
		t.Fatalf("expected nothing-to-move error, got %v", err)
	}
	if _, err := add(c, "   ", []string{"5"}, nil, nil); err == nil || !strings.Contains(err.Error(), "folder name is required") {
		t.Fatalf("expected name error, got %v", err)
	}
	if _, err := add(c, "ZZZ Folder", []string{" "}, nil, nil); err == nil {
		t.Fatal("expected blank-ID error")
	}
	if f.createN != 0 {
		t.Fatalf("a rejected call created %d folders", f.createN)
	}
}

// A7: Google rejects the move after the folder was created: say so.
func TestAddToFolder_MoveFailureReportsLeftoverFolder(t *testing.T) {
	f := newFake()
	f.failMove = true
	c := testClient(t, f)
	_, err := add(c, "ZZZ Folder", []string{"999"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "was created by this call and has been left in place") {
		t.Fatalf("expected leftover-folder note, got %v", err)
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected wrapped ErrNotFound, got %v", err)
	}
}

// G2 + G3.
func TestGetFolderEntities_EmptyAndMissing(t *testing.T) {
	f := newFake()
	c := testClient(t, f)
	f.folders = append(f.folders, &tagmanager.Folder{FolderId: "50", Name: "Empty", Path: testWS + "/folders/50"})
	ent, err := c.GetFolderEntities(context.Background(), "1", "2", "3", "50")
	if err != nil {
		t.Fatal(err)
	}
	if len(ent.Tags)+len(ent.Triggers)+len(ent.Variables) != 0 {
		t.Fatalf("expected empty, got %+v", ent)
	}
	if _, err := c.GetFolderEntities(context.Background(), "1", "2", "3", "404"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
