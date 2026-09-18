package medical

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"GoAI/config"
)

// ConfiguredWebSearch returns nil when the optional tool is unavailable.
func ConfiguredWebSearch() func(context.Context, string) ([]Source, error) {
	c := config.GetConfig().TavilyConfig
	key := strings.TrimSpace(os.Getenv("TAVILY_API_KEY"))
	if key == "" {
		key = strings.TrimSpace(c.APIKey)
	}
	if !c.Enabled || key == "" {
		return nil
	}
	timeout := c.TimeoutSeconds
	if timeout <= 0 {
		timeout = 15
	}
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, query string) ([]Source, error) {
		return searchTavily(ctx, client, "https://api.tavily.com/search", key, query)
	}
}

func searchTavily(ctx context.Context, client *http.Client, endpoint, key, query string) ([]Source, error) {
	if strings.TrimSpace(query) == "" || len(query) > 12000 {
		return nil, fmt.Errorf("无效的联网检索词")
	}
	body, _ := json.Marshal(map[string]any{"query": query, "search_depth": "basic", "max_results": 5, "topic": "general", "auto_parameters": false, "include_answer": false, "include_raw_content": false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("联网搜索请求配置无效")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("联网搜索连接失败或超时，请稍后重试")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		switch resp.StatusCode {
		case 401, 403:
			return nil, fmt.Errorf("联网搜索鉴权失败，请检查 Tavily 配置")
		case 429:
			return nil, fmt.Errorf("联网搜索请求过于频繁，请稍后重试")
		case 432, 433:
			return nil, fmt.Errorf("联网搜索额度已耗尽")
		default:
			return nil, fmt.Errorf("联网搜索服务暂不可用（HTTP %d）", resp.StatusCode)
		}
	}
	const maxBody = 2 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(data) > maxBody {
		return nil, fmt.Errorf("联网搜索响应读取失败或内容过大")
	}
	var result struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if json.Unmarshal(data, &result) != nil {
		return nil, fmt.Errorf("联网搜索返回了无效数据")
	}
	sources := make([]Source, 0, 5)
	seen := map[string]bool{}
	for _, item := range result.Results {
		u, err := url.Parse(strings.TrimSpace(item.URL))
		if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") || strings.TrimSpace(item.Content) == "" {
			continue
		}
		u.Fragment = ""
		link := u.String()
		if seen[link] {
			continue
		}
		seen[link] = true
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = u.Hostname()
		}
		sources = append(sources, Source{ID: fmt.Sprintf("web_%x", sha256.Sum256([]byte(link))), Kind: "web", URL: link, Title: preview(title, 300), Content: preview(item.Content, 4000)})
		if len(sources) == 5 {
			break
		}
	}
	return sources, nil
}
