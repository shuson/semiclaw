package webcrawl

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Page struct {
	URL   string
	Title string
	Text  string
	Links []string
}

type Fetcher struct {
	client *http.Client
}

func NewFetcher(timeout time.Duration) *Fetcher {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &Fetcher{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (f *Fetcher) Fetch(ctx context.Context, rawURL string, maxChars int, maxLinks int) (*Page, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}
	if maxChars <= 0 {
		maxChars = 4000
	}
	if maxLinks < 0 {
		maxLinks = 0
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/81.0.4044.129 Safari/537.36")

	res, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch page: %w", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, 3*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read page body: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("page request failed: status=%d body=%s", res.StatusCode, string(body))
	}

	htmlBody := string(body)
	finalURL := rawURL
	if res.Request != nil && res.Request.URL != nil {
		finalURL = res.Request.URL.String()
	}

	page := &Page{
		URL:   finalURL,
		Title: extractTitle(htmlBody),
		Text:  extractText(htmlBody, maxChars),
		Links: extractLinks(htmlBody, finalURL, maxLinks),
	}
	return page, nil
}

var (
	titleRe    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	scriptRe   = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleRe    = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	tagRe      = regexp.MustCompile(`(?is)<[^>]+>`)
	spaceRe    = regexp.MustCompile(`\s+`)
	linkHrefRe = regexp.MustCompile(`(?is)<a[^>]+href\s*=\s*['"]([^'"]+)['"][^>]*>`)
)

func extractTitle(html string) string {
	m := titleRe.FindStringSubmatch(html)
	if len(m) < 2 {
		return ""
	}
	return cleanText(m[1])
}

func extractText(html string, maxChars int) string {
	out := scriptRe.ReplaceAllString(html, " ")
	out = styleRe.ReplaceAllString(out, " ")
	out = tagRe.ReplaceAllString(out, " ")
	out = cleanText(out)
	if len(out) > maxChars {
		return out[:maxChars] + "..."
	}
	return out
}

func cleanText(input string) string {
	out := strings.TrimSpace(input)
	out = strings.ReplaceAll(out, "&nbsp;", " ")
	out = strings.ReplaceAll(out, "&amp;", "&")
	out = strings.ReplaceAll(out, "&lt;", "<")
	out = strings.ReplaceAll(out, "&gt;", ">")
	out = strings.ReplaceAll(out, "&#39;", "'")
	out = strings.ReplaceAll(out, "&quot;", "\"")
	out = spaceRe.ReplaceAllString(out, " ")
	return strings.TrimSpace(out)
}

func extractLinks(html string, baseURL string, maxLinks int) []string {
	matches := linkHrefRe.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 || maxLinks == 0 {
		return nil
	}

	base, _ := url.Parse(baseURL)
	seen := make(map[string]struct{})
	links := make([]string, 0, maxLinks)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		href := strings.TrimSpace(m[1])
		if href == "" ||
			strings.HasPrefix(href, "#") ||
			strings.HasPrefix(strings.ToLower(href), "javascript:") ||
			strings.HasPrefix(strings.ToLower(href), "mailto:") {
			continue
		}
		parsed, err := url.Parse(href)
		if err != nil {
			continue
		}
		if base != nil {
			parsed = base.ResolveReference(parsed)
		}
		u := parsed.String()
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		links = append(links, u)
		if len(links) >= maxLinks {
			break
		}
	}
	return links
}
