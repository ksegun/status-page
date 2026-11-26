package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWeekWindowStartsMonday(t *testing.T) {
	now := time.Date(2026, time.November, 26, 8, 30, 0, 0, time.FixedZone("local", -6*60*60))
	start, end := weekWindow(now)
	if got, want := start.Weekday(), time.Monday; got != want {
		t.Fatalf("week starts on %s, want Monday", got)
	}
	if got := end.Sub(start); got != 7*24*time.Hour {
		t.Fatalf("week duration = %s, want 168h", got)
	}
}

func TestInWindowUsesInclusiveStartAndExclusiveEnd(t *testing.T) {
	start := time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 7)
	if !inWindow(start, start, end) {
		t.Fatal("start should be included")
	}
	if inWindow(end, start, end) {
		t.Fatal("end should be excluded")
	}
	if inWindow(time.Time{}, start, end) {
		t.Fatal("zero timestamp should not be included")
	}
}

func TestBuildReportUsesDemoOnlyWhenNoCollectorsConfigured(t *testing.T) {
	now := time.Date(2026, time.November, 26, 8, 30, 0, 0, time.UTC)
	report := buildReport(context.Background(), nil, now)
	if !report.Demo {
		t.Fatal("expected sample report when no sources are configured")
	}
	if len(report.Shipped) == 0 || len(report.Metrics) == 0 {
		t.Fatal("sample report should contain items and metrics")
	}
	if report.Quality.QualityGate != "OK" || !report.Quality.Metrics["coverage"].Available {
		t.Fatal("sample report should include quality metrics")
	}
}

func TestIncompleteConfigurationDoesNotReturnDemoData(t *testing.T) {
	for _, name := range []string{
		"GITHUB_TOKEN", "GITHUB_REPOS", "GITHUB_OWNER", "GITHUB_REPO",
		"GITHUB_GRAPHQL_URL", "GITHUB_API_URL", "AZDO_PROJECT", "AZDO_REPOSITORY", "AZDO_PAT",
		"SONARCLOUD_TOKEN", "SONARCLOUD_ORGANIZATION", "SONARCLOUD_PROJECT_KEY",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("AZDO_ORG", "example")
	now := time.Date(2026, time.November, 26, 8, 30, 0, 0, time.UTC)
	report := generateReport(context.Background(), loadConfig(), now)
	if report.Demo {
		t.Fatal("incomplete integration configuration must not show sample data")
	}
	if len(report.Errors) == 0 || len(report.Metrics) != 0 {
		t.Fatalf("expected a configuration error and no fabricated metrics, got errors=%v metrics=%v", report.Errors, report.Metrics)
	}
}

func TestSonarCloudCollectorReturnsQualityMeasuresAndGate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantAuth := "Bearer sonar-token"
		if got := r.Header.Get("Authorization"); got != wantAuth {
			t.Errorf("authorization = %q, want SonarCloud token authentication", got)
		}
		switch r.URL.Path {
		case "/api/measures/component":
			if got := r.URL.Query().Get("component"); got != "acme:service" {
				t.Errorf("component = %q, want acme:service", got)
			}
			if got := r.URL.Query().Get("organization"); got != "acme" {
				t.Errorf("organization = %q, want acme", got)
			}
			_, _ = fmt.Fprint(w, `{"component":{"measures":[{"metric":"coverage","value":"82.5"},{"metric":"bugs","value":"3"},{"metric":"vulnerabilities","value":"0"},{"metric":"code_smells","value":"12"},{"metric":"duplicated_lines_density","value":"4.2"}]}}`)
		case "/api/qualitygates/project_status":
			_, _ = fmt.Fprint(w, `{"projectStatus":{"status":"OK"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	report, err := (sonarCloudCollector{
		token: "sonar-token", organization: "acme", projectKey: "acme:service", apiURL: server.URL,
	}).collectQuality(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.QualityGate != "OK" {
		t.Fatalf("quality gate = %q, want OK", report.QualityGate)
	}
	if got := report.Metrics["coverage"]; !got.Available || got.Value != 82.5 || got.Unit != "%" {
		t.Errorf("coverage = %+v, want available 82.5%%", got)
	}
	if got := report.Metrics["vulnerabilities"]; !got.Available || got.Value != 0 {
		t.Errorf("vulnerabilities = %+v, want available zero", got)
	}
	if got := report.Metrics["codeSmells"]; !got.Available || got.Value != 12 {
		t.Errorf("code smells = %+v, want available 12", got)
	}
}

func TestSonarCloudConfigurationIsRequiredAsASet(t *testing.T) {
	for _, name := range []string{
		"GITHUB_TOKEN", "GITHUB_REPOS", "GITHUB_OWNER", "GITHUB_REPO",
		"AZDO_ORG", "AZDO_PROJECT", "AZDO_REPOSITORY", "AZDO_PAT",
		"SONARCLOUD_TOKEN", "SONARCLOUD_ORGANIZATION",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("SONARCLOUD_PROJECT_KEY", "acme:service")

	report := generateReport(context.Background(), loadConfig(), time.Now())
	if report.Demo {
		t.Fatal("incomplete SonarCloud configuration must not show sample data")
	}
	if len(report.Errors) != 1 || len(report.Metrics) != 0 {
		t.Fatalf("expected only a configuration error and no fabricated metrics, got errors=%v metrics=%v", report.Errors, report.Metrics)
	}
}

type fixedCollector struct {
	sourceName   string
	currentStart time.Time
	current      sourceData
	previous     sourceData
}

func (c fixedCollector) name() string { return c.sourceName }

func (c fixedCollector) collect(_ context.Context, start, _ time.Time) (sourceData, error) {
	if start.Equal(c.currentStart) {
		return c.current, nil
	}
	return c.previous, nil
}

func TestBuildReportCalculatesTeamReviewCoverage(t *testing.T) {
	now := time.Date(2026, time.November, 26, 8, 30, 0, 0, time.UTC)
	currentStart, _ := weekWindow(now)
	report := buildReport(context.Background(), []collector{
		fixedCollector{
			sourceName:   "GitHub Enterprise",
			currentStart: currentStart,
			current: sourceData{
				Shipped:     []workItem{{}, {}, {}, {}},
				ReviewedPRs: 3,
			},
			previous: sourceData{
				Shipped:     []workItem{{}, {}},
				ReviewedPRs: 1,
			},
		},
	}, now)

	got := report.Metrics["reviewCoverage"]
	if got.Value != 75 || got.Previous != 50 || got.Unit != "%" {
		t.Fatalf("review coverage = %+v, want 75%% current and 50%% previous", got)
	}
}

func TestPercentageWithNoMergedPullRequests(t *testing.T) {
	if got := percentage(0, 0); got != 0 {
		t.Fatalf("percentage with no merged pull requests = %v, want 0", got)
	}
}
