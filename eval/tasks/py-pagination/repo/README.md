# pagination

Pagination helpers for the list views in our admin dashboard.

```python
from pagination import paginate, page_links

page = paginate(orders, page=2, per_page=25)
page.items        # the orders on page 2
page.total_pages  # used by the template to render the pager
page_links(page.page, page.total_pages)  # e.g. [1, None, 4, 5, 6, 7, 8, None, 12]
```

## Rules

- Pages are numbered from 1. `page` below 1 raises `ValueError`.
- `per_page` must be between 1 and 100, otherwise `ValueError`.
- `total_pages` is the number of pages needed to show every item. An empty
  list still has one (empty) page.
- A page after the last page is valid and has no items.
- `has_prev` is true for every page after the first. `has_next` is true
  only when a later page has items.

`page_links(page, total_pages, window=2)` returns the page numbers for the
pager: the first page, the last page, and every page within `window` of the
current page. `None` marks a gap. A gap that would hide only one page shows
that page instead.

Run the tests with `python3 -m unittest`.
