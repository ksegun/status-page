# Engineering Status

A lightweight weekly engineering status report built with Go. It summarizes merged pull requests, active work, stale-work risks, successful GitHub Actions runs, average PR cycle time, and team-level review coverage, with comparisons to the previous week.

With no integrations configured, the dashboard displays clearly labeled sample data. Configure any of the data sources to use live data.

## Run locally

```sh
go run .
```

Open [http://localhost:8080](http://localhost:8080). The app binds to localhost by default; set `ADDR` to change the listen address. If exposing it beyond localhost, put it behind your organization’s authentication and network controls. The app uses only the Go standard library.

## Data sources

### GitHub Enterprise

Set a read-only token with access to the selected repositories and Actions workflow runs:

```sh
export GITHUB_TOKEN=...
export GITHUB_REPOS="engineering/api,engineering/web"
```

For GitHub Enterprise Server, configure its GraphQL and REST endpoints:

```sh
export GITHUB_GRAPHQL_URL="https://github.example.com/api/graphql"
export GITHUB_API_URL="https://github.example.com/api/v3"
```

Alternatively, select a single repo with `GITHUB_OWNER` and `GITHUB_REPO`. Pull requests are queried over GraphQL; Actions workflow runs use the GitHub REST API because that is the supported Actions interface. The report calls these successful CI runs, not deployments.

### Azure DevOps

Configure an organization, project, repository, and read-only personal access token:

```sh
export AZDO_ORG="my-organization"
export AZDO_PROJECT="my-project"
export AZDO_REPOSITORY="my-repository"
export AZDO_PAT=...
```

Azure DevOps pull requests are read through its supported REST API; Azure DevOps does not expose a supported public GraphQL API for this data.

### SonarCloud

Configure a project and a read-only SonarCloud user token to show the latest quality gate, coverage, bug, vulnerability, code-smell, and duplicated-line measures:

```sh
export SONARCLOUD_TOKEN=...
export SONARCLOUD_ORGANIZATION="my-organization"
export SONARCLOUD_PROJECT_KEY="my-project-key"
```

Quality measures are a snapshot of the latest SonarCloud analysis, not a weekly comparison. Metrics not available for the selected project are shown as unavailable rather than as zero.

### Notes

- The reporting week starts Monday in the server's local timezone. The current week and the previous seven-day week are compared.
- Review coverage is the percentage of merged pull requests with a peer GitHub review or a non-zero Azure DevOps reviewer vote. It is not a measure of review quality.
- Deployment frequency, change failure rate, and investment allocation are not inferred from CI runs or pull request data and are not currently measured.
- A stale-work risk is an open PR created at least seven days ago. Treat it as a prompt to review, not a confirmed blocker.
- The dashboard surfaces connection failures instead of substituting sample data for configured sources. See `/healthz` for a basic process health check and `/api/status` for report data.
- Keep credentials in environment variables or your deployment platform's secret store. Do not commit tokens.
