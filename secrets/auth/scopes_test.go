package auth

import "testing"

func TestMatches_EmptyRequired(t *testing.T) {
	if !Matches(nil, nil) {
		t.Fatal("empty required should always match")
	}
}

func TestMatches_Exact(t *testing.T) {
	if !Matches([]string{"read"}, []string{"read", "write"}) {
		t.Fatal("expected exact match")
	}
}

func TestMatches_NoMatch(t *testing.T) {
	if Matches([]string{"delete"}, []string{"read", "write"}) {
		t.Fatal("expected no match")
	}
}

func TestMatches_Wildcard(t *testing.T) {
	if !Matches([]string{"task:create"}, []string{"task:*"}) {
		t.Fatal("expected wildcard match")
	}
	if !Matches([]string{"task:read"}, []string{"task:*"}) {
		t.Fatal("expected wildcard match")
	}
}

func TestMatches_WildcardNoMatch(t *testing.T) {
	if Matches([]string{"patch:approve"}, []string{"task:*"}) {
		t.Fatal("expected no wildcard match across segments")
	}
}

func TestMatches_MultipleRequired(t *testing.T) {
	granted := []string{"task:*", "secret:read"}
	if !Matches([]string{"task:create", "secret:read"}, granted) {
		t.Fatal("expected match with multiple required")
	}
	if Matches([]string{"task:create", "secret:delete"}, granted) {
		t.Fatal("expected no match for missing scope")
	}
}

func TestContainsAll(t *testing.T) {
	if !ContainsAll([]string{"a"}, []string{"a", "b", "c"}) {
		t.Fatal("expected ContainsAll true")
	}
	if ContainsAll([]string{"a", "d"}, []string{"a", "b", "c"}) {
		t.Fatal("expected ContainsAll false")
	}
}
