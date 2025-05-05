package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"skeleton-internship-backend/config"
	"skeleton-internship-backend/internal/dto"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

type GeminiPart struct {
	Text string `json:"text"`
}
type GeminiContent struct {
	Parts []GeminiPart `json:"parts"`
	Role  string       `json:"role,omitempty"`
}
type GeminiRequestBody struct {
	Contents []GeminiContent `json:"contents"`
}

type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
	Index        int           `json:"index"`
}

type GeminiResponse struct {
	Candidates []GeminiCandidate `json:"candidates"`
}

type LLMService interface {
	AnalyzeQuery(ctx context.Context, userQuery string, schemaContext string) (*dto.LLMAnalysisResult, error)
}

type geminiLLMService struct {
	apiKey     string
	httpClient *http.Client
	modelID    string
}

func NewGeminiLLMService(cfg *config.Config) (LLMService, error) {
	return &geminiLLMService{
		apiKey: cfg.APIKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		modelID: "gemini-1.5-flash-latest",
	}, nil
}

func (s *geminiLLMService) AnalyzeQuery(ctx context.Context, userQuery string, schemaContext string) (*dto.LLMAnalysisResult, error) {
	log.Info().Str("user_query", userQuery).Msg("Gemini LLM Service: Analyzing query")

	prompt := buildPrompt(userQuery, schemaContext)
	log.Debug().Str("prompt", prompt).Msg("Gemini LLM Service: Generated prompt")

	respBodyBytes, err := s.callGeminiAPI(ctx, prompt)
	if err != nil {
		return nil, err
	}
	log.Debug().Bytes("raw_response", respBodyBytes).Msg("Gemini LLM Service: Received raw response")

	//  Parse Response Gemini
	var geminiResp GeminiResponse
	if err := json.Unmarshal(respBodyBytes, &geminiResp); err != nil {
		log.Error().Err(err).Bytes("response_body", respBodyBytes).Msg("Failed to unmarshal Gemini API response")
		return nil, fmt.Errorf("failed to parse Gemini response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		log.Error().Interface("gemini_response", geminiResp).Msg("Gemini response has no candidates or parts")
		return nil, errors.New("received empty or invalid response structure from Gemini")
	}

	generatedText := geminiResp.Candidates[0].Content.Parts[0].Text
	log.Debug().Str("generated_text", generatedText).Msg("Gemini LLM Service: Extracted generated text")

	cleanedJson := cleanLLMJsonOutput(generatedText)
	if cleanedJson == "" {
		log.Error().Str("raw_text", generatedText).Msg("Failed to extract valid JSON from Gemini response text")
		return nil, errors.New("LLM did not return valid JSON in its response")
	}

	var analysisResult dto.LLMAnalysisResult
	decoder := json.NewDecoder(strings.NewReader(cleanedJson))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&analysisResult); err != nil {
		log.Error().Err(err).Str("cleaned_json", cleanedJson).Msg("Failed to unmarshal cleaned JSON from Gemini response")
		return nil, fmt.Errorf("failed to parse structured analysis from LLM: %w", err)
	}

	log.Info().Interface("analysis_result", analysisResult).Msg("Gemini LLM Service: Successfully analyzed query")
	return &analysisResult, nil
}

func (s *geminiLLMService) callGeminiAPI(ctx context.Context, prompt string) ([]byte, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", s.modelID, s.apiKey)

	requestBody := GeminiRequestBody{
		Contents: []GeminiContent{
			{
				Role: "user",
				Parts: []GeminiPart{
					{Text: prompt},
				},
			},
		},
	}
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal Gemini request body")
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}
	log.Trace().RawJSON("request_body", bodyBytes).Msg("Gemini request body")

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		log.Error().Err(err).Msg("Failed to create Gemini HTTP request")
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Error().Err(err).Msg("Gemini HTTP request failed")
		return nil, fmt.Errorf("gemini request failed: %w", err)
	}
	defer resp.Body.Close()

	respBodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read Gemini response body")
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Error().Int("status_code", resp.StatusCode).Bytes("response_body", respBodyBytes).Msg("Gemini API returned non-OK status")
		return nil, fmt.Errorf("gemini API error: status code %d", resp.StatusCode)
	}

	return respBodyBytes, nil
}

func buildPrompt(userQuery string, schemaContext string) string {
	return fmt.Sprintf(
		`Analyze the user's natural language query to extract structured information for querying logs or metrics. Respond *ONLY* with a valid JSON object matching the specified format, without any introductory text or markdown formatting.
		Data Schema Context:
		%s
		Desired JSON Output Format:
		{
		"intent": ("query_metric" | "query_log" | "unknown"), // Identify if the user wants aggregated metrics or raw logs.
		"metric_name": (string | null), // "error_event" or "log_event" if intent is "query_metric", otherwise null.
		"time_range": { // Always extract or infer a time range. Default to "now-1h" to "now" if not specified.
			"start": (string), // ISO8601 format or relative like "now-1h", "yesterday".
			"end": (string)    // ISO8601 format or relative like "now".
		},
		"filters": [ // List of filters extracted from the query. Map field names based on intent.
			// Example for metrics: { "field": "tags.level", "operator": "=", "value": "ERROR" }
			// Example for logs: { "field": "component.keyword", "operator": "!=", "value": "Heartbeat" }
			// Example for logs text search: { "field": "content", "operator": "CONTAINS", "value": "connection refused" }
			// Example for multiple apps: { "field": "application", "operator": "IN", "value": ["app_123", "app_456"] }
			{ "field": string, "operator": ("=" | "!=" | "IN" | "NOT IN" | "CONTAINS"), "value": (string | array[string] | number) }
		],
		"group_by": (array[string] | null), // Fields to group by for metrics (e.g., ["application", "tags.level"]). Null for logs or no aggregation. Include time bucketing like "time_bucket('5m', time)" if applicable based on query and time range.
		"aggregation": ("COUNT" | "AVG" | "SUM" | "NONE"), // Aggregation for metrics. "NONE" for logs. Default to "COUNT" for metrics if not specified.
		"visualization_hint": (string | null) // User's preference like "bar", "line", "table". Null if not mentioned.
		}
		User Query: "%s"

JSON Output:`, schemaContext, userQuery)
}

func cleanLLMJsonOutput(raw string) string {
	startIndex := strings.Index(raw, "{")
	if startIndex == -1 {
		return ""
	}

	endIndex := strings.LastIndex(raw, "}")
	if endIndex == -1 || endIndex < startIndex {
		return ""
	}

	potentialJson := raw[startIndex : endIndex+1]

	var js map[string]interface{}
	if json.Unmarshal([]byte(potentialJson), &js) == nil {
		return potentialJson // Trả về nếu parse thành công
	}

	log.Warn().Str("potential_json", potentialJson).Msg("Could not validate potential JSON extracted from LLM response")
	return ""
}
