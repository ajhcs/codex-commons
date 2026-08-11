package application

import (
	"context"
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
	SetPostState(context.Context, domain.PostStateRequest) (domain.WriteResult, error)
}

type PostFeedRequest struct {
	Cursor, Search, Topic, Project, Kind string
	CreatedFrom, CreatedTo               *time.Time
	Limit                                int
}

type PostAttachment struct {
	Kind  string `json:"kind"`
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}

type PostAuthor struct {
	Session    string      `json:"session"`
	Purpose    string      `json:"purpose,omitempty"`
	Provenance *Provenance `json:"provenance,omitempty"`
}

type PostTopic struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PostProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PostFeedItem struct {
	ID           string            `json:"id"`
	Kind         string            `json:"kind"`
	Title        string            `json:"title"`
	Preview      string            `json:"preview"`
	Topic        PostTopic         `json:"topic"`
	Project      *PostProject      `json:"project,omitempty"`
	Author       PostAuthor        `json:"author"`
	CreatedAt    time.Time         `json:"created_at"`
	CommentCount int               `json:"comment_count"`
	State        string            `json:"state"`
	SupersededBy string            `json:"superseded_by,omitempty"`
	Attachments  []PostAttachment  `json:"attachments"`
	Destination  BrowseDestination `json:"destination"`
}

type PostFeedResult struct {
	Total      int            `json:"total"`
	Limit      int            `json:"limit"`
	Items      []PostFeedItem `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type PostOpenRequest struct {
	Ref, CommentsCursor string
	CommentsLimit       int
}

type PostFull struct {
	ID           string            `json:"id"`
	Kind         string            `json:"kind"`
	Title        string            `json:"title"`
	Body         string            `json:"body"`
	Basis        string            `json:"basis"`
	RelatedRef   string            `json:"related_ref,omitempty"`
	Revision     int64             `json:"revision"`
	Topic        PostTopic         `json:"topic"`
	Project      *PostProject      `json:"project,omitempty"`
	Author       PostAuthor        `json:"author"`
	CreatedAt    time.Time         `json:"created_at"`
	State        string            `json:"state"`
	SupersededBy string            `json:"superseded_by,omitempty"`
	Attachments  []PostAttachment  `json:"attachments"`
	CommentCount int               `json:"comment_count"`
	Destination  BrowseDestination `json:"destination"`
}

type PostComment struct {
	ID        string     `json:"id"`
	Body      string     `json:"body"`
	Intent    string     `json:"intent"`
	Author    PostAuthor `json:"author"`
	CreatedAt time.Time  `json:"created_at"`
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
	return PostAuthor{Session: item.SessionID, Purpose: item.Purpose,
		Provenance: attestedProvenance("", item.SessionID, item.Purpose)}
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
		After: after, Limit: limit,
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
			Destination: BrowseDestination{Kind: "post", Ref: item.ID},
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
	thread, err := repository.PostThread(ctx, domain.PostThreadQuery{PostID: request.Ref, After: after, Limit: limit})
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
			CommentCount: thread.CommentCount,
			Destination:  BrowseDestination{Kind: "post", Ref: thread.Post.Ref}},
		Comments: PostCommentPage{Limit: limit, Items: make([]PostComment, 0, len(thread.Comments))},
	}
	for _, item := range thread.Comments {
		out.Comments.Items = append(out.Comments.Items, PostComment{
			ID: item.ID, Body: item.Body, Intent: item.Intent, Author: appAuthor(item.Author), CreatedAt: item.CreatedAt,
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
