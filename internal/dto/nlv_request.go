package dto

type NLVQueryRequest struct {
	Query string `json:"query" binding:"required"`
}
