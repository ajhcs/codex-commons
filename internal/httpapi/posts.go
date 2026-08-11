package httpapi

import (
	"errors"
	"net/url"
	"strings"
)

func validPostAttachment(item PostAttachment) bool {
	if item.Kind != "link" && item.Kind != "github" && item.Kind != "image" && item.Kind != "video" ||
		item.URL == "" || len(item.URL) > 2048 || len(item.Title) > 200 ||
		strings.TrimSpace(item.URL) != item.URL || strings.TrimSpace(item.Title) != item.Title {
		return false
	}
	parsed, err := url.Parse(item.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	return item.Kind != "github" || strings.EqualFold(parsed.Hostname(), "github.com")
}

func parsePostFeedQuery(values url.Values) (PostFeedQuery, error) {
	limit, err := intParam(values.Get("limit"), 20, 1, 50)
	if err != nil {
		return PostFeedQuery{}, err
	}
	from, err := optionalTimeParam(values.Get("created_from"))
	if err != nil {
		return PostFeedQuery{}, err
	}
	to, err := optionalTimeParam(values.Get("created_to"))
	if err != nil {
		return PostFeedQuery{}, err
	}
	if from != nil && to != nil && from.After(*to) {
		return PostFeedQuery{}, errors.New("created_from must not be after created_to")
	}
	search := strings.TrimSpace(values.Get("q"))
	if len(search) > 200 {
		return PostFeedQuery{}, errors.New("q maximum length is 200")
	}
	kind := values.Get("kind")
	if kind != "" && !validPostKind(kind) {
		return PostFeedQuery{}, errors.New("unsupported post kind")
	}
	return PostFeedQuery{
		Cursor: values.Get("cursor"), Search: search, Topic: values.Get("topic"),
		Project: values.Get("project"), Kind: kind, CreatedFrom: from, CreatedTo: to, Limit: limit,
	}, nil
}
