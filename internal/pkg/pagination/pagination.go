// Package pagination provides a generic cursor-based pagination helper for
// pre-sorted slices.
package pagination

// Page holds a paginated result set.
type Page[T any] struct {
	Items         []T   // the page of results
	NextCursor    int64 // cursor value of the last item (use as afterCursor for next page)
	HasMore       bool  // true if more items exist beyond this page
	TotalFiltered int   // total count of items matching the filter
}

// Paginate filters and paginates a pre-sorted slice.
//
// Parameters:
//   - items: the full sorted slice to paginate
//   - cursor: extracts the monotonic ID/cursor from an item
//   - afterCursor: exclusive lower bound (0 = start from beginning)
//   - limit: requested page size (<=0 uses defaultLimit; capped at maxLimit)
//   - defaultLimit: fallback when limit is <=0
//   - maxLimit: hard cap on page size
//   - filter: optional predicate (nil = include all items)
func Paginate[T any](
	items []T,
	cursor func(T) int64,
	afterCursor int64,
	limit, defaultLimit, maxLimit int,
	filter func(T) bool,
) Page[T] {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	var result []T
	totalFiltered := 0

	// Single pass: skip items at or before the cursor, apply filter, then
	// collect up to limit items. Continue counting totalFiltered past the
	// limit to determine HasMore.
	for _, item := range items {
		c := cursor(item)
		if afterCursor > 0 && c <= afterCursor {
			continue
		}
		if filter != nil && !filter(item) {
			continue
		}
		totalFiltered++
		if len(result) < limit {
			result = append(result, item)
		}
	}

	// Ensure non-nil slice for JSON serialization ([] instead of null).
	if result == nil {
		result = []T{}
	}

	var page Page[T]
	page.Items = result
	page.TotalFiltered = totalFiltered
	page.HasMore = totalFiltered > len(result)
	if len(result) > 0 {
		page.NextCursor = cursor(result[len(result)-1])
	}
	return page
}
