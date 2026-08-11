import { useRef, useState } from "react";
import { AppShell, Notice } from "../components/AppShell.jsx";
import { LoginDialog } from "../components/AuthControls.jsx";
import { SearchField, Select, Timestamp } from "../components/Controls.jsx";
import { PostComposer } from "../components/PostComposer.jsx";
import { PostFeedRow, PostReader, postKindOptions } from "../components/PostParts.jsx";
import { ProjectPostsBar } from "../components/ProjectPostsBar.jsx";
import { commonsAdapter } from "../data/adapter.js";
import { useAuthSession } from "../hooks/useAuthSession.js";
import { useNotifications } from "../hooks/NotificationContext.jsx";
import { useCursorPager } from "../hooks/useCursorPager.js";
import { useResource } from "../hooks/useResource.js";
import FileDocument from "../icons/FileDocument.tsx";
import Plus from "../icons/Plus.tsx";

function PostsRail({ topics, topicsTruncated, selectedTopic, onSelectTopic, onNewPost }) {
  return (
    <>
      <section className="rail-topics" aria-labelledby="rail-topics-title">
        <h2 id="rail-topics-title">Topics</h2>
        <button
          type="button"
          className={selectedTopic === "" ? "is-current" : ""}
          aria-pressed={selectedTopic === ""}
          onClick={() => onSelectTopic("")}
        >
          <FileDocument aria-hidden="true" />
          <span>All posts</span>
        </button>
        {topics.map((topic) => (
          <button
            key={topic.value}
            type="button"
            className={selectedTopic === topic.value ? "is-current" : ""}
            aria-pressed={selectedTopic === topic.value}
            onClick={() => onSelectTopic(topic.value)}
          >
            <FileDocument aria-hidden="true" />
            <span>{topic.label}</span>
          </button>
        ))}
        {topicsTruncated ? <p className="rail-topics-note">Showing the first 100 topics.</p> : null}
      </section>
      <button className="rail-new-post" type="button" onClick={onNewPost}>
        <Plus aria-hidden="true" />
        <span>New post</span>
      </button>
    </>
  );
}

function NotificationBand({ active, onClose }) {
  if (!active) return null;
  const { notification } = active;
  const actorName = notification.actor.displayName || notification.actor.purpose || notification.actor.handle || "Contributor";
  return (
    <section className={`notification-band${active.status === "error" ? " notification-band--error" : ""}`} aria-label="Opened mention" aria-live="polite">
      <span className={`notification-unread-dot${notification.readAt ? " is-read" : ""}`} aria-hidden="true" />
      <div className="notification-band-copy">
        <p>
          <strong>{actorName}</strong>
          {notification.actor.handle ? <span>@{notification.actor.handle}</span> : null}
          <span>wrote</span>
        </p>
        <blockquote>{notification.snippet}</blockquote>
        {active.status === "error" ? <small>{active.message}</small> : null}
      </div>
      <Timestamp value={notification.created} compact />
      <button type="button" onClick={onClose}>Close</button>
    </section>
  );
}

export function PostsScreen({ selectedPostID = "", onNavigate, projectID = "", projectInfo = null, onProjectNavigate = null }) {
  const auth = useAuthSession();
  const notifications = useNotifications();
  const resumeAfterLoginRef = useRef(null);
  const [filters, setFilters] = useState({ q: "", topic: "", kind: "" });
  const [composerOpen, setComposerOpen] = useState(false);
  const [loginOpen, setLoginOpen] = useState(false);
  const [notice, setNotice] = useState("");
  const [refreshKey, setRefreshKey] = useState(0);
  const pager = useCursorPager(10);
  const resource = useResource(
    (signal) => commonsAdapter.readPosts({
      q: filters.q,
      topic: filters.topic,
      project: projectID,
      kind: filters.kind,
      created_from: "",
      created_to: "",
      cursor: pager.cursor,
      limit: pager.limit,
    }, signal),
    [filters.q, filters.topic, filters.kind, projectID, pager.cursor, pager.limit, refreshKey],
  );
  const topicsResource = useResource(
    (signal) => commonsAdapter.readTopics(100, signal),
    [],
  );
  const data = resource.data;
  const items = data?.items || [];
  const activePostID = selectedPostID || items[0]?.id || "";
  const availableTopics = topicsResource.data?.items || [{ id: "general", name: "General", projectID: "" }];
  const topics = (projectID ? availableTopics.filter((topic) => topic.projectID === projectID) : availableTopics)
    .map((topic) => ({ value: topic.id, label: topic.name, projectID: topic.projectID || "" }));

  function navigatePosts(postID = "") {
    if (projectID) onProjectNavigate?.("posts", postID);
    else onNavigate(postID ? "post" : "posts", postID);
  }

  function updateFilter(key, value) {
    setFilters((current) => ({ ...current, [key]: value }));
    pager.reset();
    if (selectedPostID) navigatePosts();
  }

  function published(postID) {
    setComposerOpen(false);
    setNotice(postID ? `Post published as ${postID}.` : "Post published.");
    setRefreshKey((value) => value + 1);
    if (postID) navigatePosts(postID);
  }

  function requestWriting(resume) {
    resumeAfterLoginRef.current = resume || null;
    setLoginOpen(true);
  }

  function startNewPost() {
    if (auth.session?.authenticated) setComposerOpen(true);
    else requestWriting(() => setComposerOpen(true));
  }

  function authenticated(session) {
    auth.accept(session);
    setLoginOpen(false);
    const resume = resumeAfterLoginRef.current;
    resumeAfterLoginRef.current = null;
    resume?.();
  }


  function requestAuthFromComposer() {
    setComposerOpen(false);
    requestWriting(() => setComposerOpen(true));
  }

  function postChanged(message) {
    setNotice(message);
    setRefreshKey((value) => value + 1);
  }

  const railContent = (
    <PostsRail
      topics={topics}
      topicsTruncated={Boolean(topicsResource.data?.truncated)}
      selectedTopic={filters.topic}
      onSelectTopic={(topic) => updateFilter("topic", topic)}
      onNewPost={startNewPost}
    />
  );

  return (
    <AppShell
      route={projectID ? "project" : selectedPostID ? "post" : "posts"}
      onNavigate={onNavigate}
      railContent={railContent}
    >
      {projectID ? <ProjectPostsBar projectInfo={projectInfo} onBack={() => onNavigate("projects")} onNavigate={onProjectNavigate} /> : null}
      <section className={`posts-workspace${projectID ? " posts-workspace--project" : ""}${selectedPostID ? " show-reader-mobile" : ""}`}>
        <div className="post-index">
          <header className="post-index-heading">
            <h1>Posts</h1>
            <div className="mobile-write-actions">
              <button type="button" onClick={startNewPost} aria-label="New post"><Plus aria-hidden="true" /></button>
            </div>
          </header>
          <div className="post-index-toolbar">
            <SearchField
              label="Search posts"
              value={filters.q}
              onChange={(value) => updateFilter("q", value)}
              placeholder="Search posts"
            />
            <Select
              label="Topic"
              value={filters.topic}
              onChange={(value) => updateFilter("topic", value)}
              options={topics}
              allLabel="All topics"
            />
            <Select
              label="Kind"
              value={filters.kind}
              onChange={(value) => updateFilter("kind", value)}
              options={postKindOptions}
              allLabel="All kinds"
              compact
            />
          </div>
          <Notice message={notice} onDismiss={() => setNotice("")} />
          {topicsResource.status === "error" ? <div className="index-inline-error" role="status">Topics unavailable. Post reading still works.</div> : null}
          <div className="post-index-date">
            <span>Today</span>
            {data ? <small>{data.total} posts</small> : null}
          </div>
          {resource.status === "loading" && !data ? <div className="index-state">Loading posts…</div> : null}
          {resource.status === "error" ? <div className="index-state index-state--error"><strong>Posts unavailable</strong><span>{resource.error}</span></div> : null}
          {resource.status !== "error" && data && !items.length ? <div className="index-state"><strong>No posts found</strong><span>Try a different search, topic, or kind.</span></div> : null}
          {items.length ? (
            <div className="post-index-list">
              {items.map((post) => (
                <PostFeedRow
                  key={post.id}
                  post={post}
                  selected={activePostID === post.id}
                  onSelect={() => navigatePosts(post.id)}
                />
              ))}
            </div>
          ) : null}
          {data ? (
            <nav className="index-pager" aria-label="Posts pagination">
              <button type="button" onClick={pager.previous} disabled={!pager.canPrevious}>Previous</button>
              <span>Page {pager.page}</span>
              <button type="button" onClick={() => pager.next(data.nextCursor)} disabled={!data.nextCursor}>Next</button>
            </nav>
          ) : null}
        </div>
        <section className="post-reader-plane" aria-label="Selected post">
          <div className="reader-command-bar">
            <button className="reader-new-post" type="button" onClick={startNewPost}>
              <Plus aria-hidden="true" />
              New post
            </button>
          </div>
          <NotificationBand active={notifications.active} onClose={() => {
            notifications.close();
            queueMicrotask(() => document.querySelector(".rail-notifications")?.focus());
          }} />
          {activePostID ? (
            <PostReader
              postID={activePostID}
              onBack={() => navigatePosts()}
              session={auth.session}
              onAuthRequired={(resume) => requestWriting(resume)}
              onChanged={postChanged}
              replacementCandidates={items.filter((item) => item.id !== activePostID)}
              notificationTarget={notifications.active?.notification.source.postRef === activePostID ? notifications.active.notification : null}
              onNotificationOpened={notifications.sourceOpened}
              onNotificationFailed={notifications.sourceFailed}
            />
          ) : (
            <div className="reader-empty">
              <strong>Select a post</strong>
              <span>Choose a post from the chronological index to read its durable content.</span>
            </div>
          )}
        </section>
      </section>
      <PostComposer
        open={composerOpen}
        topics={topics}
        session={auth.session}
        onClose={() => setComposerOpen(false)}
        onPublished={published}
        onAuthRequired={requestAuthFromComposer}
      />
      <LoginDialog open={loginOpen} onClose={() => { setLoginOpen(false); resumeAfterLoginRef.current = null; }} onAuthenticated={authenticated} />
    </AppShell>
  );
}
