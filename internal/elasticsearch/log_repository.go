package elasticsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"skeleton-internship-backend/config"
	"skeleton-internship-backend/internal/dto"
	"skeleton-internship-backend/internal/model"
	"skeleton-internship-backend/internal/repository"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/typedapi/core/search"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/operator"
	"github.com/elastic/go-elasticsearch/v8/typedapi/types/enums/sortorder"
	"github.com/rs/zerolog/log"
)

type elasticsearchLogRepository struct {
	esTypedClient *elasticsearch.TypedClient
	indexPrefix   string
}

func NewElasticsearchLogRepository(cfg *config.Config) (repository.LogRepository, error) {
	transport := &http.Transport{
		MaxIdleConnsPerHost:   10,
		ResponseHeaderTimeout: time.Second * 10,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
	}
	esCfgForTyped := elasticsearch.Config{
		Addresses: cfg.Elasticsearch.Addresses,
		Transport: transport,
	}

	typedClient, err := elasticsearch.NewTypedClient(esCfgForTyped)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create Typed Elasticsearch Client in Repository")
		return nil, err
	}

	return &elasticsearchLogRepository{
		esTypedClient: typedClient,
		indexPrefix:   cfg.Elasticsearch.LogIndex,
	}, nil
}

func (r *elasticsearchLogRepository) Search(ctx context.Context, req dto.LogSearchRequest) (*dto.LogSearchResponse, error) {
	indexPattern := fmt.Sprintf("%s-*", r.indexPrefix)
	queryParts := []types.Query{}

	startTimeStr := req.StartTime.Format(time.RFC3339)
	endTimeStr := req.EndTime.Format(time.RFC3339)

	queryParts = append(queryParts, types.Query{
		Range: map[string]types.RangeQuery{
			"@timestamp": types.DateRangeQuery{
				Gte: &startTimeStr,
				Lte: &endTimeStr,
			},
		},
	})

	if req.Query != "" {
		queryString := req.Query

		queryParts = append(queryParts, types.Query{
			QueryString: &types.QueryStringQuery{
				Query:  queryString,
				Fields: []string{"content", "component", "application", "level", "raw_log"},
				DefaultOperator: &operator.Operator{
					Name: "AND",
				},
			},
		})
	}

	if len(req.Levels) > 0 {
		levelTerms := make([]types.FieldValue, len(req.Levels))
		for i, level := range req.Levels {
			levelTerms[i] = level
		}
		queryParts = append(queryParts, types.Query{
			Terms: &types.TermsQuery{
				TermsQuery: map[string]types.TermsQueryField{
					"level.keyword": levelTerms,
				},
			},
		})
	}

	if len(req.Applications) > 0 {
		appTerms := make([]types.FieldValue, len(req.Applications))
		for i, app := range req.Applications {
			appTerms[i] = app
		}
		queryParts = append(queryParts, types.Query{
			Terms: &types.TermsQuery{
				TermsQuery: map[string]types.TermsQueryField{
					"application.keyword": appTerms,
				},
			},
		})
	}

	from := (req.Page - 1) * req.Size
	order := sortorder.Desc
	if req.SortOrder == "asc" {
		order = sortorder.Asc
	}
	sortField := req.SortBy
	if sortField != "@timestamp" {
		knownKeywordFields := map[string]bool{
			"level":       true,
			"component":   true,
			"application": true,
			"source_file": true,
		}
		if knownKeywordFields[sortField] {
			sortField = fmt.Sprintf("%s.keyword", req.SortBy)
			log.Debug().Str("original_sort", req.SortBy).Str("effective_sort", sortField).Msg("Appending .keyword for sorting")
		} else {
			log.Warn().Str("sort_field", req.SortBy).Msg("Attempting to sort on unknown field")
		}
	}

	searchRequest := &search.Request{
		Query: &types.Query{
			Bool: &types.BoolQuery{
				Filter: queryParts,
			},
		},
		Size: &req.Size,
		From: &from,
		Sort: []types.SortCombinations{
			types.SortOptions{
				SortOptions: map[string]types.FieldSort{
					sortField: {Order: &order},
				},
			},
		},
	}

	res, err := r.esTypedClient.Search().
		Index(indexPattern).
		Request(searchRequest).
		Do(ctx)

	if err != nil {
		log.Error().Err(err).Msg("Error executing Elasticsearch search via TypedClient")
		return nil, fmt.Errorf("elasticsearch search failed: %w", err)
	}

	logs := make([]model.LogEntry, 0, len(res.Hits.Hits))
	for _, hit := range res.Hits.Hits {
		var entry model.LogEntry
		if hit.Source_ != nil {
			if err := json.Unmarshal(hit.Source_, &entry); err != nil {
				log.Error().Err(err).Msg("Error unmarshalling Elasticsearch hit source")
				continue
			}
			logs = append(logs, entry)
		}
	}

	response := &dto.LogSearchResponse{
		Logs:       logs,
		TotalCount: res.Hits.Total.Value,
		Page:       req.Page,
		Size:       req.Size,
	}

	log.Debug().Int64("total_hits", response.TotalCount).Int("returned_hits", len(response.Logs)).Msg("Elasticsearch search successful")
	return response, nil
}
