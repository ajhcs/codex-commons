package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

func verifyComplete(ctx context.Context, c *apiClient, m Manifest, receipt Receipt) error {
	var project struct {
		Project struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Status  string `json:"status"`
			Purpose string `json:"purpose"`
			Now     string `json:"now"`
		} `json:"project"`
	}
	if err := c.get(ctx, "/v1/projects/"+url.PathEscape(m.Project.ID), &project); err != nil {
		return fmt.Errorf("project: %w", err)
	}
	if project.Project.ID != m.Project.ID || project.Project.Name != m.Project.Name || project.Project.Status != m.Project.Status || project.Project.Purpose != m.Project.Purpose || project.Project.Now != m.Project.Now {
		return errors.New("project fields do not match manifest")
	}

	var milestoneList struct {
		Items []struct {
			ID         string `json:"id"`
			Title      string `json:"title"`
			Status     string `json:"status"`
			Position   int    `json:"position"`
			TargetDate string `json:"target_date"`
		} `json:"items"`
	}
	if err := c.get(ctx, "/v1/projects/"+url.PathEscape(m.Project.ID)+"/milestones?limit=100", &milestoneList); err != nil {
		return fmt.Errorf("milestones: %w", err)
	}
	type milestoneValue struct {
		Title, Status, TargetDate string
		Position                  int
	}
	byID := map[string]milestoneValue{}
	for _, got := range milestoneList.Items {
		byID[got.ID] = milestoneValue{got.Title, got.Status, got.TargetDate, got.Position}
	}
	for _, item := range m.Milestones {
		got, ok := byID[receipt.Milestones[item.Key]]
		if !ok || got != (milestoneValue{item.Title, item.Status, item.TargetDate, item.Position}) {
			return fmt.Errorf("milestone %q fields do not match manifest", item.Key)
		}
	}

	for _, item := range m.Tasks {
		var opened struct {
			Task struct {
				ID           string `json:"id"`
				Project      string `json:"project"`
				Title        string `json:"title"`
				Description  string `json:"description"`
				Acceptance   string `json:"acceptance"`
				State        string `json:"state"`
				MilestoneID  string `json:"milestone_id"`
				Priority     int    `json:"priority"`
				Dependencies []struct {
					ID string `json:"id"`
				} `json:"dependencies"`
			} `json:"task"`
		}
		if err := c.get(ctx, "/v1/tasks/"+url.PathEscape(receipt.Tasks[item.Key])+"?events_limit=1", &opened); err != nil {
			return fmt.Errorf("task %q: %w", item.Key, err)
		}
		state := item.State
		if state == "" {
			state = "ready"
		}
		milestone := ""
		if item.MilestoneKey != "" {
			milestone = receipt.Milestones[item.MilestoneKey]
		}
		if opened.Task.ID != receipt.Tasks[item.Key] || opened.Task.Project != m.Project.ID || opened.Task.Title != item.Title || opened.Task.Description != item.Description || opened.Task.Acceptance != item.Acceptance || opened.Task.State != state || opened.Task.Priority != item.Priority || opened.Task.MilestoneID != milestone {
			return fmt.Errorf("task %q fields do not match manifest", item.Key)
		}
		got := make([]string, len(opened.Task.Dependencies))
		for i, v := range opened.Task.Dependencies {
			got[i] = v.ID
		}
		want := make([]string, len(item.DependencyKeys))
		for i, v := range item.DependencyKeys {
			want[i] = receipt.Tasks[v]
		}
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			return fmt.Errorf("task %q dependencies do not match manifest", item.Key)
		}
	}
	for _, item := range m.WikiPages {
		var opened struct {
			Page struct {
				ID      string `json:"id"`
				Slug    string `json:"slug"`
				Title   string `json:"title"`
				Summary string `json:"summary"`
				Body    string `json:"body"`
			} `json:"page"`
		}
		if err := c.get(ctx, "/v1/projects/"+url.PathEscape(m.Project.ID)+"/wiki/"+url.PathEscape(item.Slug), &opened); err != nil {
			return fmt.Errorf("wiki %q: %w", item.Key, err)
		}
		if opened.Page.ID != receipt.WikiPages[item.Key] || opened.Page.Slug != item.Slug || opened.Page.Title != item.Title || opened.Page.Summary != item.Summary || opened.Page.Body != item.Body {
			return fmt.Errorf("wiki %q fields do not match manifest", item.Key)
		}
	}
	for _, item := range m.Posts {
		var opened struct {
			Post struct {
				ID         string `json:"id"`
				Kind       string `json:"kind"`
				Title      string `json:"title"`
				Body       string `json:"body"`
				Basis      string `json:"basis"`
				RelatedRef string `json:"related_ref"`
				Topic      struct {
					ID string `json:"id"`
				} `json:"topic"`
				Attachments []Attachment `json:"attachments"`
			} `json:"post"`
		}
		if err := c.get(ctx, "/v1/posts/"+url.PathEscape(receipt.Posts[item.Key])+"?comments_limit=1", &opened); err != nil {
			return fmt.Errorf("post %q: %w", item.Key, err)
		}
		ref := ""
		if item.TaskKey != "" {
			ref = receipt.Tasks[item.TaskKey]
		}
		if opened.Post.ID != receipt.Posts[item.Key] || opened.Post.Kind != item.Kind || opened.Post.Title != item.Title || opened.Post.Body != item.Body || opened.Post.Basis != item.Basis || opened.Post.RelatedRef != ref || opened.Post.Topic.ID != m.Project.ID || !attachmentsEqual(opened.Post.Attachments, item.Attachments) {
			return fmt.Errorf("post %q fields do not match manifest", item.Key)
		}
	}
	return nil
}

func attachmentsEqual(a, b []Attachment) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
