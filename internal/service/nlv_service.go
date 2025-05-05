package service

import (
	"context"
	"skeleton-internship-backend/internal/dto"
	"skeleton-internship-backend/internal/model"
	"skeleton-internship-backend/internal/repository"
	"strings"
	"time"

	"skeleton-internship-backend/internal/util"

	"github.com/rs/zerolog/log"
)

type NLVService interface {
	ProcessNaturalLanguageQuery(ctx context.Context, req dto.NLVQueryRequest) (*dto.NLVQueryResponse, error)
}

type nlvService struct {
	llmService    LLMService
	metricRepo    repository.MetricRepository
	logRepo       repository.LogRepository
	schemaContext string
}

func NewNLVService(llmService LLMService, metricRepo repository.MetricRepository, logRepo repository.LogRepository) NLVService {
	schemaCtx := `
        TimescaleDB table 'log_metric_events': columns time (timestamp), metric_name (text, values: 'log_event', 'error_event'), application (text), tags (jsonb keys: 'level', 'component', 'error_key', 'parse_status').
        Elasticsearch index 'applogs-*': fields @timestamp, level (keyword), component (keyword), application (keyword), content (text), raw_log (text).
    `
	return &nlvService{
		llmService:    llmService,
		metricRepo:    metricRepo,
		logRepo:       logRepo,
		schemaContext: schemaCtx,
	}
}

func (s *nlvService) ProcessNaturalLanguageQuery(ctx context.Context, req dto.NLVQueryRequest) (*dto.NLVQueryResponse, error) {
	log.Info().Str("query", req.Query).Msg("Processing NLV query")

	// 1. Gọi LLM Service để phân tích
	analysis, err := s.llmService.AnalyzeQuery(ctx, req.Query, s.schemaContext)
	if err != nil {
		log.Error().Err(err).Msg("LLM analysis failed")
		return createErrorResponse(req.Query, "Failed to analyze query with LLM"), nil
	}

	// 2. Xử lý dựa trên Intent từ LLM
	switch analysis.Intent {
	case "query_metric":
		return s.handleMetricQuery(ctx, req.Query, analysis)
	case "query_log":
		return s.handleLogQuery(ctx, req.Query, analysis)
	default: // "unknown" hoặc intent không hỗ trợ
		log.Warn().Str("intent", analysis.Intent).Str("query", req.Query).Msg("LLM returned unknown or unsupported intent")
		return createErrorResponse(req.Query, "Sorry, I could not understand that query or it's not supported yet."), nil
	}
}

func (s *nlvService) handleMetricQuery(ctx context.Context, originalQuery string, analysis *dto.LLMAnalysisResult) (*dto.NLVQueryResponse, error) {

	startTime, errStart := util.ParseTimeInput(analysis.TimeRange.Start)
	endTime, errEnd := util.ParseTimeInput(analysis.TimeRange.End)
	if errStart != nil || errEnd != nil || endTime.Before(startTime) {
		log.Warn().Interface("range", analysis.TimeRange).Msg("LLM returned invalid time range")
		return createErrorResponse(originalQuery, "Could not understand the time range in your query."), nil
	}

	interval := determineInterval(startTime, endTime, analysis.GroupBy)

	metricReq := dto.MetricTimeseriesRequest{
		StartTime:    startTime,
		EndTime:      endTime,
		MetricName:   *analysis.MetricName,
		Interval:     interval,
		GroupBy:      determineGroupByField(analysis.GroupBy),
		Applications: extractApplicationsFromFilters(analysis.Filters),
	}

	result, err := s.metricRepo.GetTimeseriesMetrics(ctx, metricReq)
	if err != nil {
		log.Error().Err(err).Msg("Failed to get timeseries metrics from repository")
		return createErrorResponse(originalQuery, "Failed to retrieve metric data."), nil
	}

	// Chuyển đổi kết quả thành định dạng NLVQueryResponse
	resp := &dto.NLVQueryResponse{
		OriginalQuery:    originalQuery,
		InterpretedQuery: analysis,
		ResultType:       "timeseries",
		Columns:          []string{"timestamp", determineGroupByField(analysis.GroupBy), "value"},
		Data:             formatTimeseriesData(result.Series),
	}

	return resp, nil
}

func (s *nlvService) handleLogQuery(ctx context.Context, originalQuery string, analysis *dto.LLMAnalysisResult) (*dto.NLVQueryResponse, error) {
	startTime, errStart := util.ParseTimeInput(analysis.TimeRange.Start)
	endTime, errEnd := util.ParseTimeInput(analysis.TimeRange.End)
	if errStart != nil || errEnd != nil || endTime.Before(startTime) {
		return createErrorResponse(originalQuery, "Could not understand the time range."), nil
	}

	logReq := dto.LogSearchRequest{
		StartTime:    startTime,
		EndTime:      endTime,
		Query:        extractQueryTextFromFilters(analysis.Filters),
		Levels:       extractLevelsFromFilters(analysis.Filters),
		Applications: extractApplicationsFromFilters(analysis.Filters),
		Page:         1,
		Size:         50, // Giới hạn số log trả về
		SortBy:       "@timestamp",
		SortOrder:    "desc",
	}

	// Gọi Log Repository
	result, err := s.logRepo.Search(ctx, logReq)
	if err != nil {
		log.Error().Err(err).Msg("Failed to search logs from repository")
		return createErrorResponse(originalQuery, "Failed to retrieve log data."), nil
	}

	// Format response
	resp := &dto.NLVQueryResponse{
		OriginalQuery:    originalQuery,
		InterpretedQuery: analysis,
		ResultType:       "log_list",
		// Cần định nghĩa cột trả về cho log list
		Columns: []string{"@timestamp", "level", "component", "application", "content", "raw_log"},
		Data:    formatLogListData(result.Logs),
	}
	// Có thể thêm thông tin totalCount từ result nếu muốn
	return resp, nil
}

// --- Helper Functions ---

func createErrorResponse(query, message string) *dto.NLVQueryResponse {
	errMsg := message
	return &dto.NLVQueryResponse{
		OriginalQuery: query,
		ResultType:    "error",
		ErrorMessage:  &errMsg,
	}
}

// determineInterval chọn interval dựa trên khoảng thời gian và group by (logic cần cải thiện)
func determineInterval(start, end time.Time, groupBy []string) string {
	duration := end.Sub(start)
	if duration <= time.Hour*2 {
		return "1 minute"
	}
	if duration <= time.Hour*12 {
		return "5 minute"
	}
	if duration <= time.Hour*24*2 {
		return "10 minute"
	}
	if duration <= time.Hour*24*7 {
		return "1 hour"
	}
	return "1 day"
}

// determineGroupByField lấy trường group by chính từ LLM (logic đơn giản)
func determineGroupByField(groupBy []string) string {
	if len(groupBy) == 0 {
		return "total" // Không group by gì cả
	}
	// Ưu tiên các tag cụ thể trước
	for _, g := range groupBy {
		if strings.HasPrefix(g, "tags.") {
			return strings.TrimPrefix(g, "tags.") // Trả về key của tag
		}
	}
	// Nếu không có tag, trả về trường đầu tiên (ví dụ: application)
	return groupBy[0]
}

// formatTimeseriesData chuyển đổi kết quả repo thành mảng 2 chiều
/*
[]TimeseriesSeries{
  {
    Name: "INFO",
    Data: []TimeseriesDataPoint{                              [][]interface{}{
      {Timestamp: 1000, Value: 20},					          {1000, "INFO", 20},
      {Timestamp: 2000, Value: 30},					          {2000, "INFO", 30},
    },                                            -> 		  {1000, "INFO", 5},
      {Timestamp: 3000, Value: 40},					          {2000, "INFO", 8},
    },
  },
  {
    Name: "ERROR",
    Data: []TimeseriesDataPoint{
      {Timestamp: 1000, Value: 5},
      {Timestamp: 2000, Value: 8},
    },
  },
}
*/
func formatTimeseriesData(series []dto.TimeseriesSeries) [][]interface{} {
	if len(series) == 0 {
		return [][]interface{}{}
	}

	var formattedData [][]interface{}
	for _, s := range series {
		for _, dp := range s.Data {
			formattedData = append(formattedData, []interface{}{dp.Timestamp, s.Name, dp.Value})
		}
	}
	return formattedData
}

// formatLogListData chuyển đổi log entries thành mảng 2 chiều
func formatLogListData(logs []model.LogEntry) [][]interface{} {
	formattedData := make([][]interface{}, len(logs))
	for i, log := range logs {
		formattedData[i] = []interface{}{
			log.Timestamp.UnixMilli(), // Gửi epoch ms
			log.Level,
			log.Component,
			log.Application,
			log.Content,
			log.Raw,
		}
	}
	return formattedData
}

// Các hàm helper để trích xuất thông tin từ analysis.Filters (cần implement)
func extractQueryTextFromFilters(filters []dto.QueryFilter) string {
	for _, f := range filters {
		if f.Field == "content" || f.Field == "raw_log" {
			if q, ok := f.Value.(string); ok {
				return q
			}
		}
	}
	return ""
}
func extractLevelsFromFilters(filters []dto.QueryFilter) []string {
	for _, f := range filters {
		if f.Field == "level" || f.Field == "tags.level" {
			// Xử lý cả = và IN
			if f.Operator == "=" {
				if l, ok := f.Value.(string); ok {
					return []string{l}
				}
			} else if f.Operator == "IN" {
				if levels, ok := f.Value.([]string); ok {
					return levels
				}
				if levels, ok := f.Value.([]interface{}); ok {
					strLevels := []string{}
					for _, l := range levels {
						if ls, ok := l.(string); ok {
							strLevels = append(strLevels, ls)
						}
					}
					return strLevels
				}
			}
		}
	}
	return nil
}
func extractApplicationsFromFilters(filters []dto.QueryFilter) []string {
	// Tương tự extractLevelsFromFilters nhưng cho trường 'application'
	for _, f := range filters {
		if f.Field == "application" {
			if f.Operator == "=" {
				if a, ok := f.Value.(string); ok {
					return []string{a}
				}
			} else if f.Operator == "IN" {
				if apps, ok := f.Value.([]string); ok {
					return apps
				}
				if apps, ok := f.Value.([]interface{}); ok {
					strApps := []string{}
					for _, a := range apps {
						if as, ok := a.(string); ok {
							strApps = append(strApps, as)
						}
					}
					return strApps
				}
			}
		}
	}
	return nil
}
