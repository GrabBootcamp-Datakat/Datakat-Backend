package controller

import (
	"net/http"
	"skeleton-internship-backend/internal/dto"
	"skeleton-internship-backend/internal/model"
	"skeleton-internship-backend/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

type NLVController struct {
	nlvService service.NLVService
}

func NewNLVController(nlvService service.NLVService) *NLVController {
	return &NLVController{
		nlvService: nlvService,
	}
}

func RegisterNLVRoutes(router *gin.Engine, controller *NLVController) {
	v1 := router.Group("/api/v1/nlv")
	{
		v1.POST("/query", controller.HandleNLVQuery)
	}
}

// HandleNLVQuery godoc
// @Summary      Process Natural Language Query for Visualization
// @Description  Takes a natural language query and an optional conversation ID. Analyzes the query in the context of the conversation (using LLM), queries the appropriate data source (TimescaleDB for metrics, Elasticsearch for logs), and returns structured data suitable for frontend visualization.
// @Tags         nlv
// @Accept       json
// @Produce      json
// @Param        request body dto.NLVQueryRequest true "User query and optional conversation ID"
// @Success      200 {object} dto.NLVQueryResponse "Query processed. Contains data and visualization info, or an error message."
// @Failure      400 {object} model.Response "Invalid request body or parameters"
// @Failure      500 {object} model.Response "Internal server error during processing (e.g., LLM unavailable, DB error)"
// @Router       /api/v1/nlv/query [post]
func (c *NLVController) HandleNLVQuery(ctx *gin.Context) {
	var req dto.NLVQueryRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		log.Warn().Err(err).Msg("Invalid NLV request body")
		ctx.JSON(http.StatusBadRequest, model.NewResponse("Invalid request body: "+err.Error(), nil))
		return
	}

	resp, err := c.nlvService.ProcessNaturalLanguageQuery(ctx.Request.Context(), req)
	if err != nil {
		log.Error().Err(err).Str("query", req.Query).Msg("Internal error processing NLV query")
		ctx.JSON(http.StatusInternalServerError, model.NewResponse("Internal server error", nil))
		return
	}

	ctx.JSON(http.StatusOK, resp)
}
