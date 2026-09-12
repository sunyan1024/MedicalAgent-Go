package medical

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"GoAI/common/rag"
	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func PublicSearch(ctx context.Context, query string) ([]Source, error) {
	q, err := rag.NewMedicalRAGQuery(ctx)
	if err != nil {
		return nil, err
	}
	docs, err := q.RetrieveDocuments(ctx, query)
	if err != nil {
		return nil, err
	}
	sources := make([]Source, 0, len(docs))
	for _, d := range docs {
		if d == nil {
			continue
		}
		if len(sources) >= 5 {
			break
		}
		title, _ := d.MetaData["original_name"].(string)
		if title == "" {
			title, _ = d.MetaData["filename"].(string)
		}
		if title == "" {
			title = "医学知识资料"
		}
		h := sha256.Sum256([]byte(title + "\n" + d.Content))
		sources = append(sources, Source{ID: fmt.Sprintf("med_%x", h[:8]), Title: title, Content: d.Content})
	}
	return sources, nil
}

// SearchMCP crosses a real Streamable HTTP MCP transport. The listener is private,
// short-lived and authenticated; its handler only has public knowledge access.
// Keeping it in-process avoids importing the separate legacy weather Go module.
func SearchMCP(ctx context.Context, query string) ([]Source, error) {
	return searchMCP(ctx, query, PublicSearch)
}

func searchMCP(ctx context.Context, query string, search func(context.Context, string) ([]Source, error)) ([]Source, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s := server.NewMCPServer("medical-knowledge", "1.0.0", server.WithToolCapabilities(false))
	s.AddTool(mcp.NewTool("search_medical_knowledge", mcp.WithDescription("查询公共医学知识库"), mcp.WithString("query", mcp.Required())), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q, ok := req.GetArguments()["query"].(string)
		if !ok || len(q) == 0 || len(q) > 12000 {
			return mcp.NewToolResultError("invalid query"), nil
		}
		out, err := search(ctx, q)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(JSON(out)), nil
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	token := uuid.NewString()
	handler := server.NewStreamableHTTPServer(s)
	httpServer := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if subtle.ConstantTimeCompare([]byte(req.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, req)
	})}
	go func() { _ = httpServer.Serve(listener) }()
	defer httpServer.Close()
	c, err := client.NewStreamableHttpClient("http://"+listener.Addr().String()+"/mcp", transport.WithHTTPHeaders(map[string]string{"Authorization": "Bearer " + token}))
	if err != nil {
		return nil, err
	}
	defer c.Close()
	init := mcp.InitializeRequest{}
	init.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	init.Params.ClientInfo = mcp.Implementation{Name: "medical-agent", Version: "1.0.0"}
	if _, err = c.Initialize(ctx, init); err != nil {
		return nil, err
	}
	result, err := c.CallTool(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "search_medical_knowledge", Arguments: map[string]any{"query": query}}})
	if err != nil {
		return nil, err
	}
	if result.IsError {
		return nil, fmt.Errorf("医学知识检索失败，请检查公共资料索引与检索配置")
	}
	for _, content := range result.Content {
		if txt, ok := content.(mcp.TextContent); ok {
			var out []Source
			err = json.Unmarshal([]byte(txt.Text), &out)
			return out, err
		}
	}
	return nil, fmt.Errorf("MCP 未返回可用证据")
}
