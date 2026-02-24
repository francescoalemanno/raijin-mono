package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/francescoalemanno/raijin-mono/llmbridge/pkg/llm"
	"golang.org/x/net/html"
)

const webfetchDescription = "Fetch content from a web URL and return it converted to markdown."

type webfetchParams struct {
	URL string `json:"url" description:"The URL to fetch content from (HTTP or HTTPS)"`
}

const browserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

var multipleNewlinesRe = regexp.MustCompile(`\n{3,}`)

// NewWebFetchTool creates a webfetch tool for fetching web content.
func NewWebFetchTool() llm.Tool {
	handler := func(ctx context.Context, params webfetchParams, call llm.ToolCall) (llm.ToolResponse, error) {
		if resp, blocked := toolExecutionGate(ctx, "webfetch"); blocked {
			return resp, nil
		}
		if params.URL == "" {
			return llm.NewTextErrorResponse("url is required"), nil
		}

		u, err := url.Parse(params.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return llm.NewTextErrorResponse("only HTTP and HTTPS URLs are allowed"), nil
		}

		content, err := fetchURLAndConvert(ctx, params.URL)
		if err != nil {
			if ctx.Err() != nil {
				return llm.ToolResponse{}, ctx.Err()
			}
			return llm.NewTextErrorResponse(fmt.Sprintf("fetching URL: %s", err)), nil
		}

		var result strings.Builder
		result.WriteString(fmt.Sprintf("Fetched content from %s:\n\n", params.URL))
		result.WriteString(content)
		return llm.NewTextResponse(result.String()), nil
	}

	renderFunc := func(input json.RawMessage, _ string, _ int) string {
		var params webfetchParams
		if err := llm.ParseJSONInput(input, &params); err != nil {
			return "webfetch (failed)"
		}
		return fmt.Sprintf("fetch %s", params.URL)
	}

	return WithRender(
		llm.NewParallelAgentTool("webfetch", webfetchDescription, handler),
		renderFunc,
	)
}

func fetchURLAndConvert(ctx context.Context, targetURL string) (string, error) {
	client := &http.Client{
		Timeout: defaultWebFetchTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < httpStatusSuccessMin || resp.StatusCode >= httpStatusSuccessMax {
		return "", fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWebFetchBodySize))
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	content := string(body)

	if !utf8.ValidString(content) {
		return "", fmt.Errorf("response content is not valid UTF-8")
	}

	contentType := resp.Header.Get("Content-Type")

	if strings.Contains(contentType, "text/html") {
		cleanedHTML := removeNoisyElements(content)
		markdown, err := convertHTMLToMarkdown(cleanedHTML)
		if err != nil {
			return "", fmt.Errorf("failed to convert HTML to markdown: %w", err)
		}
		content = cleanupMarkdown(markdown)
	} else if strings.Contains(contentType, "application/json") || strings.Contains(contentType, "text/json") {
		formatted, err := formatJSON(content)
		if err == nil {
			content = formatted
		}
	}

	return content, nil
}

func removeNoisyElements(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return htmlContent
	}

	noisyTags := map[string]bool{
		"script":   true,
		"style":    true,
		"nav":      true,
		"header":   true,
		"footer":   true,
		"aside":    true,
		"noscript": true,
		"iframe":   true,
		"svg":      true,
	}

	var removeNodes func(*html.Node)
	removeNodes = func(n *html.Node) {
		var toRemove []*html.Node

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && noisyTags[c.Data] {
				toRemove = append(toRemove, c)
			} else {
				removeNodes(c)
			}
		}

		for _, node := range toRemove {
			n.RemoveChild(node)
		}
	}

	removeNodes(doc)

	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return htmlContent
	}

	return buf.String()
}

func convertHTMLToMarkdown(htmlContent string) (string, error) {
	markdown, err := htmltomarkdown.ConvertString(htmlContent)
	if err != nil {
		return "", err
	}
	return markdown, nil
}

func cleanupMarkdown(content string) string {
	content = multipleNewlinesRe.ReplaceAllString(content, "\n\n")

	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	content = strings.Join(lines, "\n")

	content = strings.TrimSpace(content)

	return content
}

func formatJSON(content string) (string, error) {
	var data any
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return "", err
	}

	return strings.TrimSpace(buf.String()), nil
}
