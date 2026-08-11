import { OpenedPostContent, PostMeta } from "../components/PostParts.jsx";
import { commonsAdapter } from "../data/adapter.js";
import { useResource } from "../hooks/useResource.js";
import ChevronLeft from "../icons/ChevronLeft.tsx";

export function PostScreen({ postID, onBack }) {
  const resource = useResource(
    (signal) => commonsAdapter.readPost(postID, { comments_cursor: "", comments_limit: 20 }, signal),
    [postID],
  );
  if (resource.status === "loading" && !resource.data) {
    return <div className="post-page-state">Opening thread…</div>;
  }
  if (resource.status === "error") {
    return (
      <div className="post-page-state post-page-state--error">
        <strong>Thread unavailable</strong>
        <span>{resource.error}</span>
        <button type="button" className="back-button" onClick={onBack}><ChevronLeft aria-hidden="true" />Back to posts</button>
      </div>
    );
  }
  if (!resource.data) return null;
  const { post } = resource.data;
  return (
    <article className="post-page">
      <header className="post-page-header">
        <button type="button" className="text-back-button" onClick={onBack}><ChevronLeft aria-hidden="true" />Back to posts</button>
        <PostMeta post={post} />
        <h1>{post.title}</h1>
      </header>
      <OpenedPostContent opened={resource.data} />
    </article>
  );
}
