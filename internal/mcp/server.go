package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"siyuan-knowledge-sync/internal/siyuan"
)

type MCPServer struct {
	client *siyuan.Client
	srv    *mcp.Server
}

func NewServer(client *siyuan.Client) *MCPServer {
	s := &MCPServer{client: client}

	impl := &mcp.Implementation{
		Name:    "siyuan-knowledge-sync",
		Version: "1.0.0",
	}
	srv := mcp.NewServer(impl, nil)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search",
		Description: "Search SiYuan documents by keyword. Returns matching document IDs with titles, paths, and excerpts.",
	}, s.handleSearch)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "retrieve",
		Description: "Retrieve the full markdown content of a SiYuan document by its ID.",
	}, s.handleRetrieve)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_notebooks",
		Description: "List all SiYuan notebooks with their document counts.",
	}, s.handleListNotebooks)

	s.srv = srv
	return s
}

func (s *MCPServer) Run(ctx context.Context) error {
	return s.srv.Run(ctx, &mcp.StdioTransport{})
}

type searchArgs struct {
	Query string `json:"query" jsonschema:"The search query string (matched against document content and titles)"`
}

type searchResultItem struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Path    string `json:"path"`
	Excerpt string `json:"excerpt"`
}

func (s *MCPServer) handleSearch(ctx context.Context, req *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
	escaped := strings.ReplaceAll(args.Query, "'", "''")
	sqlStmt := fmt.Sprintf(
		"SELECT id, name, box, hpath, content FROM blocks WHERE type = 'd' AND (content LIKE '%%%s%%' OR name LIKE '%%%s%%') LIMIT 20",
		escaped, escaped,
	)

	rows, err := s.client.SQLQuery(ctx, sqlStmt)
	if err != nil {
		return nil, nil, fmt.Errorf("search failed: %w", err)
	}

	var results []searchResultItem
	for _, row := range rows {
		content := strVal(row, "content")
		excerpt := content
		if len(excerpt) > 300 {
			excerpt = excerpt[:300] + "..."
		}

		results = append(results, searchResultItem{
			ID:      strVal(row, "id"),
			Title:   strVal(row, "name"),
			Path:    strVal(row, "hpath"),
			Excerpt: excerpt,
		})
	}

	if results == nil {
		results = []searchResultItem{}
	}

	jsonBytes, err := json.Marshal(results)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to format results: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(jsonBytes)},
		},
	}, nil, nil
}

type retrieveArgs struct {
	ID string `json:"id" jsonschema:"The document ID to retrieve full content for"`
}

type retrieveResult struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
	Notebook string `json:"notebook"`
}

func (s *MCPServer) handleRetrieve(ctx context.Context, req *mcp.CallToolRequest, args retrieveArgs) (*mcp.CallToolResult, any, error) {
	export, err := s.client.ExportMdContent(ctx, args.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("retrieve failed for document %s: %w", args.ID, err)
	}

	metaRows, err := s.client.SQLQuery(ctx,
		fmt.Sprintf("SELECT name, box FROM blocks WHERE id = '%s'", strings.ReplaceAll(args.ID, "'", "''")),
	)
	title := export.HPath
	notebook := ""
	if err == nil && len(metaRows) > 0 {
		if n, ok := metaRows[0]["name"]; ok {
			if ns, ok := n.(string); ok && ns != "" {
				title = ns
			}
		}
		notebook = strVal(metaRows[0], "box")
	}

	result := retrieveResult{
		ID:       args.ID,
		Title:    title,
		Markdown: export.Content,
		Notebook: notebook,
	}

	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to format result: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(jsonBytes)},
		},
	}, nil, nil
}

type listNotebooksArgs struct{}

type notebookItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	DocCount int    `json:"doc_count"`
}

func (s *MCPServer) handleListNotebooks(ctx context.Context, req *mcp.CallToolRequest, args listNotebooksArgs) (*mcp.CallToolResult, any, error) {
	notebooks, err := s.client.ListNotebooks(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list notebooks: %w", err)
	}

	countMap := map[string]int{}
	countRows, err := s.client.SQLQuery(ctx, "SELECT box, COUNT(*) AS count FROM blocks WHERE type = 'd' GROUP BY box")
	if err == nil {
		for _, row := range countRows {
			box := strVal(row, "box")
			count := intVal(row, "count")
			countMap[box] = count
		}
	}

	var items []notebookItem
	for _, nb := range notebooks {
		items = append(items, notebookItem{
			ID:       nb.ID,
			Name:     nb.Name,
			DocCount: countMap[nb.ID],
		})
	}

	if items == nil {
		items = []notebookItem{}
	}

	jsonBytes, err := json.Marshal(items)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to format results: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(jsonBytes)},
		},
	}, nil, nil
}

func strVal(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%v", val)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}

func intVal(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return int(val)
	case string:
		var n int
		fmt.Sscanf(val, "%d", &n)
		return n
	case nil:
		return 0
	}
	return 0
}
