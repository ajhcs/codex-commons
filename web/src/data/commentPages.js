export function mergeCommentPages(current, next) {
  const comments = new Map(current.items.map((comment) => [comment.id, comment]));
  for (const comment of next.items) comments.set(comment.id, comment);
  return {
    ...current,
    items: [...comments.values()],
    nextCursor: next.nextCursor,
  };
}
