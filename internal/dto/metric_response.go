package dto

type MetricSummaryResponse struct {
	TotalLogEvents   int64 `json:"totalLogEvents"`
	TotalErrorEvents int64 `json:"totalErrorEvents"`
}

// TimeseriesDataPoint
type TimeseriesDataPoint struct {
	Timestamp int64 `json:"timestamp"` // Epoch Milliseconds
	Value     int64 `json:"value"`
}

// TimeseriesSeries
type TimeseriesSeries struct {
	Name string                `json:"name"` // Tên của series (ví dụ: "INFO", "WARN", "YarnAllocator")
	Data []TimeseriesDataPoint `json:"data"`
}

// MetricTimeseriesResponse cấu trúc trả về cho API timeseries
type MetricTimeseriesResponse struct {
	Series []TimeseriesSeries `json:"series"`
}

// ApplicationListResponse cấu trúc trả về cho API lấy danh sách application
type ApplicationListResponse struct {
	Applications []string `json:"applications"`
}
