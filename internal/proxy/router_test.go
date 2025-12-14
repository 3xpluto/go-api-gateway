package proxy

import "testing"

func TestMatchLongestPrefix(t *testing.T) {
	r, err := New([]Route{
		{Name: "a", PathPrefix: "/api/"},
		{Name: "b", PathPrefix: "/api/users/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := r.Match("/api/users/me")
	if m == nil || m.Name != "b" {
		t.Fatalf("expected longest prefix route b, got %#v", m)
	}
}

func TestStripPath(t *testing.T) {
	got := StripPath("/api/users/me", "/api")
	if got != "/users/me" {
		t.Fatalf("expected /users/me, got %q", got)
	}
}
