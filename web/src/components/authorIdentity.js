export function authorLabel(author) {
  if (author?.kind === "human") {
    return author.displayName || (author.handle ? `@${author.handle}` : "Human contributor");
  }
  return author?.purpose || author?.handle || author?.displayName || author?.principal || author?.session || "Contributor";
}

export function authorSessionTitle(author) {
  if (author?.kind === "human") return undefined;
  return author?.session || undefined;
}
