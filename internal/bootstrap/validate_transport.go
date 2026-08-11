package bootstrap

import (
	"encoding/json"
	"fmt"
	"strings"
)

const maxWriteRequestBytes = 32 << 10

func validateTransport(m Manifest) []string {
	var problems []string
	check := func(path string, body any) {
		raw, err := json.Marshal(body)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s cannot be encoded", path))
			return
		}
		if len(raw) > maxWriteRequestBytes {
			problems = append(problems, fmt.Sprintf("%s encoded write exceeds %d bytes", path, maxWriteRequestBytes))
		}
	}
	check("project", map[string]any{"id": m.Project.ID, "name": m.Project.Name, "status": m.Project.Status, "purpose": m.Project.Purpose, "now": m.Project.Now})
	for i, v := range m.Milestones {
		check(fmt.Sprintf("milestones[%d]", i), map[string]any{"title": v.Title, "status": v.Status, "position": v.Position, "target_date": v.TargetDate})
	}
	placeholderID := "T-00000000000000000000000000000000"
	for i, v := range m.Tasks {
		deps := make([]string, len(v.DependencyKeys))
		for j := range deps {
			deps[j] = placeholderID
		}
		state := v.State
		if state == "" {
			state = "ready"
		}
		body := map[string]any{"title": v.Title, "description": v.Description, "acceptance": v.Acceptance, "state": state, "priority": v.Priority, "dependency_ids": deps}
		if v.MilestoneKey != "" {
			body["milestone_id"] = "MS-00000000000000000000000000000000"
		}
		check(fmt.Sprintf("tasks[%d]", i), body)
	}
	for i, v := range m.WikiPages {
		check(fmt.Sprintf("wiki_pages[%d]", i), map[string]any{"title": v.Title, "summary": v.Summary, "body": v.Body, "base_revision": 0})
	}
	for i, v := range m.Posts {
		path := fmt.Sprintf("posts[%d]", i)
		for j, a := range v.Attachments {
			if len(a.URL) > 2048 || strings.TrimSpace(a.URL) != a.URL {
				problems = append(problems, fmt.Sprintf("%s.attachments[%d].url must be trimmed and at most 2048 bytes", path, j))
			}
		}
		body := map[string]any{"topic": m.Project.ID, "kind": v.Kind, "title": v.Title, "body": v.Body, "basis": v.Basis, "attachments": v.Attachments}
		if v.TaskKey != "" {
			body["ref"] = placeholderID
		}
		check(path, body)
	}
	return problems
}
