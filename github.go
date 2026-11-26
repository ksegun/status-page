package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type githubCollector struct {
	token      string
	graphqlURL string
	apiURL     string
	repos      []string
}

func (g githubCollector) name() string { return "GitHub Enterprise" }

type githubGraphQLResponse struct {
	Data struct {
		Repository struct {
			PullRequests struct {
				Nodes []struct {
					Title     string    `json:"title"`
					URL       string    `json:"url"`
					State     string    `json:"state"`
					CreatedAt time.Time `json:"createdAt"`
					UpdatedAt time.Time `json:"updatedAt"`
					MergedAt  time.Time `json:"mergedAt"`
					Reviews   struct {
						Nodes []struct {
							State  string `json:"state"`
							Author *struct {
								Login string `json:"login"`
							} `json:"author"`
						} `json:"nodes"`
					} `json:"reviews"`
					Author *struct {
						Login string `json:"login"`
					} `json:"author"`
				} `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

const pullRequestsQuery = `query($owner:String!,$name:String!){
  repository(owner:$owner,name:$name){
    pullRequests(first:100,states:[OPEN,MERGED],orderBy:{field:UPDATED_AT,direction:DESC}){
      nodes{title url state createdAt updatedAt mergedAt author{login} reviews(first:100){nodes{state author{login}}}}
    }
  }
}`

func (g githubCollector) collect(ctx context.Context, start, end time.Time) (sourceData, error) {
	var combined sourceData
	for _, fullName := range g.repos {
		owner, repo, ok := strings.Cut(fullName, "/")
		if !ok || owner == "" || repo == "" {
			return sourceData{}, fmt.Errorf("invalid GITHUB_REPOS entry %q; expected owner/repository", fullName)
		}
		payload, err := json.Marshal(map[string]any{
			"query":     pullRequestsQuery,
			"variables": map[string]string{"owner": owner, "name": repo},
		})
		if err != nil {
			return sourceData{}, err
		}
		var response githubGraphQLResponse
		err = requestJSON(ctx, sharedHTTPClient, "POST", g.graphqlURL, bytes.NewReader(payload), map[string]string{
			"Authorization": "Bearer " + g.token,
			"Content-Type":  "application/json",
			"Accept":        "application/json",
		}, &response)
		if err != nil {
			return sourceData{}, fmt.Errorf("%s: %w", fullName, err)
		}
		if len(response.Errors) > 0 {
			return sourceData{}, fmt.Errorf("%s: GraphQL: %s", fullName, response.Errors[0].Message)
		}
		for _, pr := range response.Data.Repository.PullRequests.Nodes {
			author := ""
			if pr.Author != nil {
				author = pr.Author.Login
			}
			item := workItem{Title: pr.Title, URL: pr.URL, Source: fullName, Author: author, CreatedAt: pr.CreatedAt, UpdatedAt: pr.UpdatedAt, ClosedAt: pr.MergedAt}
			if pr.State == "MERGED" && inWindow(pr.MergedAt, start, end) {
				combined.Shipped = append(combined.Shipped, item)
				combined.CycleHours = append(combined.CycleHours, pr.MergedAt.Sub(pr.CreatedAt).Hours())
				for _, review := range pr.Reviews.Nodes {
					if review.State != "PENDING" && review.Author != nil && (pr.Author == nil || review.Author.Login != pr.Author.Login) {
						combined.ReviewedPRs++
						break
					}
				}
			}
			if pr.State == "OPEN" {
				combined.InProgress = append(combined.InProgress, item)
			}
		}
		if err := g.collectActions(ctx, owner, repo, start, end, &combined); err != nil {
			return sourceData{}, fmt.Errorf("%s Actions: %w", fullName, err)
		}
	}
	return combined, nil
}

func (g githubCollector) collectActions(ctx context.Context, owner, repo string, start, end time.Time, data *sourceData) error {
	endpoint := fmt.Sprintf("%s/repos/%s/%s/actions/runs?per_page=100", g.apiURL, url.PathEscape(owner), url.PathEscape(repo))
	var response struct {
		WorkflowRuns []struct {
			CreatedAt  time.Time `json:"created_at"`
			Status     string    `json:"status"`
			Conclusion string    `json:"conclusion"`
		} `json:"workflow_runs"`
	}
	if err := requestJSON(ctx, sharedHTTPClient, "GET", endpoint, nil, map[string]string{
		"Authorization":        "Bearer " + g.token,
		"Accept":               "application/vnd.github+json",
		"X-GitHub-Api-Version": "2022-11-28",
	}, &response); err != nil {
		return err
	}
	for _, run := range response.WorkflowRuns {
		if inWindow(run.CreatedAt, start, end) && run.Status == "completed" && run.Conclusion == "success" {
			data.SuccessfulCI++
		}
	}
	return nil
}

func inWindow(value, start, end time.Time) bool {
	return !value.IsZero() && !value.Before(start) && value.Before(end)
}
