package mesh

import "testing"

func TestMeshConfigSubject(t *testing.T) {
	got := MeshConfigSubject("agent-123")
	want := "oap.agents.agent-123.mesh.config"
	if got != want {
		t.Errorf("MeshConfigSubject = %q, want %q", got, want)
	}
}

func TestMeshConfigResultSubject(t *testing.T) {
	got := MeshConfigResultSubject("agent-123")
	want := "oap.agents.agent-123.mesh.config.result"
	if got != want {
		t.Errorf("MeshConfigResultSubject = %q, want %q", got, want)
	}
}

func TestMeshSessionRequestSubject(t *testing.T) {
	got := MeshSessionRequestSubject()
	want := "oap.mesh.session.request"
	if got != want {
		t.Errorf("MeshSessionRequestSubject = %q, want %q", got, want)
	}
}

func TestMeshSubjectsNamespace(t *testing.T) {
	// Guard against accidental rmm.winupdate.* subjects.
	subs := []string{
		MeshConfigSubject("a"),
		MeshConfigResultSubject("a"),
		MeshSessionRequestSubject(),
	}
	for _, s := range subs {
		if len(s) >= 13 && s[:13] == "rmm.winupdate" {
			t.Errorf("subject %q uses forbidden rmm.winupdate.* namespace", s)
		}
		if len(s) < 4 || s[:4] != "oap." {
			t.Errorf("subject %q does not use oap.* namespace", s)
		}
	}
}
