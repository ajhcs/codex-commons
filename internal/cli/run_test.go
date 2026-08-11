package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func invoke(args ...string) (int, string, string) {
	var out, err bytes.Buffer
	code := Run(append([]string{"--fixture"}, args...), &out, &err)
	return code, out.String(), err.String()
}

func TestContextIsCompactAndContainsOrientationFacts(t *testing.T) {
	code, out, errOut := invoke("context", "commons-lab")
	if code != 0 || errOut != "" {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	for _, want := range []string{"PURPOSE ", "MILESTONE ", "TASK T-102", "TASK T-103", "DECISION D-7", "WIKI W-home", "SESSION S-PLUM-7", "INBOX unread=1 mentions=0 replies=1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if got := estimateTokens(out); got > 500 {
		t.Fatalf("context estimate too large: %d", got)
	}
}

func TestContextDeltaAndUnchanged(t *testing.T) {
	_, delta, _ := invoke("context", "commons-lab", "--since", "40")
	if !strings.Contains(delta, "DELTA project=commons-lab from=40 to=42 changes=2") {
		t.Fatalf("unexpected delta: %s", delta)
	}
	_, unchanged, _ := invoke("context", "commons-lab", "--since", "42")
	if strings.TrimSpace(unchanged) != "UNCHANGED project=commons-lab rev=42" {
		t.Fatalf("unexpected unchanged: %s", unchanged)
	}
}

func TestJSONIsValid(t *testing.T) {
	code, out, _ := invoke("who", "--project", "commons-lab", "--json")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["type"] != "sessions" {
		t.Fatalf("unexpected payload: %#v", decoded)
	}
}

func TestSearchFindsDecision(t *testing.T) {
	code, out, _ := invoke("search", "commons-lab", "context", "budget")
	if code != 0 || !strings.Contains(out, "P-21") {
		t.Fatalf("code=%d output=%s", code, out)
	}
}

func TestWritesAreExplicitlyNonPersistent(t *testing.T) {
	code, claim, _ := invoke("task", "claim", "T-102")
	if code != 0 || !strings.Contains(claim, "persisted=false") {
		t.Fatalf("code=%d output=%s", code, claim)
	}
	code, post, _ := invoke("post", "commons-lab", "finding", "--title", "Useful fact", "--body", "A reusable result.", "--basis", "Measured twice.")
	if code != 0 || !strings.Contains(post, "id=sim-") || !strings.Contains(post, "persisted=false") {
		t.Fatalf("code=%d output=%s", code, post)
	}
	_, repeated, _ := invoke("post", "commons-lab", "finding", "--title", "Useful fact", "--body", "A reusable result.", "--basis", "Measured twice.")
	if repeated != post {
		t.Fatalf("same simulation input changed ID: first=%q second=%q", post, repeated)
	}
	_, distinct, _ := invoke("post", "general", "topic_request", "--title", "Useful topic", "--body", "A recurring project.", "--basis", "Multiple tasks need it.")
	if distinct == post || !strings.Contains(distinct, "id=sim-") {
		t.Fatalf("distinct simulation did not get a distinct sim ID: first=%q second=%q", post, distinct)
	}
}

func TestGlobalJSONAndRetrievalCommands(t *testing.T) {
	code, out, _ := invoke("--json", "context", "commons-lab")
	if code != 0 || !json.Valid([]byte(out)) {
		t.Fatalf("global JSON failed: code=%d output=%s", code, out)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"inbox", "commons-lab"}, "MESSAGE M-3"},
		{[]string{"open", "D-7"}, "PostgreSQL remains the measured escape hatch"},
		{[]string{"next", "commons-lab"}, "TASK T-102"},
	} {
		code, out, errOut := invoke(tc.args...)
		if code != 0 || errOut != "" || !strings.Contains(out, tc.want) {
			t.Errorf("args=%v code=%d out=%q err=%q", tc.args, code, out, errOut)
		}
	}
}

func TestFutureRevisionFails(t *testing.T) {
	code, _, errOut := invoke("context", "commons-lab", "--since", "99")
	if code == 0 || !strings.Contains(errOut, "BAD_REVISION") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestBadRequestsFailClearly(t *testing.T) {
	tests := [][]string{
		{"context", "missing"},
		{"context", "commons-lab", "--budget", "12"},
		{"task", "claim", "T-101"},
		{"post", "commons-lab", "chatter", "--title", "x", "--body", "y", "--basis", "z"},
	}
	for _, args := range tests {
		code, _, errOut := invoke(args...)
		if code == 0 || !strings.HasPrefix(errOut, "ERROR code=") {
			t.Errorf("args=%v code=%d err=%q", args, code, errOut)
		}
	}
}
