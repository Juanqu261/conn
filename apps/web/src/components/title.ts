import { useEffect } from "react";

/** Sets the tab title to "<page> · Conn Lab". */
export function useDocumentTitle(page: string | undefined): void {
  useEffect(() => {
    document.title = page ? `${page} · Conn Lab` : "Conn Lab";
  }, [page]);
}
