package apiclient

import (
	"context"

	"codex-commons/internal/application"
)

// ProjectArchaeology returns the durable human review and exported task-pack
// state. Agent credentials may read it, but human-only controls still reject
// agent writes at the server boundary.
func (c *Client) ProjectArchaeology(ctx context.Context) (application.ArchaeologySession, error) {
	var out application.ArchaeologySession
	err := c.get(ctx, "/v1/project-archaeology", nil, "", &out)
	return out, err
}

// ClaimProjectArchaeologyHandoff binds the exact server-attested agent session
// represented by this client credential to an exported task pack.
func (c *Client) ClaimProjectArchaeologyHandoff(ctx context.Context, handoffID, key string) (application.ArchaeologySession, error) {
	var out application.ArchaeologySession
	err := c.post(ctx, "/v1/project-archaeology/handoff/claim", application.ArchaeologyHandoffClaimRequest{HandoffID: handoffID}, key, &out)
	return out, err
}

// ReportProjectArchaeologyHandoff accepts only the exact claiming session and
// creates review proposals; it never applies the embedded historical import.
func (c *Client) ReportProjectArchaeologyHandoff(ctx context.Context, request application.ArchaeologyHandoffReportEnvelope, key string) (application.ArchaeologySession, error) {
	var out application.ArchaeologySession
	err := c.post(ctx, "/v1/project-archaeology/handoff/report", request, key, &out)
	return out, err
}
