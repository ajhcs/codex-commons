package application

import (
	"context"
	"sort"
	"strings"
	"time"

	"codex-commons/internal/domain"
)

const (
	defaultPostFeedLimit = 20
	maxPostFeedLimit     = 50
	defaultCommentLimit  = 10
	maxCommentLimit      = 20
)

type PostRepository interface {
	PostBrowseSnapshot(context.Context, domain.PostBrowseQuery) (domain.PostBrowseSnapshot, error)
	PostThread(context.Context, domain.PostThreadQuery) (domain.PostThread, error)
	PostCommentByID(context.Context, string, string, string) (string, domain.PostComment, error)
	SetPostState(context.Context, domain.PostStateRequest) (domain.WriteResult, error)
}

type AddressabilityRepository interface {
	Contributors(context.Context, domain.ContributorQuery) ([]domain.Contributor, error)
	SetPerspectiveScope(context.Context, domain.PerspectiveScopeRequest) (domain.WriteResult, error)
	Comment(context.Context, domain.CommentRequest) (domain.WriteResult, error)
}

type PostFeedRequest struct {
	ViewerKind, ViewerPrincipal, ViewerSession string
	Cursor, Search, Topic, Project, Kind       string
	CreatedFrom, CreatedTo                     *time.Time
	Limit                                      int
}

type PostAttachment struct {
	Kind  string `json:"kind"`
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

type PostAuthor struct {
	Handle     string      `json:"handle,omitempty"`
	Session    string      `json:"session,omitempty"`
	Purpose    string      `json:"purpose,omitempty"`
	Provenance *Provenance `json:"provenance,omitempty"`
}

type MentionTarget struct {
	Kind        string      `json:"kind"`
	Principal   string      `json:"principal"`
	Session     string      `json:"session,omitempty"`
	Handle      string      `json:"handle,omitempty"`
	DisplayName string      `json:"display_name,omitempty"`
	Purpose     string      `json:"purpose,omitempty"`
	Provenance  *Provenance `json:"provenance,omitempty"`
}

type PostTopic struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PostProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PerspectiveScope struct {
	Value    string `json:"value"`
	Revision int64  `json:"revision"`
}

type PostFeedItem struct {
	ID               string            `json:"id"`
	Kind             string            `json:"kind"`
	Title            string            `json:"title"`
	Preview          string            `json:"preview"`
	Topic            PostTopic         `json:"topic"`
	Project          *PostProject      `json:"project,omitempty"`
	Author           PostAuthor        `json:"author"`
	CreatedAt        time.Time         `json:"created_at"`
	CommentCount     int               `json:"comment_count"`
	State            string            `json:"state"`
	SupersededBy     string            `json:"superseded_by,omitempty"`
	Attachments      []PostAttachment  `json:"attachments"`
	Destination      BrowseDestination `json:"destination"`
	PerspectiveScope PerspectiveScope  `json:"perspective_scope"`
	Mentions         []MentionTarget   `json:"mentions"`
}

type PostFeedResult struct {
	Total      int            `json:"total"`
	Limit      int            `json:"limit"`
	Items      []PostFeedItem `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type PostOpenRequest struct {
	Ref, CommentsCursor                        string
	CommentsLimit                              int
	ViewerKind, ViewerPrincipal, ViewerSession string
}

type PostFull struct {
	ID               string            `json:"id"`
	Kind             string            `json:"kind"`
	Title            string            `json:"title"`
	Body             string            `json:"body"`
	Basis            string            `json:"basis"`
	RelatedRef       string            `json:"related_ref,omitempty"`
	Revision         int64             `json:"revision"`
	Topic            PostTopic         `json:"topic"`
	Project          *PostProject      `json:"project,omitempty"`
	Author           PostAuthor        `json:"author"`
	CreatedAt        time.Time         `json:"created_at"`
	State            string            `json:"state"`
	SupersededBy     string            `json:"superseded_by,omitempty"`
	Attachments      []PostAttachment  `json:"attachments"`
	CommentCount     int               `json:"comment_count"`
	Destination      BrowseDestination `json:"destination"`
	PerspectiveScope PerspectiveScope  `json:"perspective_scope"`
	Mentions         []MentionTarget   `json:"mentions"`
}

type PostComment struct {
	ID        string          `json:"id"`
	Body      string          `json:"body"`
	Intent    string          `json:"intent"`
	Author    PostAuthor      `json:"author"`
	CreatedAt time.Time       `json:"created_at"`
	Mentions  []MentionTarget `json:"mentions"`
}

type PostCommentPage struct {
	Limit      int           `json:"limit"`
	Items      []PostComment `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type PostOpenResult struct {
	Post     PostFull        `json:"post"`
	Comments PostCommentPage `json:"comments"`
}

type PostStateRequest struct {
	Ref, State, SupersededBy  string
	Actor, Session, RequestID string
}

type PerspectiveScopeRequest struct {
	Ref, Scope, Actor, Session, RequestID string
	BaseRevision                          int64
}

type CommentRequest struct {
	Ref, Body, Intent                                    string
	Actor, ActorKind, ActorPrincipal, Session, RequestID string
	MentionSessionIDs, MentionPrincipals                 []string
}

func postFeedLimit(value int) (int, bool) {
	if value == 0 {
		return defaultPostFeedLimit, true
	}
	return value, value >= 1 && value <= maxPostFeedLimit
}

func commentLimit(value int) (int, bool) {
	if value == 0 {
		return defaultCommentLimit, true
	}
	return value, value >= 1 && value <= maxCommentLimit
}

func appAttachment(item domain.PostAttachment) PostAttachment {
	return PostAttachment{Kind: item.Kind, URL: item.URL, Title: item.Title}
}

func appAttachments(items []domain.PostAttachment) []PostAttachment {
	out := make([]PostAttachment, 0, len(items))
	for _, item := range items {
		out = append(out, appAttachment(item))
	}
	return out
}

func appTopic(item domain.PostTopic) PostTopic {
	return PostTopic{ID: item.ID, Name: item.Name}
}

func appProject(item *domain.PostProject) *PostProject {
	if item == nil {
		return nil
	}
	return &PostProject{ID: item.ID, Name: item.Name}
}

func appAuthor(item domain.PostAuthor) PostAuthor {
	return PostAuthor{Handle: item.Handle, Session: item.SessionID, Purpose: item.Purpose,
		Provenance: attestedProvenance("", item.SessionID, item.Purpose)}
}

func (s *Service) appMentions(items []domain.MentionTarget) []MentionTarget {
	out := make([]MentionTarget, 0, len(items))
	for _, item := range items {
		target := MentionTarget{Kind: item.Kind, Principal: item.Principal, Session: item.SessionID, Handle: item.Handle, Purpose: item.Purpose}
		if item.Kind == "human" {
			target.Handle = s.humanHandle
			target.DisplayName = s.humanDisplayName
		} else {
			target.Provenance = attestedProvenance("", item.SessionID, item.Purpose)
		}
		out = append(out, target)
	}
	return out
}

func (s *Service) BrowsePosts(ctx context.Context, request PostFeedRequest) (PostFeedResult, error) {
	if s == nil {
		return PostFeedResult{}, domain.ErrInvalid
	}
	repository, ok := s.repository.(PostRepository)
	if !ok || len(request.Search) > 200 || strings.TrimSpace(request.Search) != request.Search ||
		request.Kind != "" && !domain.PostKinds[request.Kind] ||
		request.CreatedFrom != nil && request.CreatedTo != nil && request.CreatedFrom.After(*request.CreatedTo) {
		return PostFeedResult{}, domain.ErrInvalid
	}
	limit, ok := postFeedLimit(request.Limit)
	if !ok {
		return PostFeedResult{}, domain.ErrInvalid
	}
	after, err := decodeCursor("posts", request.Cursor)
	if err != nil || after != nil && after.Time.IsZero() {
		return PostFeedResult{}, domain.ErrInvalid
	}
	snapshot, err := repository.PostBrowseSnapshot(ctx, domain.PostBrowseQuery{
		Filters: domain.PostFilters{Search: request.Search, TopicID: request.Topic,
			ProjectID: request.Project, Kind: request.Kind,
			CreatedFrom: request.CreatedFrom, CreatedTo: request.CreatedTo},
		After: after, Limit: limit, ViewerKind: request.ViewerKind, ViewerPrincipal: request.ViewerPrincipal, ViewerSession: request.ViewerSession,
	})
	if err != nil {
		return PostFeedResult{}, err
	}
	hasMore := len(snapshot.Items) > limit
	if hasMore {
		snapshot.Items = snapshot.Items[:limit]
	}
	out := PostFeedResult{Total: snapshot.Total, Limit: limit, Items: make([]PostFeedItem, 0, len(snapshot.Items))}
	for _, item := range snapshot.Items {
		out.Items = append(out.Items, PostFeedItem{
			ID: item.ID, Kind: item.Kind, Title: item.Title, Preview: item.Preview,
			Topic: appTopic(item.Topic), Project: appProject(item.Project), Author: appAuthor(item.Author),
			CreatedAt: item.CreatedAt, CommentCount: item.CommentCount, State: item.State,
			SupersededBy: item.SupersededBy, Attachments: appAttachments(item.Attachments),
			Destination:      BrowseDestination{Kind: "post", Ref: item.ID},
			PerspectiveScope: PerspectiveScope{Value: item.PerspectiveScope.Scope, Revision: item.PerspectiveScope.Revision},
			Mentions:         s.appMentions(item.Mentions),
		})
	}
	if hasMore && len(snapshot.Items) > 0 {
		last := snapshot.Items[len(snapshot.Items)-1]
		out.NextCursor = encodeCursor("posts", domain.BrowseCursor{Time: last.CreatedAt, ID: last.ID})
	}
	return out, nil
}

func (s *Service) OpenPost(ctx context.Context, request PostOpenRequest) (PostOpenResult, error) {
	if s == nil {
		return PostOpenResult{}, domain.ErrInvalid
	}
	repository, ok := s.repository.(PostRepository)
	if !ok || request.Ref == "" {
		return PostOpenResult{}, domain.ErrInvalid
	}
	limit, ok := commentLimit(request.CommentsLimit)
	if !ok {
		return PostOpenResult{}, domain.ErrInvalid
	}
	resource := "post-comments:" + request.Ref
	after, err := decodeCursor(resource, request.CommentsCursor)
	if err != nil || after != nil && after.Time.IsZero() {
		return PostOpenResult{}, domain.ErrInvalid
	}
	thread, err := repository.PostThread(ctx, domain.PostThreadQuery{PostID: request.Ref, After: after, Limit: limit, ViewerKind: request.ViewerKind, ViewerPrincipal: request.ViewerPrincipal, ViewerSession: request.ViewerSession})
	if err != nil {
		return PostOpenResult{}, err
	}
	hasMore := len(thread.Comments) > limit
	if hasMore {
		thread.Comments = thread.Comments[:limit]
	}
	out := PostOpenResult{
		Post: PostFull{ID: thread.Post.Ref, Kind: thread.Post.Kind, Title: thread.Post.Title,
			Body: thread.Post.Body, Basis: thread.Post.Basis, RelatedRef: thread.Post.RelatedRef,
			Revision: thread.Post.Revision, Topic: appTopic(thread.Topic), Project: appProject(thread.Project),
			Author: appAuthor(thread.Author), CreatedAt: thread.Post.CreatedAt, State: thread.State,
			SupersededBy: thread.SupersededBy, Attachments: appAttachments(thread.Attachments),
			CommentCount:     thread.CommentCount,
			Destination:      BrowseDestination{Kind: "post", Ref: thread.Post.Ref},
			PerspectiveScope: PerspectiveScope{Value: thread.PerspectiveScope.Scope, Revision: thread.PerspectiveScope.Revision},
			Mentions:         s.appMentions(thread.Mentions)},
		Comments: PostCommentPage{Limit: limit, Items: make([]PostComment, 0, len(thread.Comments))},
	}
	for _, item := range thread.Comments {
		out.Comments.Items = append(out.Comments.Items, PostComment{
			ID: item.ID, Body: item.Body, Intent: item.Intent, Author: appAuthor(item.Author), CreatedAt: item.CreatedAt,
			Mentions: s.appMentions(item.Mentions),
		})
	}
	if hasMore && len(thread.Comments) > 0 {
		last := thread.Comments[len(thread.Comments)-1]
		out.Comments.NextCursor = encodeCursor(resource, domain.BrowseCursor{Time: last.CreatedAt, ID: last.ID})
	}
	return out, nil
}

func (s *Service) SetPostState(ctx context.Context, request PostStateRequest) (domain.WriteResult, error) {
	if s == nil {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	repository, ok := s.repository.(PostRepository)
	if !ok {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	return repository.SetPostState(ctx, domain.PostStateRequest{
		PostID: request.Ref, State: request.State, SupersededBy: request.SupersededBy,
		ActorID: request.Actor, SessionID: request.Session, RequestID: request.RequestID,
	})
}

type ContributorLookupRequest struct {
	Cursor, Search, Project string
	Limit                   int
}
type ContributorProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type ContributorItem struct {
	Kind                string              `json:"kind"`
	Principal           string              `json:"principal"`
	DisplayName         string              `json:"display_name,omitempty"`
	Handle              string              `json:"handle"`
	Session             string              `json:"session,omitempty"`
	Purpose             string              `json:"purpose,omitempty"`
	Host                string              `json:"host,omitempty"`
	Project             *ContributorProject `json:"project,omitempty"`
	ProjectRelationship string              `json:"project_relationship"`
	Addressable         bool                `json:"addressable"`
	Reachable           bool                `json:"reachable"`
	Interpretation      string              `json:"interpretation"`
	HostConnected       bool                `json:"host_connected"`
	Execution           string              `json:"execution"`
	LastActivity        *time.Time          `json:"last_activity,omitempty"`
	Provenance          *Provenance         `json:"provenance,omitempty"`
}
type ContributorLookupResult struct {
	Limit      int               `json:"limit"`
	Items      []ContributorItem `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

func (s *Service) LookupContributors(ctx context.Context, request ContributorLookupRequest) (ContributorLookupResult, error) {
	repository, ok := s.repository.(AddressabilityRepository)
	if !ok || s.presence == nil || len(request.Search) > 100 || strings.TrimSpace(request.Search) != request.Search {
		return ContributorLookupResult{}, domain.ErrInvalid
	}
	limit := request.Limit
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 20 {
		return ContributorLookupResult{}, domain.ErrInvalid
	}
	after, err := decodeCursor("contributors", request.Cursor)
	if err != nil || after != nil && after.Text == "" {
		return ContributorLookupResult{}, domain.ErrInvalid
	}
	q := domain.ContributorQuery{Search: request.Search, ProjectID: request.Project, Limit: limit}
	if after != nil {
		q.AfterHandle = after.Text
		q.AfterSessionID = after.ID
	}
	rows, err := repository.Contributors(ctx, q)
	if err != nil {
		return ContributorLookupResult{}, err
	}
	out := ContributorLookupResult{Limit: limit, Items: make([]ContributorItem, 0, len(rows)+1)}
	for _, v := range rows {
		item := ContributorItem{Kind: "agent", Principal: v.SessionID, Handle: v.Handle, Session: v.SessionID, Purpose: v.Purpose, Host: v.Host, Addressable: true, Execution: "not_running", ProjectRelationship: "none", Interpretation: "Addressable registry session; no current reachability evidence.", Provenance: attestedProvenance("", v.SessionID, v.Purpose)}
		if v.ProjectID != "" {
			item.Project = &ContributorProject{ID: v.ProjectID, Name: v.ProjectName}
			item.ProjectRelationship = "project"
			if request.Project == v.ProjectID {
				item.ProjectRelationship = "same_project"
			}
		}
		if p, ok := s.presence.Get(v.SessionID); ok {
			item.HostConnected, item.Execution = p.HostConnected, p.Execution
			t := p.LastActivity
			item.LastActivity = &t
			item.Reachable = p.HostConnected
			if item.Reachable {
				item.Interpretation = "Addressable and currently connected; delivery is not guaranteed."
			}
		}
		out.Items = append(out.Items, item)
	}
	search := strings.ToLower(request.Search)
	humanMatches := search == "" || strings.Contains(strings.ToLower(s.humanHandle), search) || strings.Contains(strings.ToLower(s.humanDisplayName), search) || strings.Contains(domain.HumanLocalPrincipal, search)
	humanAfter := after == nil || strings.ToLower(s.humanHandle) > strings.ToLower(after.Text) || strings.EqualFold(s.humanHandle, after.Text) && domain.HumanLocalPrincipal > after.ID
	if request.Project == "" && humanMatches && humanAfter {
		out.Items = append(out.Items, ContributorItem{Kind: "human", Principal: domain.HumanLocalPrincipal, Handle: s.humanHandle, DisplayName: s.humanDisplayName, Addressable: true, ProjectRelationship: "none", Execution: "not_applicable", Interpretation: "Stable local human principal; browser session state is not recipient identity."})
	}
	sort.Slice(out.Items, func(i, j int) bool {
		left, right := strings.ToLower(out.Items[i].Handle), strings.ToLower(out.Items[j].Handle)
		if left == right {
			return out.Items[i].Principal < out.Items[j].Principal
		}
		return left < right
	})
	more := len(out.Items) > limit
	if more {
		out.Items = out.Items[:limit]
	}
	if more && len(out.Items) > 0 {
		last := out.Items[len(out.Items)-1]
		out.NextCursor = encodeCursor("contributors", domain.BrowseCursor{Text: last.Handle, ID: last.Principal})
	}
	return out, nil
}

func (s *Service) SetPerspectiveScope(ctx context.Context, request PerspectiveScopeRequest) (domain.WriteResult, error) {
	repository, ok := s.repository.(AddressabilityRepository)
	if !ok {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	return repository.SetPerspectiveScope(ctx, domain.PerspectiveScopeRequest{PostID: request.Ref, Scope: request.Scope, BaseRevision: request.BaseRevision, ActorID: request.Actor, SessionID: request.Session, RequestID: request.RequestID})
}
func (s *Service) Comment(ctx context.Context, request CommentRequest) (domain.WriteResult, error) {
	repository, ok := s.repository.(AddressabilityRepository)
	if !ok {
		return domain.WriteResult{}, domain.ErrInvalid
	}
	return repository.Comment(ctx, domain.CommentRequest{PostID: request.Ref, Body: request.Body, Intent: request.Intent, ActorID: request.Actor, ActorKind: request.ActorKind, ActorPrincipal: request.ActorPrincipal, SessionID: request.Session, RequestID: request.RequestID, MentionSessionIDs: request.MentionSessionIDs, MentionPrincipals: request.MentionPrincipals})
}
