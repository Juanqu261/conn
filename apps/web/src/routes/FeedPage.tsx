import { useParams } from "@tanstack/react-router";
import { useDocumentTitle } from "../components/title";
import { LABELS } from "../labels";
import { projectIdFromSlug } from "../names/directory";
import { FeedPanel } from "../shell/FeedPanel";

/** The Lightning feed as a page, for narrow screens and shareable links. */
export function FeedPage() {
  const { project } = useParams({ from: "/p/$project" });
  useDocumentTitle(LABELS.feed.themed);
  return (
    <div className="page page-narrow">
      <FeedPanel projectId={projectIdFromSlug(project)} />
    </div>
  );
}
