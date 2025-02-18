package influxdb

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/datasource"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"

	"github.com/grafana/grafana/pkg/tsdb/influxdb/flux"
	"github.com/grafana/grafana/pkg/tsdb/influxdb/fsql"

	"github.com/grafana/grafana/pkg/infra/httpclient"
	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/tsdb/influxdb/influxql"
	"github.com/grafana/grafana/pkg/tsdb/influxdb/models"
)

var logger log.Logger = log.New("tsdb.influxdb")

type Service struct {
	im instancemgmt.InstanceManager
}

func ProvideService(httpClient httpclient.Provider) *Service {
	return &Service{
		im: datasource.NewInstanceManager(newInstanceSettings(httpClient)),
	}
}

func newInstanceSettings(httpClientProvider httpclient.Provider) datasource.InstanceFactoryFunc {
	return func(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
		opts, err := settings.HTTPClientOptions(ctx)
		if err != nil {
			return nil, err
		}

		client, err := httpClientProvider.New(opts)
		if err != nil {
			return nil, err
		}
		//fmt.Println("Received JSONData:", string(settings.JSONData))

		jsonData := models.DatasourceInfo{}
		err = json.Unmarshal(settings.JSONData, &jsonData)
		if err != nil {
			return nil, fmt.Errorf("error reading settings: %w", err)
		}

		httpMode := jsonData.HTTPMode
		if httpMode == "" {
			httpMode = "GET"
		}

		maxSeries := jsonData.MaxSeries
		if maxSeries == 0 {
			maxSeries = 1000
		}

		version := jsonData.Version
		if version == "" {
			version = influxVersionInfluxQL
		}

		database := jsonData.DbName
		if database == "" {
			database = settings.Database
		}

		model := &models.DatasourceInfo{
			HTTPClient:                  client,
			URL:                         settings.URL,
			DbName:                      database,
			Version:                     version,
			HTTPMode:                    httpMode,
			TimeInterval:                jsonData.TimeInterval,
			DefaultBucket:               jsonData.DefaultBucket,
			Organization:                jsonData.Organization,
			Metadata:                    jsonData.Metadata,
			MaxSeries:                   maxSeries,
			SecureGrpc:                  true,
			Token:                       settings.DecryptedSecureJSONData["token"],
			ExemplarTraceIdDestinations: jsonData.ExemplarTraceIdDestinations,
		}
		return model, nil
	}
}

func (s *Service) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	logger := logger.FromContext(ctx)

	dsInfo, err := s.getDSInfo(ctx, req.PluginContext)
	if err != nil {
		return nil, err
	}

	logger.Info(fmt.Sprintf("Making a %s type query", dsInfo.Version))

	switch dsInfo.Version {
	case influxVersionFlux:
		return flux.Query(ctx, dsInfo, *req)
	case influxVersionInfluxQL:
		// Check if ExemplarTraceIdDestinations is not empty
		if len(dsInfo.ExemplarTraceIdDestinations) > 0 {
			// Call the function to query exemplar data
			influxql.QueryExemplarData(ctx, dsInfo, req)
		}
		return influxql.Query(ctx, dsInfo, req)
	case influxVersionSQL:
		return fsql.Query(ctx, dsInfo, *req)
	default:
		return nil, fmt.Errorf("unknown influxdb version")
	}
}

func (s *Service) getDSInfo(ctx context.Context, pluginCtx backend.PluginContext) (*models.DatasourceInfo, error) {
	i, err := s.im.Get(ctx, pluginCtx)
	if err != nil {
		return nil, err
	}

	instance, ok := i.(*models.DatasourceInfo)
	if !ok {
		return nil, fmt.Errorf("failed to cast datsource info")
	}
	//fmt.Println("Exemplar Data:", instance.ExemplarTraceIdDestinations)

	return instance, nil
}
