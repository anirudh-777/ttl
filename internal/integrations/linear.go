// Linear provider. Uses Linear's GraphQL API with a Personal API Key.

package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Linear implements Provider.
type Linear struct{}

// NewLinear returns a fresh Linear provider. Stateless, safe to share.
func NewLinear() *Linear { return &Linear{} }

func (Linear) Name() string { return "linear" }

const linearEndpoint = "https://api.linear.app/graphql"

type linearRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type linearResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

type linearIssue struct {
	ID         string         `json:"id"`
	Identifier string         `json:"identifier"`
	Title      string         `json:"title"`
	URL        string         `json:"url"`
	State      linearWorkflow `json:"state"`
	UpdatedAt  string         `json:"updatedAt"`
	Labels     struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
}

type linearWorkflow struct {
	Name string `json:"name"`
	Type string `json:"type"` // 'backlog' | 'unstarted' | 'started' | 'completed' | 'canceled'
}

// fetchAssignedQuery returns issues assigned to the authenticated user
// that are not yet completed. We use `state.type != "completed"`.
const fetchAssignedQuery = `
query AssignedIssues {
  viewer {
    assignedIssues(filter: { state: { type: { neq: "completed" } } }, first: 50) {
      nodes {
        id
        identifier
        title
        url
        updatedAt
        state { name type }
        labels { nodes { name } }
      }
    }
  }
}`

const fetchIssueQuery = `
query IssueByIdentifier($identifier: String!) {
  issue(id: $identifier) {
    id
    identifier
    title
    url
    updatedAt
    state { name type }
    labels { nodes { name } }
  }
}`

func (Linear) FetchAssigned(ctx context.Context, cfg map[string]string) ([]ExternalIssue, error) {
	token, ok := cfg["token"]
	if !ok || token == "" {
		return nil, errors.New("linear: config.token is required")
	}
	body := linearRequest{Query: fetchAssignedQuery}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", linearEndpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)

	resp, err := httpClient(0).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, newUpstreamErr("linear", resp)
	}
	var lr linearResponse
	if err := readJSON(resp.Body, &lr); err != nil {
		return nil, err
	}
	if len(lr.Errors) > 0 {
		return nil, fmt.Errorf("linear: %s", lr.Errors[0].Message)
	}
	var data struct {
		Viewer struct {
			AssignedIssues struct {
				Nodes []linearIssue `json:"nodes"`
			} `json:"assignedIssues"`
		} `json:"viewer"`
	}
	if err := json.Unmarshal(lr.Data, &data); err != nil {
		return nil, err
	}
	out := make([]ExternalIssue, 0, len(data.Viewer.AssignedIssues.Nodes))
	for _, n := range data.Viewer.AssignedIssues.Nodes {
		labels := make([]string, 0, len(n.Labels.Nodes))
		for _, l := range n.Labels.Nodes {
			labels = append(labels, l.Name)
		}
		state := n.State.Type
		if state == "" {
			state = "open"
		}
		out = append(out, ExternalIssue{
			ID:        n.Identifier, // user-visible id like "ENG-42"
			Title:     n.Title,
			URL:       n.URL,
			State:     state,
			Labels:    labels,
			UpdatedAt: parseTime(n.UpdatedAt),
		})
	}
	return out, nil
}

func (Linear) CloseIssue(ctx context.Context, cfg map[string]string, externalID string) error {
	token, ok := cfg["token"]
	if !ok || token == "" {
		return errors.New("linear: config.token is required")
	}
	// Resolve identifier -> id.
	body := linearRequest{Query: fetchIssueQuery, Variables: map[string]any{"identifier": externalID}}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", linearEndpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)
	resp, err := httpClient(0).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return newUpstreamErr("linear", resp)
	}
	var lr linearResponse
	if err := readJSON(resp.Body, &lr); err != nil {
		return err
	}
	var data struct {
		Issue *linearIssue `json:"issue"`
	}
	if err := json.Unmarshal(lr.Data, &data); err != nil {
		return err
	}
	if data.Issue == nil {
		return ErrNotFound
	}
	// Find a "completed" state. Linear requires us to use one of the
	// team's workflow states. We pick the first state whose type is
	// "completed" via a second query.
	completedStateID, err := findCompletedStateID(ctx, token, data.Issue.ID)
	if err != nil {
		return err
	}
	mutate := `mutation IssueUpdate($id: String!, $stateId: String!) {
  issueUpdate(id: $id, input: { stateId: $stateId }) { success }
}`
	mb, _ := json.Marshal(linearRequest{
		Query:     mutate,
		Variables: map[string]any{"id": data.Issue.ID, "stateId": completedStateID},
	})
	mreq, err := http.NewRequestWithContext(ctx, "POST", linearEndpoint, bytes.NewReader(mb))
	if err != nil {
		return err
	}
	mreq.Header.Set("Content-Type", "application/json")
	mreq.Header.Set("Authorization", token)
	mresp, err := httpClient(0).Do(mreq)
	if err != nil {
		return err
	}
	defer mresp.Body.Close()
	if mresp.StatusCode != 200 {
		return newUpstreamErr("linear", mresp)
	}
	return nil
}

const teamStatesQuery = `
query TeamStates($issueId: String!) {
  issue(id: $issueId) {
    team {
      states(first: 50) {
        nodes { id name type }
      }
    }
  }
}`

func findCompletedStateID(ctx context.Context, token, issueID string) (string, error) {
	b, _ := json.Marshal(linearRequest{Query: teamStatesQuery, Variables: map[string]any{"issueId": issueID}})
	req, err := http.NewRequestWithContext(ctx, "POST", linearEndpoint, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)
	resp, err := httpClient(0).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", newUpstreamErr("linear", resp)
	}
	var lr linearResponse
	if err := readJSON(resp.Body, &lr); err != nil {
		return "", err
	}
	var data struct {
		Issue struct {
			Team struct {
				States struct {
					Nodes []linearWorkflow `json:"nodes"`
				} `json:"states"`
			} `json:"team"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(lr.Data, &data); err != nil {
		return "", err
	}
	for _, s := range data.Issue.Team.States.Nodes {
		if strings.EqualFold(s.Type, "completed") {
			return s.Name, nil
		}
	}
	return "", errors.New("linear: no 'completed' state in team workflow")
}

// VerifyWebhook checks the Linear-Signature HMAC header.
func (Linear) VerifyWebhook(r *http.Request, secret string) (*WebhookEvent, error) {
	if secret == "" {
		return nil, errors.New("linear: webhook secret not configured")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if !verifyHMACSHA256(body, r.Header.Get("Linear-Signature"), secret) {
		return nil, errors.New("linear: signature mismatch")
	}
	var payload struct {
		Action string `json:"action"`
		Type   string `json:"type"`
		Data   struct {
			ID         string `json:"id"`
			Identifier string `json:"identifier"`
			Title      string `json:"title"`
			URL        string `json:"url"`
			UpdatedAt  string `json:"updatedAt"`
			State      struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"state"`
			Labels struct {
				Nodes []struct {
					Name string `json:"name"`
				} `json:"nodes"`
			} `json:"labels"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("linear: parse payload: %w", err)
	}
	if payload.Type != "Issue" {
		return nil, fmt.Errorf("linear: ignoring type %q", payload.Type)
	}
	labels := make([]string, 0, len(payload.Data.Labels.Nodes))
	for _, l := range payload.Data.Labels.Nodes {
		labels = append(labels, l.Name)
	}
	state := strings.ToLower(payload.Data.State.Type)
	if state == "" {
		state = strings.ToLower(payload.Data.State.Name)
	}
	return &WebhookEvent{
		Provider:   "linear",
		Action:     payload.Action,
		ExternalID: payload.Data.Identifier,
		Title:      payload.Data.Title,
		URL:        payload.Data.URL,
		State:      state,
		Labels:     labels,
		UpdatedAt:  parseTime(payload.Data.UpdatedAt),
	}, nil
}
