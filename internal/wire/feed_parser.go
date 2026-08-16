package wire

import (
	"bytes"
	"encoding/xml"
	"fmt"
	stdhtml "html"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"
)

var trackingQueryKey = regexp.MustCompile(`(?i)^(utm_.+|ref|referrer|source|campaign|cmpid|cid)$`)

type feedItem struct {
	ID          string
	Title       string
	Description string
	Link        string
	Author      string
	PublishedAt time.Time
	UpdatedAt   time.Time
}

type rssDocument struct {
	Channel struct {
		Title string    `xml:"title"`
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	GUID        string `xml:"guid"`
	Title       string `xml:"title"`
	Description string `xml:"description"`
	Link        string `xml:"link"`
	Author      string `xml:"author"`
	Creator     string `xml:"creator"`
	PubDate     string `xml:"pubDate"`
	Updated     string `xml:"updated"`
}

type atomDocument struct {
	Title   string      `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID        string     `xml:"id"`
	Title     string     `xml:"title"`
	Summary   string     `xml:"summary"`
	Content   string     `xml:"content"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
	Links     []atomLink `xml:"link"`
	Author    struct {
		Name string `xml:"name"`
	} `xml:"author"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

func parseSyndication(payload []byte, feedURL string) ([]feedItem, error) {
	decoder := xml.NewDecoder(bytes.NewReader(payload))
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read feed XML: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch strings.ToLower(start.Name.Local) {
		case "rss", "rdf":
			var document rssDocument
			if err := xml.Unmarshal(payload, &document); err != nil {
				return nil, fmt.Errorf("decode RSS: %w", err)
			}
			return rssItems(document, feedURL), nil
		case "feed":
			var document atomDocument
			if err := xml.Unmarshal(payload, &document); err != nil {
				return nil, fmt.Errorf("decode Atom: %w", err)
			}
			return atomItems(document, feedURL), nil
		default:
			return nil, fmt.Errorf("unsupported feed root %q", start.Name.Local)
		}
	}
}

func rssItems(document rssDocument, feedURL string) []feedItem {
	items := make([]feedItem, 0, len(document.Channel.Items))
	for _, item := range document.Channel.Items {
		author := strings.TrimSpace(item.Creator)
		if author == "" {
			author = strings.TrimSpace(item.Author)
		}
		link := resolveFeedLink(feedURL, item.Link)
		id := strings.TrimSpace(item.GUID)
		if id == "" {
			id = link
		}
		items = append(items, feedItem{
			ID:          id,
			Title:       cleanFeedText(item.Title, 220),
			Description: cleanFeedText(item.Description, 420),
			Link:        link,
			Author:      cleanFeedText(author, 120),
			PublishedAt: parseFeedTime(item.PubDate),
			UpdatedAt:   parseFeedTime(item.Updated),
		})
	}
	return items
}

func atomItems(document atomDocument, feedURL string) []feedItem {
	items := make([]feedItem, 0, len(document.Entries))
	for _, entry := range document.Entries {
		link := ""
		for _, candidate := range entry.Links {
			if candidate.Rel == "" || candidate.Rel == "alternate" {
				link = resolveFeedLink(feedURL, candidate.Href)
				break
			}
		}
		description := entry.Summary
		if strings.TrimSpace(description) == "" {
			description = entry.Content
		}
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			id = link
		}
		items = append(items, feedItem{
			ID:          id,
			Title:       cleanFeedText(entry.Title, 220),
			Description: cleanFeedText(description, 420),
			Link:        link,
			Author:      cleanFeedText(entry.Author.Name, 120),
			PublishedAt: parseFeedTime(entry.Published),
			UpdatedAt:   parseFeedTime(entry.Updated),
		})
	}
	return items
}

func parseFeedTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if parsed, err := mail.ParseDate(value); err == nil {
		return parsed.UTC()
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func cleanFeedText(value string, maxRunes int) string {
	value = stdhtml.UnescapeString(value)
	tokenizer := xhtml.NewTokenizer(strings.NewReader(value))
	parts := make([]string, 0, 8)
	ignoredDepth := 0
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case xhtml.ErrorToken:
			return compactText(strings.Join(parts, " "), maxRunes)
		case xhtml.StartTagToken:
			token := tokenizer.Token()
			if token.Data == "script" || token.Data == "style" {
				ignoredDepth++
			}
		case xhtml.EndTagToken:
			token := tokenizer.Token()
			if (token.Data == "script" || token.Data == "style") && ignoredDepth > 0 {
				ignoredDepth--
			}
		case xhtml.TextToken:
			if ignoredDepth == 0 {
				parts = append(parts, string(tokenizer.Text()))
			}
		}
	}
}

func resolveFeedLink(feedURL, value string) string {
	value = strings.TrimSpace(value)
	reference, err := url.Parse(value)
	if err != nil {
		return ""
	}
	base, err := url.Parse(feedURL)
	if err == nil {
		reference = base.ResolveReference(reference)
	}
	if reference.Scheme != "http" && reference.Scheme != "https" {
		return ""
	}
	return reference.String()
}

func canonicalSourceURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return ""
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	query := parsed.Query()
	for key := range query {
		if trackingQueryKey.MatchString(key) {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	if parsed.Path != "/" {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	}
	return parsed.String()
}
