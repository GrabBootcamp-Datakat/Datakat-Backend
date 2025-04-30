package controller

import (
	"net/http"
	"skeleton-internship-backend/internal/dto"
	"skeleton-internship-backend/internal/model"
	"skeleton-internship-backend/internal/service"
	"skeleton-internship-backend/internal/util"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

type LogController struct {
	logQueryService service.LogQueryService
}

func NewLogController(logQueryService service.LogQueryService) *LogController {
	return &LogController{
		logQueryService: logQueryService,
	}
}

func RegisterLogRoutes(router *gin.Engine, controller *LogController) {
	v1 := router.Group("/api/v1/logs")
	{
		v1.GET("", controller.GetLogs)
	}
}

// GetLogs godoc
// @Summary      Search and filter logs
// @Description  Retrieves logs based on specified time range, search query, levels, and applications. Supports pagination and sorting.
// @Tags         logs
// @Accept       json
// @Produce      json
// @Param        startTime    query     string  true   "Start time in ISO 8601 format (e.g., 2023-04-29T09:00:00Z) or epoch milliseconds"
// @Param        endTime      query     string  true   "End time in ISO 8601 format (e.g., 2023-04-29T10:00:00Z) or epoch milliseconds"
// @Param        query        query     string  false  "Free text search query"
// @Param        levels       query     string  false  "Comma-separated list of log levels (e.g., ERROR,WARN)"
// @Param        applications query     string  false  "Comma-separated list of application IDs (e.g., application_123,app_456)"
// @Param        sortBy       query     string  false  "Field to sort by (default: @timestamp)" Enums(@timestamp, level, component, application)
// @Param        sortOrder    query     string  false  "Sort order (asc or desc, default: desc)" Enums(asc, desc)
// @Param        page         query     int     false  "Page number (default: 1)" minimum(1)
// @Param        size         query     int     false  "Number of logs per page (default: 50, max: 1000)" minimum(1) maximum(1000)
// @Success      200          {object}  dto.LogSearchResponse "Successfully retrieved logs"
// @Failure      400          {object}  model.Response "Invalid query parameters"
// @Failure      500          {object}  model.Response "Internal server error"
// @Router       /api/v1/logs [get]
func (c *LogController) GetLogs(ctx *gin.Context) {
	startTimeStr := ctx.Query("startTime")
	endTimeStr := ctx.Query("endTime")
	query := ctx.Query("query")
	levelsStr := ctx.Query("levels")
	applicationsStr := ctx.Query("applications")
	sortBy := ctx.DefaultQuery("sortBy", "@timestamp")
	sortOrder := ctx.DefaultQuery("sortOrder", "desc")
	pageStr := ctx.DefaultQuery("page", "1")
	sizeStr := ctx.DefaultQuery("size", "50")

	var levels []string
	if levelsStr != "" {
		levels = strings.Split(levelsStr, ",")
		// Trim spaces
		for i := range levels {
			levels[i] = strings.TrimSpace(levels[i])
		}
	}
	startTime, errStart := util.ParseTimeFlexible(startTimeStr)
	endTime, errEnd := util.ParseTimeFlexible(endTimeStr)
	if errStart != nil || errEnd != nil {
		ctx.JSON(http.StatusBadRequest, model.NewResponse("Invalid startTime or endTime format. Use ISO 8601 or epoch milliseconds.", nil))
		return
	}
	var applications []string
	if applicationsStr != "" {
		applications = strings.Split(applicationsStr, ",")
		for i := range applications {
			applications[i] = strings.TrimSpace(applications[i])
		}
	}
	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		page = 1
	}
	size, err := strconv.Atoi(sizeStr)
	if err != nil || size <= 0 || size > 1000 {
		size = 500
	}
	searchReq := dto.LogSearchRequest{
		StartTime:    startTime,
		EndTime:      endTime,
		Query:        query,
		Levels:       levels,
		Applications: applications,
		SortBy:       sortBy,
		SortOrder:    sortOrder,
		Page:         page,
		Size:         size,
	}

	result, err := c.logQueryService.SearchLogs(ctx.Request.Context(), searchReq)
	if err != nil {
		log.Error().Err(err).Msg("Error searching logs")
		ctx.JSON(http.StatusInternalServerError, model.NewResponse("Failed to search logs", nil))
		return
	}

	ctx.JSON(http.StatusOK, result)

}
