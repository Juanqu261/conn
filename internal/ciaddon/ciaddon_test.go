package ciaddon

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const testRef = "0123456789abcdef0123456789abcdef01234567"

// TestWorkflowTemplateIsReportOnlyAndPinned protects requirement 7: the
// workflow runs on pull_request only, grants contents: read and adds
// pull-requests: write only with comments, pins every action to a full
// commit SHA, never persists checkout credentials, and exposes the CI token
// only to the Conn check step.
func TestWorkflowTemplateIsReportOnlyAndPinned(t *testing.T) {
	for _, comment := range []bool{false, true} {
		content, err := Render(Options{ConnRef: testRef, Comment: comment})
		if err != nil {
			t.Fatal(err)
		}
		workflow := string(content)
		if strings.Contains(workflow, "@@") || strings.Contains(workflow, "pull_request_target") || !strings.Contains(workflow, "\n  pull_request:\n") {
			t.Fatalf("comment=%v: unexpected trigger or placeholder:\n%s", comment, workflow)
		}
		if strings.Contains(workflow, "pull-requests: write") != comment || strings.Contains(workflow, "--comment'") != comment || strings.Contains(workflow, "GITHUB_TOKEN") != comment {
			t.Fatalf("comment=%v: comment permissions do not follow the option:\n%s", comment, workflow)
		}
		uses := regexp.MustCompile(`uses: ([^\s]+)`).FindAllStringSubmatch(workflow, -1)
		if len(uses) != 5 {
			t.Fatalf("actions = %v", uses)
		}
		for _, use := range uses {
			if !regexp.MustCompile(`@[0-9a-f]{40}$`).MatchString(use[1]) {
				t.Errorf("action %s is not pinned to a full commit SHA", use[1])
			}
		}
		if strings.Count(workflow, "persist-credentials: false") != 2 || strings.Count(workflow, "actions/checkout@") != 2 {
			t.Fatal("a checkout persists credentials")
		}
		if strings.Count(workflow, "secrets.CONN_CI_TOKEN") != 1 || strings.Index(workflow, "secrets.CONN_CI_TOKEN") < strings.Index(workflow, "- name: Conn check") {
			t.Fatal("the CI token is exposed outside the Conn check step")
		}
		if !strings.Contains(workflow, "ref: "+testRef+"\n") || !strings.Contains(workflow, "contents: read") {
			t.Fatal("the workflow does not pin the Conn source revision")
		}
	}
	if _, err := Render(Options{ConnRef: "--upload-pack=x"}); err == nil {
		t.Fatal("an option-like ref was accepted")
	}
}

// TestInstallerRefusesConflictsAndRemovesOnlyConnContent protects
// requirement 6: preview writes nothing, install refuses different content,
// and removal deletes only byte-identical Conn content.
func TestInstallerRefusesConflictsAndRemovesOnlyConnContent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, TargetPath)
	options := Options{ConnRef: testRef, Comment: true}
	plan, err := Preview(root, ModeInstall, options)
	if err != nil || plan.Action != ActionCreate {
		t.Fatalf("preview = %+v, %v", plan, err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("preview wrote the workflow")
	}
	if err := Apply(plan); err != nil {
		t.Fatal(err)
	}
	if again, err := Preview(root, ModeInstall, options); err != nil || again.Action != ActionUnchanged {
		t.Fatalf("second preview = %+v, %v", again, err)
	}
	if other, err := Preview(root, ModeInstall, Options{ConnRef: "v2.0.0"}); err != nil || other.Action != ActionConflict || len(Difference(other.Existing, other.Content, 40)) == 0 {
		t.Fatalf("install over different content = %+v, %v", other, err)
	}

	// Removal recognizes the installed options and removes only Conn content.
	removal, err := Preview(root, ModeRemove, Options{})
	if err != nil || removal.Action != ActionRemove || removal.Options != options {
		t.Fatalf("removal preview = %+v, %v", removal, err)
	}
	if err := Apply(removal); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("removal kept the workflow")
	}

	// A modified workflow is preserved.
	if err := Apply(mustPreview(t, root, options)); err != nil {
		t.Fatal(err)
	}
	modified := strings.Replace(string(mustRead(t, target)), "timeout-minutes: 15", "timeout-minutes: 30", 1)
	if err := os.WriteFile(target, []byte(modified), 0o644); err != nil {
		t.Fatal(err)
	}
	if plan, err := Preview(root, ModeRemove, Options{}); err != nil || plan.Action != ActionConflict {
		t.Fatalf("removal of a modified workflow = %+v, %v", plan, err)
	}
	if string(mustRead(t, target)) != modified {
		t.Fatal("a modified workflow changed")
	}
}

func mustPreview(t *testing.T, root string, options Options) Plan {
	t.Helper()
	plan, err := Preview(root, ModeInstall, options)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
