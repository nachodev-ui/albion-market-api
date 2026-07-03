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

var stageSixAlertNames = []string{
	"AlbionMarketAPIUnavailable",
	"AlbionMarketAPINotReady",
	"AlbionMarketAPIRepeatedRestarts",
	"AlbionMarketAPIHighHTTP5xxRate",
	"AlbionMarketAPIHighHTTPLatency",
	"AlbionMarketAPIAuthenticationFailuresHigh",
	"AlbionMarketAPIIngestTrafficStopped",
	"AlbionMarketAPINoSuccessfulIngest",
	"AlbionMarketAPIIngestErrorsRepeated",
	"AlbionMarketAPIIngestPersistenceErrorsRepeated",
	"AlbionMarketAPIDatabasePoolSaturated",
	"AlbionMarketAPIDatabaseAcquireSlow",
}

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
	for _, alert := range stageSixAlertNames {
		requireContains(t, rules, "alert: "+alert)
	}

	forbiddenLabel := regexp.MustCompile(`(?m)^\s{8}(item_id|request_id|token_id|location|message):`)
	if match := forbiddenLabel.FindString(rules); match != "" {
		t.Fatalf("alert rules contain forbidden high-cardinality label %q", strings.TrimSpace(match))
	}
}

func TestEveryAlertHasRunbookRuleTestAndRuntimeCoverage(t *testing.T) {
	rules := readProjectFile(t, "observability", "prometheus", "rules", "albion-market-api.rules.yml")
	ruleTests := readProjectFile(t, "observability", "prometheus", "tests", "albion-market-api.rules.test.yml")
	runbooks := readProjectFile(t, "docs", "operations", "alerts.md")
	smoke := readProjectFile(t, "scripts", "test-observability-compose.ps1")

	alertPattern := regexp.MustCompile(`(?m)^\s*-\s+alert:\s+([A-Za-z0-9_]+)\s*$`)
	matches := alertPattern.FindAllStringSubmatch(rules, -1)
	if len(matches) != len(stageSixAlertNames) {
		t.Fatalf("alert rule count=%d, expected=%d", len(matches), len(stageSixAlertNames))
	}

	discovered := make(map[string]struct{}, len(matches))
	severityPattern := regexp.MustCompile(`(?m)^\s+severity:\s+(critical|warning)\s*$`)
	for _, match := range matches {
		discovered[match[1]] = struct{}{}
	}

	for _, alert := range stageSixAlertNames {
		if _, ok := discovered[alert]; !ok {
			t.Fatalf("expected alert %s was not discovered in rules", alert)
		}

		block := alertRuleBlock(t, rules, alert)
		if !severityPattern.MatchString(block) {
			t.Fatalf("alert %s does not declare a supported severity", alert)
		}
		for _, expected := range []string{
			"for:",
			"summary:",
			"description:",
			"runbook_url: https://nachodev-ui.github.io/albion-market-api/operations/alerts#" + strings.ToLower(alert),
		} {
			requireContains(t, block, expected)
		}

		requireContains(t, runbooks, "| `"+alert+"` |")
		requireContains(t, runbooks, "## "+alert)
		requireContains(t, ruleTests, "alertname: "+alert)
		requireContains(t, smoke, "\""+alert+"\"")
	}
}

func alertRuleBlock(t *testing.T, rules, alert string) string {
	t.Helper()

	marker := "      - alert: " + alert
	start := strings.Index(rules, marker)
	if start < 0 {
		t.Fatalf("alert %s was not found", alert)
	}

	remainderStart := start + len(marker)
	remainder := rules[remainderStart:]
	end := len(rules)
	for _, nextMarker := range []string{"\n      - alert:", "\n  - name:"} {
		if offset := strings.Index(remainder, nextMarker); offset >= 0 {
			candidate := remainderStart + offset
			if candidate < end {
				end = candidate
			}
		}
	}

	return rules[start:end]
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
			Title   string `json:"title"`
			Targets []struct {
				Expr string `json:"expr"`
			} `json:"targets"`
		} `json:"panels"`
	}
	if err := json.Unmarshal([]byte(dashboardJSON), &dashboard); err != nil {
		t.Fatalf("decode Grafana dashboard: %v", err)
	}
	if dashboard.UID != "albion-market-api-overview" {
		t.Fatalf("dashboard uid=%q", dashboard.UID)
	}
	if dashboard.Title == "" || len(dashboard.Panels) != 12 {
		t.Fatalf("dashboard is incomplete: title=%q panels=%d", dashboard.Title, len(dashboard.Panels))
	}

	panelTitles := make(map[string]struct{}, len(dashboard.Panels))
	var expressions strings.Builder
	for _, panel := range dashboard.Panels {
		panelTitles[panel.Title] = struct{}{}
		for _, target := range panel.Targets {
			expressions.WriteString(target.Expr)
			expressions.WriteByte('\n')
		}
	}

	for _, title := range []string{
		"API disponible",
		"Readiness",
		"Pool PostgreSQL",
		"Última ingesta exitosa",
		"Solicitudes HTTP por ruta",
		"Errores HTTP",
		"Latencia HTTP p95",
		"Resultado de batches de ingesta",
		"Entradas de ingesta",
		"Latencia de persistencia p95",
		"Conexiones PostgreSQL",
		"Errores de base de datos",
	} {
		if _, ok := panelTitles[title]; !ok {
			t.Fatalf("dashboard is missing panel %q", title)
		}
	}

	for _, metric := range []string{
		`up{job="albion-market-api"}`,
		"albion_market_api_readiness_ready",
		"albion_market_api_database_pool_utilization_ratio",
		"albion_market_api_ingest_last_success_timestamp_seconds",
		"albion_market_api_http_requests_total",
		"albion_market_api_http_errors_total",
		"albion_market_api_http_request_duration_seconds_bucket",
		"albion_market_api_ingest_batches_total",
		"albion_market_api_ingest_entries_stored_total",
		"albion_market_api_ingest_copy_duration_seconds_bucket",
		"albion_market_api_database_pool_acquired_connections",
		"albion_market_api_database_errors_total",
	} {
		requireContains(t, expressions.String(), metric)
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
	for _, alert := range stageSixAlertNames {
		requireContains(t, script, "\""+alert+"\"")
	}
}
