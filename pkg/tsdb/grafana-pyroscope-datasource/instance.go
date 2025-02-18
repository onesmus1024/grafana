package pyroscope

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/tracing"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/grafana/grafana/pkg/infra/httpclient"
	"github.com/grafana/grafana/pkg/services/accesscontrol"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var (
	_ backend.QueryDataHandler    = (*PyroscopeDatasource)(nil)
	_ backend.CallResourceHandler = (*PyroscopeDatasource)(nil)
	_ backend.CheckHealthHandler  = (*PyroscopeDatasource)(nil)
	_ backend.StreamHandler       = (*PyroscopeDatasource)(nil)
)

type ProfilingClient interface {
	ProfileTypes(context.Context) ([]*ProfileType, error)
	LabelNames(ctx context.Context) ([]string, error)
	LabelValues(ctx context.Context, label string) ([]string, error)
	GetSeries(ctx context.Context, profileTypeID string, labelSelector string, start int64, end int64, groupBy []string, step float64) (*SeriesResponse, error)
	GetProfile(ctx context.Context, profileTypeID string, labelSelector string, start int64, end int64, maxNodes *int64) (*ProfileResponse, error)
}

// PyroscopeDatasource is a datasource for querying application performance profiles.
type PyroscopeDatasource struct {
	httpClient *http.Client
	client     ProfilingClient
	settings   backend.DataSourceInstanceSettings
	ac         accesscontrol.AccessControl
}

// NewPyroscopeDatasource creates a new datasource instance.
func NewPyroscopeDatasource(ctx context.Context, httpClientProvider httpclient.Provider, settings backend.DataSourceInstanceSettings, ac accesscontrol.AccessControl) (instancemgmt.Instance, error) {
	ctxLogger := logger.FromContext(ctx)
	opt, err := settings.HTTPClientOptions(ctx)
	if err != nil {
		ctxLogger.Error("Failed to get HTTP client options", "error", err, "function", logEntrypoint())
		return nil, err
	}
	httpClient, err := httpClientProvider.New(opt)
	if err != nil {
		ctxLogger.Error("Failed to create HTTP client", "error", err, "function", logEntrypoint())
		return nil, err
	}

	return &PyroscopeDatasource{
		httpClient: httpClient,
		client:     NewPyroscopeClient(httpClient, settings.URL),
		settings:   settings,
		ac:         ac,
	}, nil
}

func (d *PyroscopeDatasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	ctxLogger := logger.FromContext(ctx)
	ctx, span := tracing.DefaultTracer().Start(ctx, "datasource.pyroscope.CallResource", trace.WithAttributes(attribute.String("path", req.Path), attribute.String("method", req.Method)))
	defer span.End()
	ctxLogger.Debug("CallResource", "Path", req.Path, "Method", req.Method, "Body", req.Body, "function", logEntrypoint())
	if req.Path == "profileTypes" {
		return d.profileTypes(ctx, req, sender)
	}
	if req.Path == "labelNames" {
		return d.labelNames(ctx, req, sender)
	}
	if req.Path == "labelValues" {
		return d.labelValues(ctx, req, sender)
	}
	return sender.Send(&backend.CallResourceResponse{
		Status: 404,
	})
}

func (d *PyroscopeDatasource) profileTypes(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	ctxLogger := logger.FromContext(ctx)
	types, err := d.client.ProfileTypes(ctx)
	if err != nil {
		ctxLogger.Error("Received error from client", "error", err, "function", logEntrypoint())
		return err
	}
	bodyData, err := json.Marshal(types)
	if err != nil {
		ctxLogger.Error("Failed to marshal response", "error", err, "function", logEntrypoint())
		return err
	}
	err = sender.Send(&backend.CallResourceResponse{Body: bodyData, Headers: req.Headers, Status: 200})
	if err != nil {
		ctxLogger.Error("Failed to send response", "error", err, "function", logEntrypoint())
		return err
	}
	return nil
}

func (d *PyroscopeDatasource) labelNames(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	ctxLogger := logger.FromContext(ctx)
	res, err := d.client.LabelNames(ctx)
	if err != nil {
		ctxLogger.Error("Received error from client", "error", err, "function", logEntrypoint())
		return fmt.Errorf("error calling LabelNames: %v", err)
	}
	data, err := json.Marshal(res)
	if err != nil {
		ctxLogger.Error("Failed to marshal response", "error", err, "function", logEntrypoint())
		return err
	}
	err = sender.Send(&backend.CallResourceResponse{Body: data, Headers: req.Headers, Status: 200})
	if err != nil {
		ctxLogger.Error("Failed to send response", "error", err, "function", logEntrypoint())
		return err
	}
	return nil
}

type LabelValuesPayload struct {
	Query string
	Label string
	Start int64
	End   int64
}

func (d *PyroscopeDatasource) labelValues(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	ctxLogger := logger.FromContext(ctx)
	u, err := url.Parse(req.URL)
	if err != nil {
		ctxLogger.Error("Failed to parse URL", "error", err, "function", logEntrypoint())
		return err
	}
	query := u.Query()

	res, err := d.client.LabelValues(ctx, query["label"][0])
	if err != nil {
		ctxLogger.Error("Received error from client", "error", err, "function", logEntrypoint())
		return fmt.Errorf("error calling LabelValues: %v", err)
	}

	data, err := json.Marshal(res)
	if err != nil {
		ctxLogger.Error("Failed to marshal response", "error", err, "function", logEntrypoint())
		return err
	}

	err = sender.Send(&backend.CallResourceResponse{Body: data, Headers: req.Headers, Status: 200})
	if err != nil {
		ctxLogger.Error("Failed to send response", "error", err, "function", logEntrypoint())
		return err
	}

	return nil
}

// QueryData handles multiple queries and returns multiple responses.
// req contains the queries []DataQuery (where each query contains RefID as a unique identifier).
// The QueryDataResponse contains a map of RefID to the response for each query, and each response
// contains Frames ([]*Frame).
func (d *PyroscopeDatasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	ctxLogger := logger.FromContext(ctx)
	ctxLogger.Debug("Processing queries", "queryLenght", len(req.Queries), "function", logEntrypoint())

	// create response struct
	response := backend.NewQueryDataResponse()

	// loop over queries and execute them individually.
	for i, q := range req.Queries {
		ctxLogger.Debug("Processing query", "counter", i, "function", logEntrypoint())
		res := d.query(ctx, req.PluginContext, q)

		// save the response in a hashmap
		// based on with RefID as identifier
		response.Responses[q.RefID] = res
	}

	ctxLogger.Debug("All queries processed", "function", logEntrypoint())
	return response, nil
}

// CheckHealth handles health checks sent from Grafana to the plugin.
// The main use case for these health checks is the test button on the
// datasource configuration page which allows users to verify that
// a datasource is working as expected.
func (d *PyroscopeDatasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	logger.FromContext(ctx).Debug("CheckHealth called", "function", logEntrypoint())

	status := backend.HealthStatusOk
	message := "Data source is working"

	if _, err := d.client.ProfileTypes(ctx); err != nil {
		status = backend.HealthStatusError
		message = err.Error()
	}

	return &backend.CheckHealthResult{
		Status:  status,
		Message: message,
	}, nil
}

// SubscribeStream is called when a client wants to connect to a stream. This callback
// allows sending the first message.
func (d *PyroscopeDatasource) SubscribeStream(_ context.Context, req *backend.SubscribeStreamRequest) (*backend.SubscribeStreamResponse, error) {
	logger.Debug("Subscribing stream called", "function", logEntrypoint())

	status := backend.SubscribeStreamStatusPermissionDenied
	if req.Path == "stream" {
		// Allow subscribing only on expected path.
		status = backend.SubscribeStreamStatusOK
	}
	return &backend.SubscribeStreamResponse{
		Status: status,
	}, nil
}

// RunStream is called once for any open channel.  Results are shared with everyone
// subscribed to the same channel.
func (d *PyroscopeDatasource) RunStream(ctx context.Context, req *backend.RunStreamRequest, sender *backend.StreamSender) error {
	ctxLogger := logger.FromContext(ctx)
	ctxLogger.Debug("Running stream", "path", req.Path, "function", logEntrypoint())

	// Create the same data frame as for query data.
	frame := data.NewFrame("response")

	// Add fields (matching the same schema used in QueryData).
	frame.Fields = append(frame.Fields,
		data.NewField("time", nil, make([]time.Time, 1)),
		data.NewField("values", nil, make([]int64, 1)),
	)

	counter := 0

	// Stream data frames periodically till stream closed by Grafana.
	for {
		select {
		case <-ctx.Done():
			ctxLogger.Info("Context done, finish streaming", "path", req.Path, "function", logEntrypoint())
			return nil
		case <-time.After(time.Second):
			// Send new data periodically.
			frame.Fields[0].Set(0, time.Now())
			frame.Fields[1].Set(0, int64(10*(counter%2+1)))

			counter++

			err := sender.SendFrame(frame, data.IncludeAll)
			if err != nil {
				ctxLogger.Error("Error sending frame", "error", err, "function", logEntrypoint())
				continue
			}
		}
	}
}

// PublishStream is called when a client sends a message to the stream.
func (d *PyroscopeDatasource) PublishStream(ctx context.Context, _ *backend.PublishStreamRequest) (*backend.PublishStreamResponse, error) {
	logger.FromContext(ctx).Debug("Publishing stream", "function", logEntrypoint())

	// Do not allow publishing at all.
	return &backend.PublishStreamResponse{
		Status: backend.PublishStreamStatusPermissionDenied,
	}, nil
}
