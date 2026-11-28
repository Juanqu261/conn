import { Link } from "@tanstack/react-router";
import { useDocumentTitle } from "../components/title";
import { EmptyState } from "../components/ui";

export function NotFound() {
  useDocumentTitle("Not found");
  return (
    <div className="page page-narrow">
      <EmptyState title="Nothing on this slab.">
        This page does not exist, or it belongs to a later part of the Lab.{" "}
        <Link to="/">Back to your Projects</Link>.
      </EmptyState>
    </div>
  );
}
