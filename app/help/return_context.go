package help

import (
	"net/http"
	"net/url"
	"strings"

	"gridiron-2000/internal/navigation"
)

// ReturnToQuery is the one query key shared by contextual help links and the
// topic route. It carries a complete same-origin task URL, including the
// owning page's query and focus fragment, rather than trying to reconstruct
// mutable page state inside the help corpus.
const ReturnToQuery = "return_to"

// ReturnPathForRequest preserves the current GET route's path and query and
// appends the caller's stable focus fragment. HTTP requests do not carry a
// browser fragment, so pages pass the fragment for the section that opened
// help. SafeActionReturnPath remains the single validation contract.
//
// An empty result means the request could not produce a safe target. The
// caller should then render a topic URL without ReturnToQuery.
func ReturnPathForRequest(r *http.Request, fragment string) string {
	if r == nil || r.URL == nil {
		return ""
	}
	raw := r.URL.RequestURI()
	if raw == "" {
		raw = "/"
	}
	fragment = strings.TrimPrefix(strings.TrimSpace(fragment), "#")
	if fragment != "" {
		raw += "#" + fragment
	}
	safe := navigation.SafeActionReturnPath(raw)
	if safe == navigation.DefaultReturnPath && raw != navigation.DefaultReturnPath {
		return ""
	}
	return safe
}

// ContextualTopicURL builds a topic URL and carries only a validated return
// target. The supplied topic parameters are copied so callers do not observe
// this helper mutating their url.Values map.
func ContextualTopicURL(topicID string, params url.Values, returnPath string) string {
	values := make(url.Values, len(params)+1)
	for key, entries := range params {
		values[key] = append([]string(nil), entries...)
	}
	delete(values, ReturnToQuery)
	if safe, ok := safeContextReturnPath(returnPath); ok {
		values.Set(ReturnToQuery, safe)
	}

	target := "/help/" + url.PathEscape(strings.TrimSpace(topicID))
	if encoded := values.Encode(); encoded != "" {
		target += "?" + encoded
	}
	return target
}

func safeContextReturnPath(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	safe := navigation.SafeActionReturnPath(raw)
	if safe == navigation.DefaultReturnPath && raw != navigation.DefaultReturnPath {
		return "", false
	}
	return safe, true
}
