package controller

import (
	"errors"
	"net/http"
	"skeleton-internship-backend/internal/dto"
	"skeleton-internship-backend/internal/model"
	"skeleton-internship-backend/internal/service"
	"skeleton-internship-backend/internal/util"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

type MetricController struct {
	metricQueryService service.MetricQueryService
}

func NewMetricController(metricQueryService service.MetricQueryService) *MetricController {
	return &MetricController{
		metricQueryService: metricQueryService,
	}
}

func RegisterMetricRoutes(router *gin.Engine, controller *MetricController) {
	v1Metrics := router.Group("/api/v1/metrics")
	{
		v1Metrics.GET("/summary", controller.GetSummaryMetrics)
		v1Metrics.GET("/timeseries", controller.GetTimeseriesMetrics)
	}
	v1Logs := router.Group("/api/v1/logs")
	{
		v1Logs.GET("/applications", controller.GetApplications)
	}
}

// GetSummaryMetrics godoc
// @Summary      Get summary metrics
// @Description  Retrieves total log and error counts within a time range, optionally filtered by applications.
// @Tags         metrics
// @Accept       json
// @Produce      json
// @Param        startTime    query     string  true   "Start time (ISO 8601 or epoch ms)"
// @Param        endTime      query     string  true   "End time (ISO 8601 or epoch ms)"
// @Param        applications query     string  false  "Comma-separated list of application IDs"
// @Success      200          {object}  dto.MetricSummaryResponse "Successfully retrieved summary metrics"
// @Failure      400          {object}  model.Response "Invalid query parameters"
// @Failure      500          {object}  model.Response "Internal server error"
// @Router       /api/v1/metrics/summary [get]
func (c *MetricController) GetSummaryMetrics(ctx *gin.Context) {
	startTime, endTime, applications, err := parseBaseQueryParams(ctx)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.NewResponse(err.Error(), nil))
		return
	}

	req := dto.MetricSummaryRequest{
		StartTime:    startTime,
		EndTime:      endTime,
		Applications: applications,
	}

	result, err := c.metricQueryService.GetSummary(ctx.Request.Context(), req)
	if err != nil {
		log.Error().Err(err).Msg("Error getting summary metrics")
		ctx.JSON(http.StatusInternalServerError, model.NewResponse("Failed to get summary metrics", nil))
		return
	}
	ctx.JSON(http.StatusOK, result)
}

// GetTimeseriesMetrics godoc
// @Summary      Get timeseries metrics
// @Description  Retrieves timeseries data for a specific metric, aggregated over an interval and optionally grouped by a tag.
// @Tags         metrics
// @Accept       json
// @Produce      json
// @Param        startTime    query     string  true   "Start time (ISO 8601 or epoch ms)"
// @Param        endTime      query     string  true   "End time (ISO 8601 or epoch ms)"
// @Param        applications query     string  false  "Comma-separated list of application IDs"
// @Param        metricName   query     string  true   "Metric name (e.g., log_event, error_event)" Enums(log_event, error_event)
// @Param        interval     query     string  true   "Time interval for bucketing (e.g., '5 minute', '1 hour')" Enums(1 minute, 5 minute, 10 minute, 30 minute, 1 hour, 1 day)
// @Param        groupBy      query     string  false  "Tag key to group by (e.g., level, component, error_key, application, total)" Enums(level, component, error_key, application, total)
// @Success      200          {object}  dto.MetricTimeseriesResponse "Successfully retrieved timeseries metrics"
// @Failure      400          {object}  model.Response "Invalid query parameters"
// @Failure      500          {object}  model.Response "Internal server error"
// @Router       /api/v1/metrics/timeseries [get]
func (c *MetricController) GetTimeseriesMetrics(ctx *gin.Context) {
	startTime, endTime, applications, err := parseBaseQueryParams(ctx)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.NewResponse(err.Error(), nil))
		return
	}

	metricName := ctx.Query("metricName")
	interval := ctx.Query("interval")
	groupBy := ctx.DefaultQuery("groupBy", "total") // Mặc định là total nếu không truyền

	if metricName == "" {
		ctx.JSON(http.StatusBadRequest, model.NewResponse("metricName is required", nil))
		return
	}
	if interval == "" {
		ctx.JSON(http.StatusBadRequest, model.NewResponse("interval is required", nil))
		return
	}

	req := dto.MetricTimeseriesRequest{
		StartTime:    startTime,
		EndTime:      endTime,
		Applications: applications,
		MetricName:   metricName,
		Interval:     interval,
		GroupBy:      groupBy,
	}

	result, err := c.metricQueryService.GetTimeseries(ctx.Request.Context(), req)
	if err != nil {
		log.Error().Err(err).Msg("Error getting timeseries metrics")
		// Kiểm tra lỗi validation từ service
		if strings.Contains(err.Error(), "invalid") {
			ctx.JSON(http.StatusBadRequest, model.NewResponse(err.Error(), nil))
		} else {
			ctx.JSON(http.StatusInternalServerError, model.NewResponse("Failed to get timeseries metrics", nil))
		}
		return
	}
	ctx.JSON(http.StatusOK, result)
}

// GetApplications godoc
// @Summary      Get distinct application IDs
// @Description  Retrieves a list of unique application IDs found within a time range.
// @Tags         logs
// @Accept       json
// @Produce      json
// @Param        startTime    query     string  true   "Start time (ISO 8601 or epoch ms)"
// @Param        endTime      query     string  true   "End time (ISO 8601 or epoch ms)"
// @Success      200          {object}  dto.ApplicationListResponse "Successfully retrieved application list"
// @Failure      400          {object}  model.Response "Invalid query parameters"
// @Failure      500          {object}  model.Response "Internal server error"
// @Router       /api/v1/logs/applications [get]
func (c *MetricController) GetApplications(ctx *gin.Context) {
	startTime, endTime, _, err := parseBaseQueryParams(ctx) // Không cần applications ở đây
	if err != nil {
		ctx.JSON(http.StatusBadRequest, model.NewResponse(err.Error(), nil))
		return
	}

	req := dto.ApplicationListRequest{
		StartTime: startTime,
		EndTime:   endTime,
	}

	result, err := c.metricQueryService.GetApplications(ctx.Request.Context(), req)
	if err != nil {
		log.Error().Err(err).Msg("Error getting applications")
		ctx.JSON(http.StatusInternalServerError, model.NewResponse("Failed to get applications", nil))
		return
	}
	ctx.JSON(http.StatusOK, result)
}

func parseBaseQueryParams(ctx *gin.Context) (time.Time, time.Time, []string, error) {
	startTimeStr := ctx.Query("startTime")
	endTimeStr := ctx.Query("endTime")
	applicationsStr := ctx.Query("applications")

	if startTimeStr == "" || endTimeStr == "" {
		return time.Time{}, time.Time{}, nil, errors.New("startTime and endTime are required query parameters")
	}

	startTime, errStart := util.ParseTimeFlexible(startTimeStr)
	endTime, errEnd := util.ParseTimeFlexible(endTimeStr)
	if errStart != nil || errEnd != nil {
		return time.Time{}, time.Time{}, nil, errors.New("invalid startTime or endTime format. Use ISO 8601 or epoch milliseconds")
	}
	if endTime.Before(startTime) {
		return time.Time{}, time.Time{}, nil, errors.New("endTime cannot be before startTime")
	}

	var applications []string
	if applicationsStr != "" {
		applications = strings.Split(applicationsStr, ",")
		for i := range applications {
			applications[i] = strings.TrimSpace(applications[i])
		}
	}
	return startTime, endTime, applications, nil
}
