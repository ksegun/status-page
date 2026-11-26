package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type qualityCollector interface {
	name() string
	collectQuality(context.Context) (qualityReport, error)
}

type sonarCloudCollector struct {
	token        string
	organization string
	projectKey   string
	apiURL       string
}

func (s sonarCloudCollector) name() string { return "SonarCloud" }

var sonarMetricKeys = map[string]string{
	"coverage":                 "coverage",
	"bugs":                     "bugs",
	"vulnerabilities":          "vulnerabilities",
	"code_smells":              "codeSmells",
	"duplicated_lines_density": "duplicatedLinesDensity",
}

func (s sonarCloudCollector) collectQuality(ctx context.Context) (qualityReport, error) {
	apiURL := strings.TrimRight(s.apiURL, "/")
	if apiURL == "" {
		apiURL = "https://sonarcloud.io"
	}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(s.token+":"))
	values := url.Values{
		"component":    {s.projectKey},
		"organization": {s.organization},
		"metricKeys":   {"coverage,bugs,vulnerabilities,code_smells,duplicated_lines_density"},
	}
	var measures struct {
		Component struct {
			Measures []struct {
				Metric string `json:"metric"`
				Value  string `json:"value"`
			} `json:"measures"`
		} `json:"component"`
		Errors []struct {
			Message string `json:"msg"`
		} `json:"errors"`
	}
	headers := map[string]string{"Authorization": auth, "Accept": "application/json"}
	if err := requestJSON(ctx, sharedHTTPClient, "GET", apiURL+"/api/measures/component?"+values.Encode(), nil, headers, &measures); err != nil {
		return qualityReport{}, fmt.Errorf("measures: %w", err)
	}
	if len(measures.Errors) > 0 {
		return qualityReport{}, fmt.Errorf("measures: %s", measures.Errors[0].Message)
	}

	report := qualityReport{Metrics: make(map[string]qualityMetric)}
	for _, measure := range measures.Component.Measures {
		key, ok := sonarMetricKeys[measure.Metric]
		if !ok {
			continue
		}
		value, err := strconv.ParseFloat(measure.Value, 64)
		if err != nil {
			return qualityReport{}, fmt.Errorf("parse %s measure %q: %w", measure.Metric, measure.Value, err)
		}
		unit := ""
		if measure.Metric == "coverage" || measure.Metric == "duplicated_lines_density" {
			unit = "%"
		}
		report.Metrics[key] = qualityMetric{Value: value, Unit: unit, Available: true}
	}

	gateValues := url.Values{"projectKey": {s.projectKey}, "organization": {s.organization}}
	var gate struct {
		ProjectStatus struct {
			Status string `json:"status"`
		} `json:"projectStatus"`
		Errors []struct {
			Message string `json:"msg"`
		} `json:"errors"`
	}
	if err := requestJSON(ctx, sharedHTTPClient, "GET", apiURL+"/api/qualitygates/project_status?"+gateValues.Encode(), nil, headers, &gate); err != nil {
		return qualityReport{}, fmt.Errorf("quality gate: %w", err)
	}
	if len(gate.Errors) > 0 {
		return qualityReport{}, fmt.Errorf("quality gate: %s", gate.Errors[0].Message)
	}
	if gate.ProjectStatus.Status == "" {
		return qualityReport{}, fmt.Errorf("quality gate response did not include a status")
	}
	report.QualityGate = gate.ProjectStatus.Status
	return report, nil
}
