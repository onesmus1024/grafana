package influxql

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"

	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/setting"
	"github.com/grafana/grafana/pkg/tsdb/influxdb/models"
)

const defaultRetentionPolicy = "default"

var (
	ErrInvalidHttpMode = errors.New("'httpMode' should be either 'GET' or 'POST'")
	glog               = log.New("tsdb.influx_influxql")
)

func Query(ctx context.Context, dsInfo *models.DatasourceInfo, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	logger := glog.FromContext(ctx)
	// Override the context for the logger temporarily

	response := backend.NewQueryDataResponse()

	for _, reqQuery := range req.Queries {
		query, err := models.QueryParse(reqQuery)
		if err != nil {
			return &backend.QueryDataResponse{}, err
		}

		rawQuery, err := query.Build(req)
		if err != nil {
			return &backend.QueryDataResponse{}, err
		}

		query.RefID = reqQuery.RefID
		query.RawQuery = rawQuery

		if setting.Env == setting.Dev {
			logger.Info("Influxdb query", "raw query", rawQuery)
		}

		request, err := createRequest(ctx, logger, dsInfo, rawQuery, query.Policy)
		if err != nil {
			return &backend.QueryDataResponse{}, err
		}

		resp, err := execute(dsInfo, logger, query, request)

		if err != nil {
			response.Responses[query.RefID] = backend.DataResponse{Error: err}
		} else {
			response.Responses[query.RefID] = resp
		}
	}

	return response, nil
}

func createNewExemplarQuery(rawQuery string) (string, error) {
	fromIndex := strings.Index(rawQuery, "FROM")
	if fromIndex == -1 {
		return "", errors.New("keyword 'FROM' not found in query")
	}

	prefix := "SELECT * FROM "
	suffix := rawQuery[fromIndex+len("FROM")+1:]

	endOfTableName := strings.Index(suffix, " ")
	if endOfTableName == -1 {
		return "", errors.New("space not found after table name in query")
	}

	tableName := suffix[:endOfTableName]
	modifiedTableName := strings.TrimSuffix(tableName, "\"") + "_exemplar\""
	remainder := suffix[endOfTableName:]

	return prefix + modifiedTableName + remainder, nil
}

// QueryExemplarData function returns a slice of models.Exemplar
func QueryExemplarData(ctx context.Context, dsInfo *models.DatasourceInfo, req *backend.QueryDataRequest) ([]models.Exemplar, error) {
	logger := glog.FromContext(ctx)
	var exemplars []models.Exemplar // Declare a slice of models.Exemplar

	for _, reqQuery := range req.Queries {
		query, err := models.QueryParse(reqQuery)
		if err != nil {
			return nil, err
		}

		rawQuery, err := query.Build(req)
		if err != nil {
			return nil, err
		}

		modifiedQuery, err := createNewExemplarQuery(rawQuery)
		if err != nil {
			return nil, err
		}

		logger.Info("Influxdb exemplar query", "raw exemplar query", modifiedQuery)

		query.RefID = reqQuery.RefID
		query.RawQuery = modifiedQuery

		if setting.Env == setting.Dev {
			logger.Debug("Influxdb query", "raw query", rawQuery)
		}

		request, err := createRequest(ctx, logger, dsInfo, rawQuery, query.Policy)
		if err != nil {
			return nil, err
		}

		resp, err := execute(dsInfo, logger, query, request)
		if err != nil {
			return nil, err
		}

		// Transform the frames to exemplars and append them to the exemplars slice
		exemplars = append(exemplars, transformToExemplars(resp.Frames)...)

	}
	logger.Info("exemplars", "exemplars", exemplars)
	return exemplars, nil
}

func createRequest(ctx context.Context, logger log.Logger, dsInfo *models.DatasourceInfo, queryStr string, retentionPolicy string) (*http.Request, error) {
	u, err := url.Parse(dsInfo.URL)
	if err != nil {
		return nil, err
	}

	u.Path = path.Join(u.Path, "query")
	httpMode := dsInfo.HTTPMode

	var req *http.Request
	switch httpMode {
	case "GET":
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
	case "POST":
		bodyValues := url.Values{}
		bodyValues.Add("q", queryStr)
		body := bodyValues.Encode()
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(body))
		if err != nil {
			return nil, err
		}
	default:
		return nil, ErrInvalidHttpMode
	}

	params := req.URL.Query()
	params.Set("db", dsInfo.DbName)
	params.Set("epoch", "ms")
	// default is hardcoded default retention policy
	// InfluxDB will use the default policy when it is not added to the request
	if retentionPolicy != "" && retentionPolicy != "default" {
		params.Set("rp", retentionPolicy)
	}

	if httpMode == "GET" {
		params.Set("q", queryStr)
	} else if httpMode == "POST" {
		req.Header.Set("Content-type", "application/x-www-form-urlencoded")
	}

	req.URL.RawQuery = params.Encode()

	logger.Debug("Influxdb request", "url", req.URL.String())
	return req, nil
}

func execute(dsInfo *models.DatasourceInfo, logger log.Logger, query *models.Query, request *http.Request) (backend.DataResponse, error) {
	res, err := dsInfo.HTTPClient.Do(request)
	if err != nil {
		return backend.DataResponse{}, err
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			logger.Warn("Failed to close response body", "err", err)
		}
	}()
	resp := ResponseParse(res.Body, res.StatusCode, query)
	return *resp, nil
}
