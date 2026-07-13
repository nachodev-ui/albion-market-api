package deployment

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

const (
	trivyActionCommit = "ed142fd0673e97e23eac54620cfb913e5ce36c25"
	trivyVersion      = "v0.72.0"
)

func TestContainerWorkflowBlocksFixableHighAndCriticalVulnerabilities(t *testing.T) {
	workflowPath := filepath.Join(securityWorkflowRepositoryRoot(t), ".github", "workflows", "container.yml")
	contentBytes, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read container workflow: %v", err)
	}
	content := string(contentBytes)

	required := []string{
		"workflow_dispatch:",
		"Build production image for vulnerability scan",
		"docker build \\",
		"--build-arg \"RENDER_GIT_COMMIT=${GITHUB_SHA}\"",
		"--tag \"$SCAN_IMAGE\"",
		"image-ref: ${{ env.SCAN_IMAGE }}",
		"scan-type: image",
		"scanners: vuln",
		"format: json",
		"output: trivy-results.json",
		"exit-code: '0'",
		"ignore-unfixed: true",
		"pkg-types: os,library",
		"severity: CRITICAL,HIGH",
		"version: " + trivyVersion,
		"Upload Trivy results",
		"Enforce vulnerability gate",
		"[.Results[]?.Vulnerabilities[]?] | length == 0",
		"The production image contains fixed HIGH or CRITICAL vulnerabilities.",
		"exit 1",
	}
	for _, value := range required {
		if !strings.Contains(content, value) {
			t.Errorf("container workflow must contain %q", value)
		}
	}

	if strings.Contains(content, "continue-on-error: true") {
		t.Error("vulnerability scanning must not be allowed to fail silently")
	}

	actionPattern := regexp.MustCompile(`(?m)^\s*uses:\s+aquasecurity/trivy-action@([0-9a-f]{40})(?:\s+#.*)?$`)
	matches := actionPattern.FindStringSubmatch(content)
	if len(matches) != 2 {
		t.Fatal("Trivy action must be pinned to one full 40-character commit SHA")
	}
	if matches[1] != trivyActionCommit {
		t.Fatalf("unexpected Trivy action commit: got %s, want %s", matches[1], trivyActionCommit)
	}

	buildIndex := strings.Index(content, "Build production image for vulnerability scan")
	scanIndex := strings.Index(content, "Scan production image for high and critical vulnerabilities")
	gateIndex := strings.Index(content, "Enforce vulnerability gate")
	composeIndex := strings.Index(content, "Build and test secure Compose deployment")
	if buildIndex < 0 || scanIndex < 0 || gateIndex < 0 || composeIndex < 0 ||
		!(buildIndex < scanIndex && scanIndex < gateIndex && gateIndex < composeIndex) {
		t.Error("the production image must be built, scanned, gated and only then smoke-tested with Compose")
	}
}

func securityWorkflowRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
