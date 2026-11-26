package main

import (
	"context"
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
}

func TestIncompleteConfigurationDoesNotReturnDemoData(t *testing.T) {
	for _, name := range []string{
		"GITHUB_TOKEN", "GITHUB_REPOS", "GITHUB_OWNER", "GITHUB_REPO",
		"GITHUB_GRAPHQL_URL", "GITHUB_API_URL", "AZDO_PROJECT", "AZDO_REPOSITORY", "AZDO_PAT",
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
