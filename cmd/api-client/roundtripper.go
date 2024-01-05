package main

import (
	"net/http"
	"time"

	"github.com/ogen-go/ogen/otelogen"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"

	"example/internal/httpmiddleware"
)

type MeteredRoundTripper struct {
	roundTripper http.RoundTripper
	find         httpmiddleware.RouteFinder
	requests     metric.Int64Counter
	errors       metric.Int64Counter
	duration     metric.Float64Histogram
}

func NewMeteredRoundTripper(
	roundTripper http.RoundTripper,
	meterProvider metric.MeterProvider,
	find httpmiddleware.RouteFinder,
) (MeteredRoundTripper, error) {

	mrt := MeteredRoundTripper{roundTripper: roundTripper, find: find}
	meter := meterProvider.Meter("custom_" + otelogen.Name)

	var err error
	if mrt.requests, err = meter.Int64Counter("custom_" + otelogen.ClientRequestCount); err != nil {
		return mrt, err
	}
	if mrt.errors, err = meter.Int64Counter("custom_" + otelogen.ClientErrorsCount); err != nil {
		return mrt, err
	}
	if mrt.duration, err = meter.Float64Histogram("custom_" + otelogen.ClientDuration); err != nil {
		return mrt, err
	}
	return mrt, err
}

func (mrt MeteredRoundTripper) RoundTrip(req *http.Request) (res *http.Response, err error) {
	otelAttrs := []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(req.Method),
		semconv.ClientAddress(req.URL.Hostname()),
		semconv.ClientPortKey.String(req.URL.Port()),
	}

	op, ok := mrt.find(req.Method, req.URL)
	if ok {
		otelAttrs = append(otelAttrs,
			otelogen.OperationID(op.OperationID()),
			semconv.HTTPRouteKey.String(op.PathPattern()),
		)
	}

	// Run stopwatch.
	startTime := time.Now()
	defer func() {
		statusCodeAttr := semconv.HTTPStatusCode(res.StatusCode)

		// Use floating point division here for higher precision (instead of Millisecond method).
		elapsedDuration := time.Since(startTime)
		mrt.duration.Record(req.Context(), float64(float64(elapsedDuration)/float64(time.Millisecond)), metric.WithAttributes(otelAttrs...), metric.WithAttributes(statusCodeAttr))
	}()

	// Increment request counter.
	mrt.requests.Add(req.Context(), 1, metric.WithAttributes(otelAttrs...))

	// Track stage for error reporting.
	defer func() {
		if err != nil {
			mrt.errors.Add(req.Context(), 1, metric.WithAttributes(otelAttrs...))
		}
	}()

	// Send the request, get the response (or the error)
	res, err = mrt.roundTripper.RoundTrip(req)
	return
}
