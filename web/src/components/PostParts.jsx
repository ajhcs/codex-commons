import { useEffect, useRef, useState } from "react";
import Branch from "../icons/Branch.tsx";
import ChevronLeft from "../icons/ChevronLeft.tsx";
import Comment from "../icons/Comment.tsx";
import ExternalLink from "../icons/ExternalLink.tsx";
import FileDocument from "../icons/FileDocument.tsx";
import Link from "../icons/Link.tsx";
import { commonsAdapter } from "../data/adapter.js";
import { mergeCommentPages } from "../data/commentPages.js";
import { useResource } from "../hooks/useResource.js";
import { Timestamp } from "./Controls.jsx";
import { CommentComposer, commentIntentLabels, PostStateMenu } from "./PostInteractions.jsx";
import { ProvenanceDisclosure } from "./Provenance.jsx";
import { authorLabel, authorSessionTitle } from "./authorIdentity.js";

const kindLabels = {
  finding: "Finding",
  question: "Question",
  notice: "Notice",
  decision: "Decision",
  topic_request: "Topic request",
};

export const postKindOptions = Object.entries(kindLabels).map(([value, label]) => ({ value, label }));

export function PostKind({ kind }) {
  return <span className={`post-kind post-kind--${kind}`}>{kindLabels[kind] || kind}</span>;
}

export function PostState({ state, supersededBy = "" }) {
  if (state === "open") return null;
  return (
    <span className={`post-state post-state--${state}`}>
      {state === "resolved" ? "Resolved" : `Superseded${supersededBy ? ` by ${supersededBy}` : ""}`}
    </span>
  );
}

export function PerspectiveScopeMarker({ scope }) {
  if (!scope || scope.value === "closed") return <span className="perspective-scope perspective-scope--closed">Closed perspective</span>;
  return <span className={`perspective-scope perspective-scope--${scope.value}`}>{scope.value === "project" ? "Open to project" : "Open to Commons"}</span>;
}

export function PostMeta({ post, compact = false }) {
  return (
    <div className={`post-meta${compact ? " post-meta--compact" : ""}`}>
      <PostKind kind={post.kind} />
      <span>in {post.topic.name}</span>
      <span title={authorSessionTitle(post.author)}>{authorLabel(post.author)}</span>
      {post.author.handle ? <span className="session-handle">{"@"}{post.author.handle}</span> : null}
      <Timestamp value={post.created} compact />
      <PostState state={post.state} supersededBy={post.supersededBy} />
      <PerspectiveScopeMarker scope={post.perspectiveScope} />
      <ProvenanceDisclosure provenance={post.author.provenance} recorded={post.created} label="Post provenance" compact />
    </div>
  );
}

function attachmentHost(attachment) {
  try {
    return new URL(attachment.url).hostname;
  } catch {
    return attachment.kind;
  }
}

function githubRepository(attachment) {
  try {
    const parsed = new URL(attachment.url);
    return parsed.pathname.split("/").filter(Boolean).slice(0, 2).join("/");
  } catch {
    return "GitHub";
  }
}

export function AttachmentPreview({ attachment, featured = false }) {
  const isImage = attachment.kind === "image";
  const isGitHub = attachment.kind === "github";
  return (
    <a
      className={`attachment-preview attachment-preview--${attachment.kind}${featured ? " is-featured" : ""}`}
      href={attachment.url}
      target="_blank"
      rel="noreferrer"
    >
      {isImage ? <img src={attachment.url} alt="" loading="lazy" /> : (
        <span className="attachment-icon" aria-hidden="true">
          {isGitHub ? <Branch /> : <FileDocument />}
        </span>
      )}
      <span className="attachment-copy">
        {isGitHub ? <small>GitHub · {githubRepository(attachment)}</small> : null}
        <strong>{attachment.title}</strong>
        {!isGitHub ? <small>{attachmentHost(attachment)}</small> : <small>Open linked artifact</small>}
      </span>
      <ExternalLink aria-hidden="true" />
    </a>
  );
}

export function AttachmentStrip({ attachments, featured = false }) {
  if (!attachments?.length) return null;
  return (
    <div className={`attachment-strip${featured ? " attachment-strip--featured" : ""}`} aria-label="Post attachments">
      {attachments.map((attachment) => (
        <AttachmentPreview
          key={`${attachment.kind}:${attachment.url}`}
          attachment={attachment}
          featured={featured}
        />
      ))}
    </div>
  );
}

function PostBody({ body }) {
  const blocks = body.split("\n").map((line) => line.trim()).filter(Boolean);
  const output = [];
  let bullets = [];
  function flushBullets() {
    if (!bullets.length) return;
    output.push(<ul key={`list-${output.length}`}>{bullets.map((item) => <li key={item}>{item}</li>)}</ul>);
    bullets = [];
  }
  for (const block of blocks) {
    if (block.startsWith("- ")) {
      bullets.push(block.slice(2));
      continue;
    }
    flushBullets();
    output.push(<p key={`paragraph-${output.length}`}>{block}</p>);
  }
  flushBullets();
  return <div className="post-body">{output}</div>;
}

export function OpenedPostContent({
  opened,
  targetCommentID = "",
  showComments = true,
  commentComposer = null,
  onLoadMoreComments = null,
  loadingMoreComments = false,
  commentsError = "",
}) {
  const { post, comments } = opened;
  return (
    <div className="opened-post">
      <PostBody body={post.body} />
      <AttachmentStrip attachments={post.attachments} featured />
      <details className="post-details">
        <summary>Evidence and references</summary>
        <dl>
          <div><dt>Basis</dt><dd>{post.basis}</dd></div>
          {post.relatedRef ? <div><dt>Related reference</dt><dd>{post.relatedRef}</dd></div> : null}
          <div><dt>Post ID</dt><dd>{post.id}</dd></div>
        </dl>
      </details>
      {showComments ? (
        <section className="comments-section" aria-labelledby={`comments-${post.id}`}>
          <h2 id={`comments-${post.id}`}>Comments <span>{post.commentCount}</span></h2>
          {comments.items.length ? (
            <ol>
              {comments.items.map((comment) => (
                <li
                  key={comment.id}
                  data-comment-id={comment.id}
                  className={comment.id === targetCommentID ? "is-notification-source" : ""}
                  tabIndex={comment.id === targetCommentID ? -1 : undefined}
                >
                  <span className="comment-avatar" aria-hidden="true">{authorLabel(comment.author).slice(0, 1).toUpperCase()}</span>
                  <div>
                    {comment.id === targetCommentID ? <small className="notification-source-label">Opened from notification</small> : null}
                    <div className="comment-meta">
                      <strong title={authorSessionTitle(comment.author)}>{authorLabel(comment.author)}</strong>
                      {comment.author.handle ? <span className="session-handle">{"@"}{comment.author.handle}</span> : null}
                      <span className="comment-intent">{commentIntentLabels[comment.intent]}</span>
                      <Timestamp value={comment.created} compact />
                    </div>
                    <p>{comment.body}</p>
                    {comment.mentions.length ? <div className="comment-mentions" aria-label="Structured mentions">{comment.mentions.map((mention) => <span key={mention.principal}>{"@"}{mention.handle || mention.displayName || mention.principal}</span>)}</div> : null}
                    <ProvenanceDisclosure provenance={comment.author.provenance} recorded={comment.created} label="Comment provenance" compact />
                  </div>
                </li>
              ))}
            </ol>
          ) : <p className="comments-empty">No comments yet.</p>}
          {comments.nextCursor && onLoadMoreComments ? (
            <button className="comments-load-more" type="button" onClick={onLoadMoreComments} disabled={loadingMoreComments}>
              {loadingMoreComments ? "Loading comments…" : "Load more comments"}
            </button>
          ) : null}
          {commentsError ? <p className="comments-note comments-note--error" role="status">{commentsError}</p> : null}
          {commentComposer}
        </section>
      ) : null}
    </div>
  );
}

export function PostFeedRow({ post, selected, onSelect }) {
  const hasGitHub = post.attachments.some((attachment) => attachment.kind === "github");
  return (
    <article className={`post-index-item${selected ? " is-selected" : ""}`}>
      <button type="button" onClick={onSelect} aria-current={selected ? "true" : undefined}>
        <div className="index-item-meta">
          <PostKind kind={post.kind} />
          <span>{post.topic.name}</span>
          <PerspectiveScopeMarker scope={post.perspectiveScope} />
          <Timestamp value={post.created} compact />
        </div>
        <h2>{post.title}</h2>
        <p>{post.preview}</p>
        <div className="index-item-footer">
          {hasGitHub ? <span title="Includes GitHub context"><Branch aria-hidden="true" /></span> : <span />}
          <span><Comment aria-hidden="true" />{post.commentCount}</span>
          <PostState state={post.state} supersededBy={post.supersededBy} />
        </div>
      </button>
    </article>
  );
}

export function PostReader({ postID, onBack, session, replacementCandidates, onAuthRequired, onChanged, notificationTarget = null, onNotificationOpened, onNotificationFailed }) {
  const [refreshKey, setRefreshKey] = useState(0);
  const resource = useResource(
    async (signal) => {
      const openedPromise = commonsAdapter.readPost(postID, { comments_cursor: "", comments_limit: 20 }, signal);
      if (!notificationTarget?.source.commentRef) return openedPromise;
      const [opened, source] = await Promise.all([
        openedPromise,
        commonsAdapter.readCommentSource(notificationTarget.source.commentRef, signal),
      ]);
      if (source.postRef !== postID || source.comment.id !== notificationTarget.source.commentRef) {
        throw new Error("The notification source does not match this canonical thread.");
      }
      if (!opened.comments.items.some((comment) => comment.id === source.comment.id)) {
        opened.comments.items = [...opened.comments.items, source.comment].sort((left, right) => left.created.iso.localeCompare(right.created.iso) || left.id.localeCompare(right.id));
      }
      return opened;
    },
    [postID, refreshKey, notificationTarget?.id, notificationTarget?.source.commentRef],
  );
  useEffect(() => {
    if (resource.status === "error" && notificationTarget) {
      onNotificationFailed?.(notificationTarget.id, resource.error);
    }
  }, [resource.status, resource.error, notificationTarget?.id, onNotificationFailed]);
  if (resource.status === "loading" && (!resource.data || resource.data.post.id !== postID)) {
    return <div className="reader-state">Opening post…</div>;
  }
  if (resource.status === "error") {
    return (
      <div className="reader-state reader-state--error">
        <strong>Post unavailable</strong>
        <span>{resource.error}</span>
        <button type="button" onClick={onBack}><ChevronLeft aria-hidden="true" />Back to posts</button>
      </div>
    );
  }
  if (!resource.data) return null;
  return (
    <PostReaderReady
      key={postID}
      initialOpened={resource.data}
      onBack={onBack}
      session={session}
      replacementCandidates={replacementCandidates}
      onAuthRequired={onAuthRequired}
      notificationTarget={resource.status === "ready" ? notificationTarget : null}
      onNotificationOpened={onNotificationOpened}
      onRefresh={(message) => {
        setRefreshKey((value) => value + 1);
        onChanged(message);
      }}
    />
  );
}

function PostReaderReady({
  initialOpened,
  onBack,
  session,
  replacementCandidates,
  onAuthRequired,
  notificationTarget,
  onNotificationOpened,
  onRefresh,
}) {
  const [opened, setOpened] = useState(initialOpened);
  const [commentsStatus, setCommentsStatus] = useState({ loading: false, error: "" });
  const commentsControllerRef = useRef(null);
  const focusedNotificationRef = useRef("");
  const { post } = opened;

  useEffect(() => {
    setOpened(initialOpened);
    setCommentsStatus({ loading: false, error: "" });
  }, [initialOpened]);

  useEffect(() => () => commentsControllerRef.current?.abort(), []);

  useEffect(() => {
    if (!notificationTarget || focusedNotificationRef.current === notificationTarget.id) return undefined;
    const targetCommentID = notificationTarget.source.commentRef || "";
    const target = targetCommentID ? document.querySelector(`[data-comment-id="${CSS.escape(targetCommentID)}"]`) : document.querySelector(".post-reader");
    if (!target) return undefined;
    const frame = globalThis.requestAnimationFrame(() => {
      focusedNotificationRef.current = notificationTarget.id;
      target.focus?.({ preventScroll: true });
      target.scrollIntoView({ behavior: globalThis.matchMedia?.("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth", block: "center" });
      onNotificationOpened?.({
        notificationID: notificationTarget.id,
        postRef: post.id,
        commentRef: targetCommentID,
      });
    });
    return () => globalThis.cancelAnimationFrame(frame);
  }, [notificationTarget?.id, notificationTarget?.source.commentRef, post.id, opened.comments.items, onNotificationOpened]);

  function refreshed(message) {
    onRefresh(message);
  }

  async function loadMoreComments() {
    if (!opened.comments.nextCursor || commentsStatus.loading) return;
    commentsControllerRef.current?.abort();
    const controller = new AbortController();
    commentsControllerRef.current = controller;
    setCommentsStatus({ loading: true, error: "" });
    try {
      const next = await commonsAdapter.readPost(post.id, {
        comments_cursor: opened.comments.nextCursor,
        comments_limit: 20,
      }, controller.signal);
      setOpened((current) => {
        return {
          ...current,
          comments: mergeCommentPages(current.comments, next.comments),
        };
      });
      setCommentsStatus({ loading: false, error: "" });
    } catch (error) {
      if (error.name === "AbortError") return;
      setCommentsStatus({ loading: false, error: error.message });
    }
  }

  return (
    <article className="post-reader">
      <header className="reader-header">
        <button className="reader-mobile-back" type="button" onClick={onBack}>
          <ChevronLeft aria-hidden="true" />
          Back to posts
        </button>
        <div className="reader-meta-row">
          <PostMeta post={post} compact />
          <div className="reader-actions" aria-label="Post actions">
            {post.attachments[0] ? (
              <a href={post.attachments[0].url} target="_blank" rel="noreferrer" aria-label="Open first attachment">
                <Link aria-hidden="true" />
              </a>
            ) : null}
            <PostStateMenu post={post} candidates={replacementCandidates} session={session} onAuthRequired={onAuthRequired} onSuccess={refreshed} />
          </div>
        </div>
        <h1>{post.title}</h1>
      </header>
      <OpenedPostContent
        opened={opened}
        targetCommentID={notificationTarget?.source.commentRef || ""}
        onLoadMoreComments={loadMoreComments}
        loadingMoreComments={commentsStatus.loading}
        commentsError={commentsStatus.error}
        commentComposer={(
          <CommentComposer
            postID={post.id}
            projectID={post.project?.id || ""}
            session={session}
            onAuthRequired={onAuthRequired}
            onSuccess={() => refreshed("Comment added.")}
          />
        )}
      />
    </article>
  );
}
