package model

import "time"

type MetricEvent struct {
	Time        time.Time         `json:"time"`
	MetricName  string            `json:"metric_name"`
	Application string            `json:"application"`
	Tags        map[string]string `json:"tags"`
}
