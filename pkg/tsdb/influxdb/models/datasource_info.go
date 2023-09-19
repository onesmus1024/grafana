package models

import (
	"net/http"
)

type ExemplarSetting struct {
	DatasourceUid string `json:"datasourceUid"`
	Name          string `json:"name"`
}

type DatasourceInfo struct {
	HTTPClient *http.Client

	Token string
	URL   string

	DbName        string `json:"dbName"`
	Version       string `json:"version"`
	HTTPMode      string `json:"httpMode"`
	TimeInterval  string `json:"timeInterval"`
	DefaultBucket string `json:"defaultBucket"`
	Organization  string `json:"organization"`
	MaxSeries     int    `json:"maxSeries"`

	// Flight SQL metadata
	Metadata []map[string]string `json:"metadata"`
	// FlightSQL grpc connection
	SecureGrpc bool `json:"secureGrpc"`

	// Exemplar settings
	ExemplarTraceIdDestinations []ExemplarSetting `json:"exemplarTraceIdDestinations"`
}
