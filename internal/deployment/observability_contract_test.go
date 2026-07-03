package deployment

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

const (
	prometheusImage   = "prom/prometheus@sha256:a75c5a35bc21d7afe69551eefa3cb1e1fb1775fe759408007a66b54ec3de1f29"
	alertmanagerImage = "prom/alertmanager@sha256:cc54cc450174ada901b32eb2538de5fc70ee259a1ac551ed38023f2ca2ad00e3"
	grafanaImage      = "grafana/grafana:12.4.4-ubuntu@sha256:df2e7ef5f32f771794cf76bad5f2bceac227036460a2cc269a9045e5662abc58"
)

func TestObservabilityServicesAreOptionalAndDigestPinned(t *testing.T) {
	compose := readProjectFile(t, "deploy", "compose.yaml")
	for service, image := range map[string]string{
		"prometheus":   prometheusImage,
		"alertmanager": alertmanagerImage,
		"grafana":      grafanaImage,
	} {
		block := composeServiceBlock(t, compose, service)
		requireContains(t, block, "profiles:\n      - observability")
		requireContains(t, block, "image: "+image)
		requireContains(t, block, "read_only: true")
		requireContains(t, block, "cap_drop:\n      - ALL")
		requireContains(t, block, "no-new-privileges:true")
		if strings.Contains(strings.ToLower(block), ":latest") {
			t.Fatalf("service %s must not use a mutable latest tag", service)
		}
	}
}

func TestObservabilityPortsAreBoundToLoopback(t *testing.T) {
	compose := readProjectFile(t, "deploy", "compose.yaml")
	for service, expected := range map[string]string{
		"prometheus":   `127.0.0.1:${PROMETHEUS_HOST_PORT:-9090}:9090`,
		"alertmanager": `127.0.0.1:${ALERTMANAGER_HOST_PORT:-9093}:9093`,
		"grafana":      `127.0.0.1:${GRAFANA_HOST_PORT:-3000}:3000`,
	} {
		block := composeServiceBlock(t, compose, service)
		requireContains(t, block, expected)
		requireContains(t, block, "networks:\n      - observability\n      - observability_host")
	}

	requireContains(t, compose, "  observability:\n    internal: true")
	requireContains(t, compose, "  observability_host:\n")
}

func TestPrometheusScrapesAPIAndRoutesAlerts(t *testing.T) {
	config := readProjectFile(t, "observability", "prometheus", "prometheus.yml")
	for _, expected := range []string{
		"rule_files:",
		"/etc/prometheus/rules/*.yml",
		"alertmanagers:",
		"alertmanager:9093",
		"job_name: albion-market-api",
		"api:8080",
	} {
		requireContains(t, config, expected)
	}
}

func TestAlertRulesCoverStageSixOperations(t *testing.T) {
	rules := readProjectFile(t, "observability", "prometheus", "rules", "albion-market-api.rules.yml")
	for _, alert := range []string{
		"AlbionMarketAPIUnavailable",
		"AlbionMarketAPINotReady",
		"AlbionMarketAPIHighHTTP5xxRate",
		"AlbionMarketAPIHighHTTPLatency",
		"AlbionMarketAPIAuthenticationFailuresHigh",
		"AlbionMarketAPIIngestTrafficStopped",
		"AlbionMarketAPINoSuccessfulIngest",
		"AlbionMarketAPIIngestErrorsRepeated",
		"AlbionMarketAPIIngestPersistenceErrorsRepeated",
		"AlbionMarketAPIDatabasePoolSaturated",
		"AlbionMarketAPIDatabaseAcquireSlow",
		"AlbionMarketAPIRepeatedRestarts",
	} {
		requireContains(t, rules, "alert: "+alert)
	}

	forbiddenLabel := regexp.MustCompile(`(?m)^\s{8}(item_id|request_id|token_id|location|message):`)
	if match := forbiddenLabel.FindString(rules); match != "" {
		t.Fatalf("alert rules contain forbidden high-cardinality label %q", strings.TrimSpace(match))
	}
}

func TestAlertmanagerHasSafeLocalDefaultReceiver(t *testing.T) {
	config := readProjectFile(t, "observability", "alertmanager", "alertmanager.yml")
	for _, expected := range []string{
		"receiver: local-null",
		"- name: local-null",
		"inhibit_rules:",
		"severity=\"critical\"",
	} {
		requireContains(t, config, expected)
	}
	if strings.Contains(config, "webhook_configs:") || strings.Contains(config, "email_configs:") {
		t.Fatal("local Alertmanager must not ship alerts to an external receiver by default")
	}
}

func TestGrafanaDashboardAndDatasourceAreProvisioned(t *testing.T) {
	datasource := readProjectFile(t, "observability", "grafana", "provisioning", "datasources", "prometheus.yml")
	for _, expected := range []string{
		"uid: albion-prometheus",
		"url: http://prometheus:9090",
		"isDefault: true",
	} {
		requireContains(t, datasource, expected)
	}

	dashboardProvider := readProjectFile(t, "observability", "grafana", "provisioning", "dashboards", "dashboards.yml")
	requireContains(t, dashboardProvider, "path: /etc/grafana/dashboards")

	dashboardJSON := readProjectFile(t, "observability", "grafana", "dashboards", "albion-market-api-overview.json")
	var dashboard struct {
		UID    string `json:"uid"`
		Title  string `json:"title"`
		Panels []struct {
			Title string `json:"title"`
		} `json:"panels"`
	}
	if err := json.Unmarshal([]byte(dashboardJSON), &dashboard); err != nil {
		t.Fatalf("decode Grafana dashboard: %v", err)
	}
	if dashboard.UID != "albion-market-api-overview" {
		t.Fatalf("dashboard uid=%q", dashboard.UID)
	}
	if dashboard.Title == "" || len(dashboard.Panels) < 8 {
		t.Fatalf("dashboard is incomplete: title=%q panels=%d", dashboard.Title, len(dashboard.Panels))
	}
}

func TestObservabilitySmokeTestValidatesConfigsAndRuntime(t *testing.T) {
	script := readProjectFile(t, "scripts", "test-observability-compose.ps1")
	for _, expected := range []string{
		"/bin/promtool",
		"test\", \"rules",
		"/bin/amtool",
		"--profile\", \"observability",
		"/api/v1/targets",
		"/api/v1/rules?type=alert",
		"/api/dashboards/uid/albion-market-api-overview",
		"Assert-HardenedService",
	} {
		requireContains(t, script, expected)
	}
}
