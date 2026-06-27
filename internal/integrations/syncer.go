// Syncer turns external issues into ttl tasks. It's a thin orchestration
// layer: it pulls issues from a provider, creates tasks for new ones,
// updates existing tasks if the upstream title/body changed, and
// maintains the issue_links table.

package integrations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/anirudh-777/ttl/internal/model"
	"github.com/anirudh-777/ttl/internal/store"
	"github.com/anirudh-777/ttl/internal/tenant"
)

// Syncer reconciles external issues with ttl tasks.
type Syncer struct {
	Store *store.Store
}

// New returns a Syncer backed by the given store.
func New(s *store.Store) *Syncer { return &Syncer{Store: s} }

// SyncStats is a summary of one sync run.
type SyncStats struct {
	Created     int
	Updated     int
	Closed      int
	Unchanged   int
	Integration string
}

// Sync pulls issues from the provider for the given integration and
// reconciles them with ttl tasks. New issues become new tasks; known
// issues update title/body if changed; closed upstream issues mark the
// matching ttl task complete.
func (s *Syncer) Sync(ctx context.Context, tc *tenant.Context, integrationID string, p Provider) (*SyncStats, error) {
	if integrationID == "" {
		return nil, errors.New("integration_id required")
	}
	it, err := s.Store.GetIntegration(ctx, tc, integrationID)
	if err != nil {
		return nil, err
	}
	if it.Provider != p.Name() {
		return nil, fmt.Errorf("provider mismatch: integration=%s provider=%s", it.Provider, p.Name())
	}

	issues, err := p.FetchAssigned(ctx, it.Config)
	if err != nil {
		return nil, fmt.Errorf("%s fetch: %w", p.Name(), err)
	}
	stats := &SyncStats{Integration: integrationID}

	seen := map[string]bool{}

	for _, issue := range issues {
		seen[issue.ID] = true

		// Skip if there's already a link for this external_id.
		existing, err := s.Store.IssueLinkForExternal(ctx, tc, integrationID, issue.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return stats, err
		}
		if existing != nil {
			// Already linked. Update state if changed.
			if existing.ExternalState != issue.State {
				_, err := s.Store.UpsertIssueLink(ctx, tc, integrationID, existing.TaskID,
					p.Name(), issue.ID, issue.URL, issue.State)
				if err != nil {
					return stats, err
				}
				stats.Updated++
			} else {
				stats.Unchanged++
			}
			// If upstream closed and the local task is still open, mark complete.
			if isClosedState(issue.State) && existing.ExternalState != "closed" {
				if _, _, err := s.Store.CompleteTaskAndRecur(ctx, tc, existing.TaskID); err != nil {
					return stats, err
				}
				stats.Closed++
			}
			continue
		}

		// No existing link — create a new task. Find a sensible project
		// for it: use the integration label as the project name, creating
		// it on demand.
		projectID, err := s.ensureProjectFor(ctx, tc, it.Label)
		if err != nil {
			return stats, err
		}
		due := time.Time{}
		_ = due
		tags := issue.Labels
		pid := projectID
		newTask, err := s.Store.CreateTask(ctx, tc, &model.Task{
			ID:        uuid.NewString(),
			Title:     issue.Title,
			Notes:     issue.Body,
			ProjectID: &pid,
			Tags:      tags,
			Status:    "open",
			Priority:  0,
		})
		if err != nil {
			return stats, fmt.Errorf("create task for %s/%s: %w", p.Name(), issue.ID, err)
		}
		if _, err := s.Store.UpsertIssueLink(ctx, tc, integrationID, newTask.ID,
			p.Name(), issue.ID, issue.URL, issue.State); err != nil {
			return stats, err
		}
		stats.Created++
	}

	// Bump last_synced_at.
	_ = s.Store.TouchIntegration(ctx, tc, integrationID)
	return stats, nil
}

// ensureProjectFor returns the id of a project with the given name in
// the tenant, creating one if it doesn't exist.
func (s *Syncer) ensureProjectFor(ctx context.Context, tc *tenant.Context, name string) (string, error) {
	projects, err := s.Store.ListProjects(ctx, tc, false)
	if err != nil {
		return "", err
	}
	for _, p := range projects {
		if p.Name == name {
			return p.ID, nil
		}
	}
	p, err := s.Store.CreateProject(ctx, tc, name, "")
	if err != nil {
		return "", err
	}
	return p.ID, nil
}

// HandleWebhook processes a webhook event for one integration: it
// creates / updates / closes the matching task. Idempotent.
func (s *Syncer) HandleWebhook(ctx context.Context, tc *tenant.Context, integrationID string, p Provider, ev *WebhookEvent) (*SyncStats, error) {
	stats := &SyncStats{Integration: integrationID}
	if ev.Provider != p.Name() {
		return stats, fmt.Errorf("provider mismatch: webhook=%s provider=%s", ev.Provider, p.Name())
	}
	link, err := s.Store.IssueLinkForExternal(ctx, tc, integrationID, ev.ExternalID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return stats, err
	}

	if link == nil {
		// No existing task — create one for the newly-seen issue.
		if ev.Action != "opened" && ev.Action != "created" && ev.Action != "" {
			// Skip updates for unknown issues.
			return stats, nil
		}
		it, err := s.Store.GetIntegration(ctx, tc, integrationID)
		if err != nil {
			return stats, err
		}
		projectID, err := s.ensureProjectFor(ctx, tc, it.Label)
		if err != nil {
			return stats, err
		}
		pid := projectID
		newTask, err := s.Store.CreateTask(ctx, tc, &model.Task{
			ID:        uuid.NewString(),
			Title:     ev.Title,
			Notes:     "",
			ProjectID: &pid,
			Tags:      ev.Labels,
			Status:    "open",
			Priority:  0,
		})
		if err != nil {
			return stats, err
		}
		if _, err := s.Store.UpsertIssueLink(ctx, tc, integrationID, newTask.ID,
			p.Name(), ev.ExternalID, ev.URL, ev.State); err != nil {
			return stats, err
		}
		stats.Created++
		return stats, nil
	}

	// Existing link — mirror the upstream change.
	if _, err := s.Store.UpsertIssueLink(ctx, tc, integrationID, link.TaskID,
		p.Name(), ev.ExternalID, ev.URL, ev.State); err != nil {
		return stats, err
	}
	if ev.Title != "" {
		_, _ = s.Store.UpdateTask(ctx, tc, link.TaskID, map[string]any{"title": ev.Title})
		stats.Updated++
	}
	if isClosedState(ev.State) {
		if _, _, err := s.Store.CompleteTaskAndRecur(ctx, tc, link.TaskID); err != nil {
			return stats, err
		}
		stats.Closed++
	}
	return stats, nil
}

func isClosedState(s string) bool {
	switch s {
	case "closed", "completed", "canceled", "cancelled", "done":
		return true
	}
	return false
}
