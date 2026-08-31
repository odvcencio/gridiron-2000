package league

// timezoneFriendlyNames maps the IANA zone identifiers a US-based fantasy
// league is realistically configured with (config.go's Timezone field,
// validated against time.LoadLocation) to the plain-English name a manager
// actually says out loud. An id this map does not cover renders as-is:
// FriendlyTimezoneLabel never hides an unrecognized zone behind a made-up
// name.
var timezoneFriendlyNames = map[string]string{
	"America/New_York":    "Eastern Time",
	"America/Detroit":     "Eastern Time",
	"America/Chicago":     "Central Time",
	"America/Denver":      "Mountain Time",
	"America/Phoenix":     "Arizona Time",
	"America/Los_Angeles": "Pacific Time",
	"America/Anchorage":   "Alaska Time",
	"Pacific/Honolulu":    "Hawaii Time",
	"UTC":                 "UTC",
}

// FriendlyTimezoneLabel renders iana (a raw IANA zone id, e.g.
// "America/New_York") as the plain-English name a manager recognizes
// (P3-22, UI pass 2026-08-30): "Eastern Time", not the id underneath it.
// A zone outside the map above (a non-US league, for example) passes
// through unchanged rather than showing nothing.
func FriendlyTimezoneLabel(iana string) string {
	if name, ok := timezoneFriendlyNames[iana]; ok {
		return name
	}
	return iana
}
