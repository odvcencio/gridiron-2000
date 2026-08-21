package league

import (
	"net/url"
	"strconv"
)

// poolPageSize keeps the first paint useful on a phone and bounded on a
// desktop while leaving the pool itself server-searchable. The draft room and
// player pool intentionally share this budget so the two ways managers browse
// players feel the same.
const poolPageSize = 50

type poolPagination struct {
	Page        int
	Pages       int
	Total       int
	PageSize    int
	Start       int
	End         int
	HasPrevious bool
	HasNext     bool
}

func newPoolPagination(total int, rawPage string) poolPagination {
	pages := (total + poolPageSize - 1) / poolPageSize
	if pages == 0 {
		pages = 1
	}
	page, err := strconv.Atoi(rawPage)
	if err != nil || page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	start := (page - 1) * poolPageSize
	end := start + poolPageSize
	if end > total {
		end = total
	}
	return poolPagination{
		Page:        page,
		Pages:       pages,
		Total:       total,
		PageSize:    poolPageSize,
		Start:       start,
		End:         end,
		HasPrevious: page > 1,
		HasNext:     page < pages,
	}
}

// poolPageHref returns a stable GET link for a paginated pool. A first page
// omits page=1 so copied links stay compact; search and position remain in the
// URL and work without JavaScript.
func poolPageHref(path, pos, query string, page int) string {
	values := url.Values{}
	if pos != "" {
		values.Set("pos", pos)
	}
	if query != "" {
		values.Set("q", query)
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if encoded := values.Encode(); encoded != "" {
		return path + "?" + encoded
	}
	return path
}
