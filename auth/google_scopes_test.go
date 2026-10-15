package auth

import (
	"reflect"
	"testing"
)

func TestWithExtraScopes(t *testing.T) {
	base := []string{"https://www.googleapis.com/auth/tagmanager.readonly"}
	users := "https://www.googleapis.com/auth/tagmanager.manage.users"

	if got := withExtraScopes(base, ""); !reflect.DeepEqual(got, base) {
		t.Errorf("empty env must leave the default scopes unchanged, got %v", got)
	}
	if got := withExtraScopes(base, "manage.users"); !reflect.DeepEqual(got, append(append([]string{}, base...), users)) {
		t.Errorf("short name not expanded: %v", got)
	}
	if got := withExtraScopes(base, " manage.users , "+users); len(got) != 2 {
		t.Errorf("duplicates must be dropped, got %v", got)
	}
	if got := withExtraScopes(base, "https://www.googleapis.com/auth/drive,bogus"); !reflect.DeepEqual(got, base) {
		t.Errorf("non-Tag-Manager scopes must be ignored, got %v", got)
	}
	if &base[0] == &withExtraScopes(base, "manage.users")[0] {
		t.Error("base slice must not be aliased")
	}
}
