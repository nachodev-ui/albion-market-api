package contracts

import (
	"os/exec"
	"strings"
	"testing"
)

func TestTreeProbe(t *testing.T) {
	output, err := exec.Command("git", "rev-parse", "HEAD^{tree}").CombinedOutput()
	if err != nil {
		t.Fatalf("resolve git tree: %v: %s", err, output)
	}
	t.Fatalf("TREE_SHA=%s", strings.TrimSpace(string(output)))
}
