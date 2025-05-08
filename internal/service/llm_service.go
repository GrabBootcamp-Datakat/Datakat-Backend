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
	AnalyzeQueryWithHistory(ctx context.Context, conversationHistory []dto.ConversationTurn, newUserQuery string, schemaContext string) (*dto.LLMAnalysisResult, error)
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

func (s *geminiLLMService) AnalyzeQueryWithHistory(ctx context.Context, conversationHistory []dto.ConversationTurn, newUserQuery string, schemaContext string) (*dto.LLMAnalysisResult, error) {
	log.Info().Str("new_query", newUserQuery).Int("history_len", len(conversationHistory)).Msg("Gemini LLM Service: Analyzing query with history")

	prompt := buildGeminiContents(conversationHistory, newUserQuery, schemaContext)

	requestBody := GeminiRequestBody{Contents: prompt}
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal Gemini request body")
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	respBodyBytes, err := s.callGeminiAPI(ctx, bodyBytes)
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

func (s *geminiLLMService) callGeminiAPI(ctx context.Context, bodyBytes []byte) ([]byte, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", s.modelID, s.apiKey)

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
		return potentialJson
	}

	log.Warn().Str("potential_json", potentialJson).Msg("Could not validate potential JSON extracted from LLM response")
	return ""
}

func buildGeminiContents(history []dto.ConversationTurn, newUserQuery string, schemaContext string) []GeminiContent {
	contents := make([]GeminiContent, 0, len(history)+1)

	if len(history) == 0 {
		initialPrompt := buildInitialPrompt(newUserQuery, schemaContext)
		contents = append(contents, GeminiContent{
			Role:  "user",
			Parts: []GeminiPart{{Text: initialPrompt}},
		})
	} else {
		for _, turn := range history {
			contents = append(contents, GeminiContent{
				Role:  turn.Role,
				Parts: []GeminiPart{{Text: turn.Content}},
			})
		}
		followUpPrompt := buildFollowUpPrompt(newUserQuery)
		contents = append(contents, GeminiContent{
			Role:  "user",
			Parts: []GeminiPart{{Text: followUpPrompt}},
		})
	}

	return contents
}

func buildInitialPrompt(userQuery string, schemaContext string) string {
	return fmt.Sprintf(`
Analyze the user's natural language query to extract structured information for querying logs or metrics. Respond *ONLY* with a valid JSON object matching the specified format, without any introductory text or markdown formatting.

Data Schema Context:
%s

Desired JSON Output Format:
{
  "intent": ("query_metric" | "query_log" | "unknown"),
  "metric_name": (string | null),
  "time_range": { // ALWAYS use ISO 8601 format (YYYY-MM-DDTHH:mm:ssZ) or relative strings ("now", "now-1h", "now-7d"). DO NOT use formats like MM/DD/YYYY.
    "start": string,
    "end": string
  },
  "filters": [ { "field": string, "operator": string, "value": any } ],
  "group_by": (array[string] | null), // e.g., ["application", "tags.level"] or ["time_bucket('5m', time)", "application"]
  "aggregation": ("COUNT" | "AVG" | "SUM" | "NONE"),
  "sort": { "field": string, "order": ("asc" | "desc") } | null, // Optional: Infer from "top", "most", "least", "latest", "oldest". Field is often the aggregated "value" or a time field like "@timestamp" or "time".
  "limit": number | null, // Optional: Infer from "top 5", "only 1", etc.
  "visualization_hint": (string | null)
}

Example for "top 5 components with most errors in last hour":
{
  "intent": "query_metric",
  "metric_name": "error_event",
  "time_range": {"start": "now-1h", "end": "now"},
  "filters": [],
  "group_by": ["tags.component"],
  "aggregation": "COUNT",
  "sort": {"field": "value", "order": "desc"}, // Sort by the aggregated count
  "limit": 5,
  "visualization_hint": null
}

Example for "latest 10 logs containing 'timeout'":
{
  "intent": "query_log",
  "metric_name": null,
  "time_range": {"start": "now-1h", "end": "now"}, // Default range if not specified
  "filters": [{"field": "content", "operator": "CONTAINS", "value": "timeout"}],
  "group_by": null,
  "aggregation": "NONE",
  "sort": {"field": "@timestamp", "order": "desc"},
  "limit": 10,
  "visualization_hint": "table"
}


User Query: "%s"

JSON Output:`, schemaContext, userQuery)
}

func buildFollowUpPrompt(newUserQuery string) string {
	return fmt.Sprintf(`Follow-up User Query: "%s"

Based on the previous context and this new query, update the *entire* previous JSON analysis. For example, if the user asks to "group by level instead", only change the "group_by" field in the JSON, keeping other fields like time_range and filters the same unless explicitly changed by the new query. If the user asks for "top 3", add or update the "sort" and "limit" fields. Respond ONLY with the complete, updated, valid JSON object.

Updated JSON Output:`, newUserQuery)
}
