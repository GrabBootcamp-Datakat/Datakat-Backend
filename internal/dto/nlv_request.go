package dto

type NLVQueryRequest struct {
	Query          string  `json:"query" binding:"required"`
	ConversationId *string `json:"conversationId,omitempty"`
}
