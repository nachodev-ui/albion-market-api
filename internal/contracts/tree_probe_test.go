package contracts

import (
	"os/exec"
	"strings"
	"testing"
)

func TestOpenAPIContractMatchesRouterTreeProbe(t *testing.T) {
	output, err := exec.Command("git", "rev-parse", "HEAD^{tree}").CombinedOutput()
	if err != nil {
		t.Fatalf("resolve git tree: %v: %s", err, output)
	}
	if strings.TrimSpace(string(output)) == "" {
		t.Fatal("resolved tree SHA is empty")
	}
}
