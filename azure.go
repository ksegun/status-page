package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type azureCollector struct {
	org        string
	project    string
	repository string
	pat        string
}

func (a azureCollector) name() string { return "Azure DevOps" }

func (a azureCollector) collect(ctx context.Context, start, end time.Time) (sourceData, error) {
	if err := required(a.org, "AZDO_ORG"); err != nil {
		return sourceData{}, err
	}
	endpoint := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/pullrequests?searchCriteria.status=all&$top=100&api-version=7.1",
		url.PathEscape(a.org), url.PathEscape(a.project), url.PathEscape(a.repository))
	auth := base64.StdEncoding.EncodeToString([]byte(":" + a.pat))
	var response struct {
		Value []struct {
			ID           int       `json:"pullRequestId"`
			Title        string    `json:"title"`
			Status       string    `json:"status"`
			CreationDate time.Time `json:"creationDate"`
			ClosedDate   time.Time `json:"closedDate"`
			URL          string    `json:"url"`
			CreatedBy    struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayName"`
			} `json:"createdBy"`
			Reviewers []struct {
				ID   string `json:"id"`
				Vote int    `json:"vote"`
			} `json:"reviewers"`
		} `json:"value"`
	}
	err := requestJSON(ctx, sharedHTTPClient, "GET", endpoint, nil, map[string]string{
		"Authorization": "Basic " + auth,
		"Accept":        "application/json",
	}, &response)
	if err != nil {
		return sourceData{}, err
	}
	var data sourceData
	for _, pr := range response.Value {
		link := ""
		if pr.URL != "" {
			link = fmt.Sprintf("https://dev.azure.com/%s/%s/_git/%s/pullrequest/%d",
				url.PathEscape(a.org), url.PathEscape(a.project), url.PathEscape(a.repository), pr.ID)
		}
		item := workItem{
			Title: pr.Title, URL: link, Source: "Azure DevOps", Author: pr.CreatedBy.DisplayName,
			CreatedAt: pr.CreationDate, ClosedAt: pr.ClosedDate,
		}
		if strings.EqualFold(pr.Status, "completed") && inWindow(pr.ClosedDate, start, end) {
			data.Shipped = append(data.Shipped, item)
			data.CycleHours = append(data.CycleHours, pr.ClosedDate.Sub(pr.CreationDate).Hours())
			for _, reviewer := range pr.Reviewers {
				if reviewer.Vote != 0 && !strings.EqualFold(reviewer.ID, pr.CreatedBy.ID) {
					data.ReviewedPRs++
					break
				}
			}
		}
		if strings.EqualFold(pr.Status, "active") {
			data.InProgress = append(data.InProgress, item)
		}
	}
	return data, nil
}
