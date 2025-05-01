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
// @Description  Takes a natural language query, analyzes it (using LLM simulation), queries data, and returns formatted data for frontend visualization.
// @Tags         nlv
// @Accept       json
// @Produce      json
// @Param        request body dto.NLVQueryRequest true "User's natural language query"
// @Success      200 {object} dto.NLVQueryResponse "Query processed successfully (may contain data or error message)"
// @Failure      400 {object} model.Response "Invalid request body"
// @Failure      500 {object} model.Response "Internal server error during processing"
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
