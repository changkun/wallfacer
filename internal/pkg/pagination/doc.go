// Package pagination provides a generic cursor-based pagination helper for
// pre-sorted slices.
//
// Server-side pagination with stable cursors is needed for event timelines and
// archived task lists where offset-based pagination would shift as new items
// are added. [Paginate] takes a sorted slice, applies cursor positioning,
// filtering, and limit clamping, returning a [Page] with items, next cursor,
// and total count.
//
// # Connected packages
//
// No dependencies (not even stdlib). Consumed by [store] for paginating task
// event timelines via cursor-based queries. When changing pagination behavior,
// verify the event timeline API and its frontend consumer.
//
// # Usage
//
//	page := pagination.Paginate(items, cursor, afterCursor, limit, 50, 200, nil)
//	// page.Items, page.NextCursor, page.HasMore, page.TotalFiltered
package pagination
