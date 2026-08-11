package bootstrap

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const maxManifestBytes = 1 << 20

var (
	keyPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	projectPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,99}$`)
	slugPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,119}$`)
)

type Manifest struct {
	SchemaVersion int         `json:"schema_version"`
	Namespace     string      `json:"namespace"`
	Project       Project     `json:"project"`
	Milestones    []Milestone `json:"milestones"`
	Tasks         []Task      `json:"tasks"`
	WikiPages     []WikiPage  `json:"wiki_pages"`
	Posts         []Post      `json:"posts"`
	Sources       []Source    `json:"sources"`
}

type Project struct {
	Key, ID, Name, Status, Purpose, Now string
}

func (p *Project) UnmarshalJSON(data []byte) error {
	type wire struct {
		Key     string `json:"key"`
		ID      string `json:"id"`
		Name    string `json:"name"`
		Status  string `json:"status"`
		Purpose string `json:"purpose"`
		Now     string `json:"now"`
	}
	var v wire
	if err := strictUnmarshal(data, &v); err != nil {
		return err
	}
	*p = Project{v.Key, v.ID, v.Name, v.Status, v.Purpose, v.Now}
	return nil
}

type Milestone struct {
	Key        string   `json:"key"`
	Title      string   `json:"title"`
	Status     string   `json:"status"`
	Position   int      `json:"position"`
	TargetDate string   `json:"target_date,omitempty"`
	SourceKeys []string `json:"source_keys,omitempty"`
}

type Task struct {
	Key            string   `json:"key"`
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	Acceptance     string   `json:"acceptance,omitempty"`
	State          string   `json:"state,omitempty"`
	Priority       int      `json:"priority"`
	MilestoneKey   string   `json:"milestone_key,omitempty"`
	DependencyKeys []string `json:"dependency_keys,omitempty"`
	SourceKeys     []string `json:"source_keys,omitempty"`
}

type WikiPage struct {
	Key, Slug, Title, Summary, Body string
	SourceKeys                      []string
}

func (p *WikiPage) UnmarshalJSON(data []byte) error {
	type wire struct {
		Key        string   `json:"key"`
		Slug       string   `json:"slug"`
		Title      string   `json:"title"`
		Summary    string   `json:"summary"`
		Body       string   `json:"body"`
		SourceKeys []string `json:"source_keys,omitempty"`
	}
	var v wire
	if err := strictUnmarshal(data, &v); err != nil {
		return err
	}
	*p = WikiPage{v.Key, v.Slug, v.Title, v.Summary, v.Body, v.SourceKeys}
	return nil
}

type Attachment struct {
	Kind  string `json:"kind"`
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
}
type Post struct {
	Key, Kind, Title, Body, Basis, TaskKey string
	Attachments                            []Attachment
	SourceKeys                             []string
}

func (p *Post) UnmarshalJSON(data []byte) error {
	type wire struct {
		Key         string       `json:"key"`
		Kind        string       `json:"kind"`
		Title       string       `json:"title"`
		Body        string       `json:"body"`
		Basis       string       `json:"basis"`
		TaskKey     string       `json:"task_key,omitempty"`
		Attachments []Attachment `json:"attachments,omitempty"`
		SourceKeys  []string     `json:"source_keys,omitempty"`
	}
	var v wire
	if err := strictUnmarshal(data, &v); err != nil {
		return err
	}
	*p = Post{v.Key, v.Kind, v.Title, v.Body, v.Basis, v.TaskKey, v.Attachments, v.SourceKeys}
	return nil
}

type Source struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	URL   string `json:"url,omitempty"`
	Note  string `json:"note,omitempty"`
}

func DecodeManifest(r io.Reader) (Manifest, error) {
	var m Manifest
	raw, err := io.ReadAll(io.LimitReader(r, maxManifestBytes+1))
	if err != nil {
		return m, fmt.Errorf("read manifest: %w", err)
	}
	if len(raw) > maxManifestBytes {
		return m, fmt.Errorf("manifest exceeds %d bytes", maxManifestBytes)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("decode manifest: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return m, fmt.Errorf("decode manifest: one JSON value required")
	}
	if err := Validate(m); err != nil {
		return m, err
	}
	return m, nil
}

func strictUnmarshal(data []byte, dst any) error {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func Validate(m Manifest) error {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if m.SchemaVersion != 1 {
		add("schema_version must be 1")
	}
	if !keyPattern.MatchString(m.Namespace) {
		add("namespace must match %s", keyPattern)
	}
	if !keyPattern.MatchString(m.Project.Key) {
		add("project.key is invalid")
	}
	if !projectPattern.MatchString(m.Project.ID) || m.Project.ID == "general" {
		add("project.id is invalid")
	}
	bounded(&problems, "project.name", m.Project.Name, 200, true)
	bounded(&problems, "project.purpose", m.Project.Purpose, 4000, true)
	bounded(&problems, "project.now", m.Project.Now, 4000, false)
	if !oneOf(m.Project.Status, "active", "paused", "completed", "archived") {
		add("project.status is invalid")
	}
	if len(m.Milestones) > 10 {
		add("milestones exceeds 10")
	}
	if len(m.Tasks) > 25 {
		add("tasks exceeds 25")
	}
	if len(m.WikiPages) > 20 {
		add("wiki_pages exceeds 20")
	}
	if len(m.Posts) > 50 {
		add("posts exceeds 50")
	}
	if len(m.Sources) > 100 {
		add("sources exceeds 100")
	}
	sources := map[string]bool{}
	for i, v := range m.Sources {
		path := fmt.Sprintf("sources[%d]", i)
		uniqueKey(&problems, sources, path, v.Key)
		bounded(&problems, path+".title", v.Title, 300, true)
		bounded(&problems, path+".note", v.Note, 2000, false)
		if v.URL != "" {
			u, err := url.Parse(v.URL)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
				add("%s.url must be an HTTPS URL without credentials", path)
			}
		}
	}
	checkSources := func(path string, keys []string) {
		seen := map[string]bool{}
		for _, key := range keys {
			if seen[key] {
				add("%s.source_keys repeats %q", path, key)
			}
			seen[key] = true
			if !sources[key] {
				add("%s.source_keys references unknown %q", path, key)
			}
		}
	}
	milestones := map[string]bool{}
	active := 0
	for i, v := range m.Milestones {
		path := fmt.Sprintf("milestones[%d]", i)
		uniqueKey(&problems, milestones, path, v.Key)
		bounded(&problems, path+".title", v.Title, 300, true)
		if !oneOf(v.Status, "planned", "active", "completed", "cancelled") {
			add("%s.status is invalid", path)
		}
		if v.Status == "active" {
			active++
		}
		if v.Position < 0 || v.Position > 100000 {
			add("%s.position must be 0..100000", path)
		}
		if v.TargetDate != "" {
			parsed, err := time.Parse(time.DateOnly, v.TargetDate)
			if err != nil || parsed.Format(time.DateOnly) != v.TargetDate {
				add("%s.target_date must be a real YYYY-MM-DD date", path)
			}
		}
		checkSources(path, v.SourceKeys)
	}
	if active > 1 {
		add("at most one milestone may be active")
	}
	tasks := map[string]bool{}
	for i, v := range m.Tasks {
		path := fmt.Sprintf("tasks[%d]", i)
		uniqueKey(&problems, tasks, path, v.Key)
		bounded(&problems, path+".title", v.Title, 300, true)
		bounded(&problems, path+".description", v.Description, 12000, false)
		bounded(&problems, path+".acceptance", v.Acceptance, 4000, false)
		if v.State != "" && !oneOf(v.State, "ready", "blocked") {
			add("%s.state must be ready or blocked", path)
		}
		if v.Priority < -1000 || v.Priority > 1000 {
			add("%s.priority must be -1000..1000", path)
		}
		checkSources(path, v.SourceKeys)
	}
	for i, v := range m.Tasks {
		path := fmt.Sprintf("tasks[%d]", i)
		if v.MilestoneKey != "" && !milestones[v.MilestoneKey] {
			add("%s.milestone_key references unknown %q", path, v.MilestoneKey)
		}
		if len(v.DependencyKeys) > 20 {
			add("%s.dependency_keys exceeds 20", path)
		}
		seen := map[string]bool{}
		for _, d := range v.DependencyKeys {
			if d == v.Key {
				add("%s cannot depend on itself", path)
			}
			if seen[d] {
				add("%s.dependency_keys repeats %q", path, d)
			}
			seen[d] = true
			if !tasks[d] {
				add("%s.dependency_keys references unknown %q", path, d)
			}
		}
	}
	if _, err := taskOrder(m.Tasks); err != nil {
		add("%v", err)
	}
	wikis := map[string]bool{}
	slugs := map[string]bool{}
	for i, v := range m.WikiPages {
		path := fmt.Sprintf("wiki_pages[%d]", i)
		uniqueKey(&problems, wikis, path, v.Key)
		if !slugPattern.MatchString(v.Slug) {
			add("%s.slug is invalid", path)
		}
		if slugs[v.Slug] {
			add("%s.slug duplicates %q", path, v.Slug)
		}
		slugs[v.Slug] = true
		bounded(&problems, path+".title", v.Title, 300, true)
		bounded(&problems, path+".summary", v.Summary, 1000, true)
		bounded(&problems, path+".body", v.Body, 24000, true)
		checkSources(path, v.SourceKeys)
	}
	posts := map[string]bool{}
	for i, v := range m.Posts {
		path := fmt.Sprintf("posts[%d]", i)
		uniqueKey(&problems, posts, path, v.Key)
		if !oneOf(v.Kind, "finding", "decision", "notice", "question", "topic_request") {
			add("%s.kind is invalid", path)
		}
		if v.Kind == "topic_request" {
			add("%s topic_request cannot target a project topic", path)
		}
		bounded(&problems, path+".title", v.Title, 200, true)
		bounded(&problems, path+".body", v.Body, 24000, true)
		bounded(&problems, path+".basis", v.Basis, 4000, true)
		if v.TaskKey != "" && !tasks[v.TaskKey] {
			add("%s.task_key references unknown %q", path, v.TaskKey)
		}
		if len(v.Attachments) > 8 {
			add("%s.attachments exceeds 8", path)
		}
		for j, a := range v.Attachments {
			ap := fmt.Sprintf("%s.attachments[%d]", path, j)
			if !oneOf(a.Kind, "link", "github", "image", "video") {
				add("%s.kind is invalid", ap)
			}
			u, err := url.Parse(a.URL)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
				add("%s.url must be an HTTPS URL without credentials or fragment", ap)
			}
			if a.Kind == "github" && !strings.EqualFold(u.Hostname(), "github.com") {
				add("%s github URL must use github.com", ap)
			}
			bounded(&problems, ap+".title", a.Title, 200, false)
		}
		checkSources(path, v.SourceKeys)
	}
	problems = append(problems, validateTransport(m)...)
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("manifest validation failed:\n- %s", strings.Join(problems, "\n- "))
	}
	return nil
}

func bounded(problems *[]string, path, value string, max int, required bool) {
	if required && strings.TrimSpace(value) == "" {
		*problems = append(*problems, path+" is required")
		return
	}
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		*problems = append(*problems, path+" must be valid UTF-8 without NUL")
	}
	if value != strings.TrimSpace(value) {
		*problems = append(*problems, path+" must not have surrounding whitespace")
	}
	if len(value) > max {
		*problems = append(*problems, fmt.Sprintf("%s exceeds %d bytes", path, max))
	}
}
func uniqueKey(problems *[]string, seen map[string]bool, path, key string) {
	if !keyPattern.MatchString(key) {
		*problems = append(*problems, path+".key is invalid")
	}
	if seen[key] {
		*problems = append(*problems, fmt.Sprintf("%s.key duplicates %q", path, key))
	}
	seen[key] = true
}
func oneOf(value string, allowed ...string) bool {
	for _, v := range allowed {
		if value == v {
			return true
		}
	}
	return false
}

func taskOrder(tasks []Task) ([]Task, error) {
	byKey := map[string]Task{}
	indegree := map[string]int{}
	dependents := map[string][]string{}
	for _, task := range tasks {
		byKey[task.Key] = task
		indegree[task.Key] = len(task.DependencyKeys)
		for _, d := range task.DependencyKeys {
			dependents[d] = append(dependents[d], task.Key)
		}
	}
	var ready []string
	for key, degree := range indegree {
		if degree == 0 {
			ready = append(ready, key)
		}
	}
	sort.Strings(ready)
	var out []Task
	for len(ready) > 0 {
		key := ready[0]
		ready = ready[1:]
		out = append(out, byKey[key])
		sort.Strings(dependents[key])
		for _, child := range dependents[key] {
			indegree[child]--
			if indegree[child] == 0 {
				ready = append(ready, child)
				sort.Strings(ready)
			}
		}
	}
	if len(out) != len(tasks) {
		return nil, fmt.Errorf("task dependency graph contains a cycle")
	}
	return out, nil
}
