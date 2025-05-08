package dto

type SortInfo struct {
	Field string `json:"field"`
	Order string `json:"order"`
}
type LLMAnalysisResult struct {
	Intent            string        `json:"intent"`      // "query_metric", "query_log", "unknown"
	MetricName        *string       `json:"metric_name"` // "error_event" | "log_event" | null
	TimeRange         TimeRange     `json:"time_range"`
	Filters           []QueryFilter `json:"filters"`
	GroupBy           []string      `json:"group_by"`
	Aggregation       string        `json:"aggregation"` // "COUNT", "AVG", "SUM", "NONE"
	VisualizationHint *string       `json:"visualization_hint"`
	Sort              *SortInfo     `json:"sort,omitempty"`
	Limit             *int          `json:"limit,omitempty"`
}

type TimeRange struct {
	Start string `json:"start"` // "now-1h", "ISO8601", epoch ms
	End   string `json:"end"`   // "now", "ISO8601", epoch ms
}

type QueryFilter struct {
	Field    string      `json:"field"`    // "level", "component", "tags.error_key", "application"
	Operator string      `json:"operator"` // "=", "!=", "IN", "NOT IN", "CONTAINS" (cho text)
	Value    interface{} `json:"value"`    // string, []string, number
}

type NLVQueryResponse struct {
	ConversationId   string             `json:"conversationId"`
	OriginalQuery    string             `json:"originalQuery"`
	InterpretedQuery *LLMAnalysisResult `json:"interpretedQuery,omitempty"` // (optional)
	ResultType       string             `json:"resultType"`                 // "timeseries", "table", "scalar", "log_list", "error"
	Columns          []string           `json:"columns,omitempty"`          // Tên các cột dữ liệu trả về
	Data             [][]interface{}    `json:"data,omitempty"`             // [[val1, val2,...], [val1, val2,...]]
	ErrorMessage     *string            `json:"errorMessage,omitempty"`
}

type ConversationTurn struct {
	Role    string `json:"role"`    // "user" | "model"
	Content string `json:"content"` // Prompt | JSON Analysis
}
