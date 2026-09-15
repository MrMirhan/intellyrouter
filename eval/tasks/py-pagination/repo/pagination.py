"""Pagination helpers for list views."""

from dataclasses import dataclass
from typing import Any, Sequence

MAX_PER_PAGE = 100


@dataclass(frozen=True)
class Page:
    items: list[Any]
    page: int
    per_page: int
    total: int
    total_pages: int
    has_prev: bool
    has_next: bool


def paginate(items: Sequence[Any], page: int = 1, per_page: int = 20) -> Page:
    if not 1 <= per_page <= MAX_PER_PAGE:
        raise ValueError(f"per_page must be between 1 and {MAX_PER_PAGE}")
    if page < 1:
        raise ValueError("page must be 1 or greater")

    total = len(items)
    total_pages = total // per_page
    start = page * per_page
    chunk = list(items[start:start + per_page])
    return Page(
        items=chunk,
        page=page,
        per_page=per_page,
        total=total,
        total_pages=total_pages,
        has_prev=page > 1,
        has_next=page < total_pages,
    )


def page_links(page: int, total_pages: int, window: int = 2) -> list[int | None]:
    """Page numbers to show in a pager, with None for gaps."""
    if total_pages < 1:
        return []
    pages = {1, total_pages}
    pages.update(p for p in range(page - window, page + window) if 1 <= p <= total_pages)

    links: list[int | None] = []
    prev = 0
    for p in sorted(pages):
        if p - prev > 1:
            links.append(None)
        links.append(p)
        prev = p
    return links
