package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed web/*
var webFiles embed.FS

type config struct {
	githubToken      string
	githubGraphQLURL string
	githubAPIURL     string
	githubRepos      []string
	azdoOrg          string
	azdoProject      string
	azdoRepository   string
	azdoPAT          string
	issues           []configurationIssue
}

type configurationIssue struct {
	source  string
	message string
}

type collector interface {
	name() string
	collect(context.Context, time.Time, time.Time) (sourceData, error)
}

type sourceData struct {
	Shipped      []workItem
	InProgress   []workItem
	SuccessfulCI int
	CycleHours   []float64
}

type workItem struct {
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Source    string    `json:"source"`
	Author    string    `json:"author,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
	ClosedAt  time.Time `json:"closedAt,omitempty"`
}

type metric struct {
	Value    float64 `json:"value"`
	Previous float64 `json:"previous"`
	Unit     string  `json:"unit,omitempty"`
}

type sourceStatus struct {
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Connected bool      `json:"connected"`
	Message   string    `json:"message"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

type statusReport struct {
	GeneratedAt time.Time         `json:"generatedAt"`
	WeekStart   time.Time         `json:"weekStart"`
	WeekEnd     time.Time         `json:"weekEnd"`
	Demo        bool              `json:"demo"`
	Metrics     map[string]metric `json:"metrics"`
	Shipped     []workItem        `json:"shipped"`
	InProgress  []workItem        `json:"inProgress"`
	Risks       []workItem        `json:"risks"`
	Sources     []sourceStatus    `json:"sources"`
	Errors      []string          `json:"errors"`
}

func main() {
	cfg := loadConfig()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		report := generateReport(r.Context(), cfg, time.Now())
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(report); err != nil {
			log.Printf("encode status report: %v", err)
		}
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	staticFiles, err := fs.Sub(webFiles, "web")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticFiles)))

	addr := envOr("ADDR", "127.0.0.1:8080")
	log.Printf("engineering status listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func loadConfig() config {
	repos := splitList(os.Getenv("GITHUB_REPOS"))
	if len(repos) == 0 && os.Getenv("GITHUB_OWNER") != "" && os.Getenv("GITHUB_REPO") != "" {
		repos = []string{os.Getenv("GITHUB_OWNER") + "/" + os.Getenv("GITHUB_REPO")}
	}
	cfg := config{
		githubToken:      os.Getenv("GITHUB_TOKEN"),
		githubGraphQLURL: envOr("GITHUB_GRAPHQL_URL", "https://api.github.com/graphql"),
		githubAPIURL:     strings.TrimRight(envOr("GITHUB_API_URL", "https://api.github.com"), "/"),
		githubRepos:      repos,
		azdoOrg:          os.Getenv("AZDO_ORG"),
		azdoProject:      os.Getenv("AZDO_PROJECT"),
		azdoRepository:   os.Getenv("AZDO_REPOSITORY"),
		azdoPAT:          os.Getenv("AZDO_PAT"),
	}
	githubRequested := cfg.githubToken != "" || len(repos) > 0 || os.Getenv("GITHUB_OWNER") != "" || os.Getenv("GITHUB_REPO") != ""
	if githubRequested && (cfg.githubToken == "" || len(cfg.githubRepos) == 0) {
		cfg.issues = append(cfg.issues, configurationIssue{
			source:  "GitHub Enterprise",
			message: "set GITHUB_TOKEN and GITHUB_REPOS (or both GITHUB_OWNER and GITHUB_REPO)",
		})
	}
	azdoRequested := cfg.azdoOrg != "" || cfg.azdoProject != "" || cfg.azdoRepository != "" || cfg.azdoPAT != ""
	if azdoRequested && (cfg.azdoOrg == "" || cfg.azdoProject == "" || cfg.azdoRepository == "" || cfg.azdoPAT == "") {
		cfg.issues = append(cfg.issues, configurationIssue{
			source:  "Azure DevOps",
			message: "set AZDO_ORG, AZDO_PROJECT, AZDO_REPOSITORY, and AZDO_PAT",
		})
	}
	return cfg
}

func configuredCollectors(cfg config) []collector {
	var sources []collector
	if cfg.githubToken != "" && len(cfg.githubRepos) > 0 {
		sources = append(sources, githubCollector{
			token: cfg.githubToken, graphqlURL: cfg.githubGraphQLURL,
			apiURL: cfg.githubAPIURL, repos: cfg.githubRepos,
		})
	}
	if cfg.azdoOrg != "" && cfg.azdoProject != "" && cfg.azdoRepository != "" && cfg.azdoPAT != "" {
		sources = append(sources, azureCollector{
			org: cfg.azdoOrg, project: cfg.azdoProject,
			repository: cfg.azdoRepository, pat: cfg.azdoPAT,
		})
	}
	return sources
}

func generateReport(ctx context.Context, cfg config, now time.Time) statusReport {
	sources := configuredCollectors(cfg)
	report := buildReport(ctx, sources, now)
	if len(sources) == 0 && len(cfg.issues) > 0 {
		start, end := weekWindow(now)
		report = statusReport{
			GeneratedAt: now, WeekStart: start, WeekEnd: end,
			Metrics: make(map[string]metric),
		}
	}
	for _, issue := range cfg.issues {
		report.Errors = append(report.Errors, issue.source+": "+issue.message)
		report.Sources = append(report.Sources, sourceStatus{
			Name: issue.source, Kind: issue.source, Message: "Configuration incomplete",
		})
	}
	return report
}

func buildReport(ctx context.Context, sources []collector, now time.Time) statusReport {
	start, end := weekWindow(now)
	previousStart := start.AddDate(0, 0, -7)
	var all, previous sourceData
	report := statusReport{
		GeneratedAt: now, WeekStart: start, WeekEnd: end,
		Metrics: make(map[string]metric),
	}

	if len(sources) == 0 {
		return demoReport(now, start, end)
	}

	type result struct {
		name        string
		current     sourceData
		previous    sourceData
		currentErr  error
		previousErr error
	}
	results := make(chan result, len(sources))
	for _, source := range sources {
		go func(source collector) {
			sourceCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			current, err := source.collect(sourceCtx, start, end)
			if err != nil {
				results <- result{name: source.name(), currentErr: err}
				return
			}
			old, err := source.collect(sourceCtx, previousStart, start)
			results <- result{name: source.name(), current: current, previous: old, previousErr: err}
		}(source)
	}
	for received := 0; received < len(sources); received++ {
		res := <-results
		if res.currentErr != nil {
			report.Errors = append(report.Errors, res.name+": "+res.currentErr.Error())
			report.Sources = append(report.Sources, sourceStatus{
				Name: res.name, Kind: sourceKind(res.name), Connected: false,
				Message: "Unable to load data",
			})
			continue
		}
		all = mergeData(all, res.current)
		if res.previousErr == nil {
			previous = mergeData(previous, res.previous)
		} else {
			report.Errors = append(report.Errors, res.name+" previous week: "+res.previousErr.Error())
		}
		report.Sources = append(report.Sources, sourceStatus{
			Name: res.name, Kind: sourceKind(res.name), Connected: true,
			Message: "Connected", UpdatedAt: now,
		})
	}

	report.Shipped = all.Shipped
	report.InProgress = all.InProgress
	for _, item := range all.InProgress {
		if !item.CreatedAt.IsZero() && now.Sub(item.CreatedAt) >= 7*24*time.Hour {
			report.Risks = append(report.Risks, item)
		}
	}
	if len(report.Sources) > 0 && hasConnectedSource(report.Sources) {
		report.Metrics["mergedPRs"] = metric{Value: float64(len(all.Shipped)), Previous: float64(len(previous.Shipped))}
		report.Metrics["activePRs"] = metric{Value: float64(len(all.InProgress)), Previous: float64(len(previous.InProgress))}
		report.Metrics["successfulCI"] = metric{Value: float64(all.SuccessfulCI), Previous: float64(previous.SuccessfulCI)}
		report.Metrics["cycleTime"] = metric{Value: average(all.CycleHours), Previous: average(previous.CycleHours), Unit: "hours"}
	}
	return report
}

func hasConnectedSource(sources []sourceStatus) bool {
	for _, source := range sources {
		if source.Connected {
			return true
		}
	}
	return false
}

func mergeData(dst, src sourceData) sourceData {
	dst.Shipped = append(dst.Shipped, src.Shipped...)
	dst.InProgress = append(dst.InProgress, src.InProgress...)
	dst.SuccessfulCI += src.SuccessfulCI
	dst.CycleHours = append(dst.CycleHours, src.CycleHours...)
	return dst
}

func weekWindow(now time.Time) (time.Time, time.Time) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	daysSinceMonday := (int(today.Weekday()) + 6) % 7
	start := today.AddDate(0, 0, -daysSinceMonday)
	return start, start.AddDate(0, 0, 7)
}

func average(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}

func sourceKind(name string) string {
	if strings.Contains(strings.ToLower(name), "azure") {
		return "Azure DevOps"
	}
	return "GitHub Enterprise"
}

func demoReport(now, start, end time.Time) statusReport {
	shipped := []workItem{
		{Title: "Shipped account recovery improvements", URL: "#", Source: "GitHub", Author: "Platform", CreatedAt: start.Add(-72 * time.Hour), ClosedAt: start.Add(24 * time.Hour)},
		{Title: "Reduced dashboard load time", URL: "#", Source: "GitHub", Author: "Web", CreatedAt: start.Add(-96 * time.Hour), ClosedAt: start.Add(48 * time.Hour)},
		{Title: "Hardened deployment workflow", URL: "#", Source: "GitHub", Author: "Infrastructure", CreatedAt: start.Add(-48 * time.Hour), ClosedAt: start.Add(72 * time.Hour)},
	}
	active := []workItem{
		{Title: "Improve self-service onboarding", URL: "#", Source: "GitHub", Author: "Growth", CreatedAt: now.Add(-3 * 24 * time.Hour), UpdatedAt: now.Add(-4 * time.Hour)},
		{Title: "Migrate background jobs to the new queue", URL: "#", Source: "Azure DevOps", Author: "Platform", CreatedAt: now.Add(-9 * 24 * time.Hour), UpdatedAt: now.Add(-8 * 24 * time.Hour)},
	}
	return statusReport{
		GeneratedAt: now, WeekStart: start, WeekEnd: end, Demo: true,
		Metrics: map[string]metric{
			"mergedPRs":    {Value: 12, Previous: 9},
			"activePRs":    {Value: 7, Previous: 8},
			"successfulCI": {Value: 34, Previous: 29},
			"cycleTime":    {Value: 26.4, Previous: 31.2, Unit: "hours"},
		},
		Shipped: shipped, InProgress: active, Risks: []workItem{active[1]},
		Sources: []sourceStatus{
			{Name: "GitHub Enterprise", Kind: "GitHub Enterprise", Message: "Sample preview"},
			{Name: "Azure DevOps", Kind: "Azure DevOps", Message: "Sample preview"},
		},
		Errors: nil,
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func splitList(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func requestJSON(ctx context.Context, client *http.Client, method, url string, body io.Reader, headers map[string]string, target any) error {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf("upstream returned %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return fmt.Errorf("decode upstream response: %w", err)
	}
	return nil
}

var sharedHTTPClient = &http.Client{Timeout: 10 * time.Second}

func required(value, label string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New(label + " is required")
	}
	return nil
}
