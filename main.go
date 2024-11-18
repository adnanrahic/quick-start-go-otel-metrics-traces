package main

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"runtime"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var (
	serviceName      string = "test-service"
	collectorURL     string = "localhost:4317"
	meter            metric.Meter
	errorCounter     metric.Int64Counter
	latencyHistogram metric.Float64Histogram
	itemGauge        metric.Int64Gauge
	cartCount        int64 = 0
	tracer           trace.Tracer
)

// Initialize a gRPC connection to be used by both the tracer and meter providers.
func initGrpcConn() (*grpc.ClientConn, error) {
	// It connects the OpenTelemetry Collector through local gRPC connection.
	conn, err := grpc.NewClient(
		collectorURL,
		// Note the use of insecure transport here. TLS is recommended in production.
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to collector: %w", err)
	}

	return conn, err
}

// Initializes an OTLP exporter, and configures the corresponding meter provider.
func initMeterProvider(ctx context.Context, res *resource.Resource, conn *grpc.ClientConn) (func(context.Context) error, error) {
	metricExporter, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics exporter: %w", err)
	}

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter,
			// Default is 1m. Set to 3s for demonstrative purposes.
			sdkmetric.WithInterval(3*time.Second))),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(meterProvider)

	return meterProvider.Shutdown, nil
}

func initTraceProvider(ctx context.Context, res *resource.Resource, conn *grpc.ClientConn) (func(context.Context) error, error) {
	traceExporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		log.Fatalf("Failed to create exporter: %v", err)
	}

	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(traceProvider)

	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return traceProvider.Shutdown, nil
}

func collectMachineResourceMetrics(meter metric.Meter) {
	period := 5 * time.Second
	ticker := time.NewTicker(period)

	var Mb uint64 = 1_048_576 // number of bytes in a MB

	for {
		select {
		case <-ticker.C:
			// This will be executed every "period" of time passes
			meter.Float64ObservableGauge(
				"process.allocated_memory",
				metric.WithDescription("Allocated memory in MB."),
				metric.WithUnit("{MB}"),
				metric.WithFloat64Callback(
					func(ctx context.Context, fo metric.Float64Observer) error {
						var memStats runtime.MemStats
						runtime.ReadMemStats(&memStats)

						allocatedMemoryInMB := float64(memStats.Alloc) / float64(Mb)
						fo.Observe(allocatedMemoryInMB)

						return nil
					},
				),
			)
		}
	}
}

func main() {
	ctx := context.Background()

	conn, err := initGrpcConn()
	if err != nil {
		log.Fatal(err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			// The service name used to display traces in backends
			attribute.String("service.name", serviceName),
			attribute.String("library.language", "go"),
		),
	)
	if err != nil {
		log.Fatal(err)
	}

	shutdownTraceProvider, err := initTraceProvider(ctx, res, conn)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := shutdownTraceProvider(ctx); err != nil {
			log.Fatalf("failed to shutdown Tracer: %s", err)
		}
	}()

	shutdownMeterProvider, err := initMeterProvider(ctx, res, conn)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := shutdownMeterProvider(ctx); err != nil {
			log.Fatalf("failed to shutdown MeterProvider: %s", err)
		}
	}()

	// Create a Tracer
	tracer = otel.Tracer(serviceName)

	// Create a Meter
	meter = otel.Meter(serviceName)

	// Initialize metrics
	// Count
	errorCounter, err = meter.Int64Counter(
		"api.request.error_counter",
		metric.WithDescription("Number of erroneous API calls."),
		metric.WithUnit("{call}"),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Histogram
	latencyHistogram, err = meter.Float64Histogram(
		"api.request.latency_seconds",
		metric.WithDescription("Records the latency of requests in seconds"),
		metric.WithUnit("{s}"),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Gauge
	// Memory
	go collectMachineResourceMetrics(meter)
	// Cart items
	itemGauge, err = meter.Int64Gauge(
		"api.cart.items",
		metric.WithDescription("Tracks the number of items in a user's cart"),
		metric.WithUnit("{item}"),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Start HTTP server
	http.HandleFunc("/", helloWorldHandler)
	http.HandleFunc("/cart/add", cartAddHandler)
	http.HandleFunc("/cart/remove", cartRemoveHandler)
	fmt.Println("Starting server on localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}

// recordLatencyHistogram records the request latency
func recordLatencyHistogram(start time.Time) {
	latency := time.Since(start).Seconds()
	latencyHistogram.Record(context.Background(), latency)
}

// helloWorldHandler handles the API request and returns "Hello, World!"
func helloWorldHandler(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.Start(r.Context(), "helloWorldHandler")
	defer span.End()

	start := time.Now()
	defer recordLatencyHistogram(start)

	// Simulate a potential error
	if rand.Float64() < 0.5 { // 50% chance of an error
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		errorCounter.Add(r.Context(), 1)

		// HTTP request failed
		span.SetAttributes(
			attribute.Bool("helloWorldHandler.error", true),
			attribute.Int64("http.status", http.StatusInternalServerError),
		)

		return
	}

	// HTTP request successful
	span.SetAttributes(
		attribute.Bool("helloWorldHandler.error", false),
		attribute.Int64("http.status", http.StatusOK),
	)

	// Respond with "Hello, World!"
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Hello, World!"))
}

func cartAddHandler(w http.ResponseWriter, r *http.Request) {
	cartCount = cartCount + 1
	itemGauge.Record(r.Context(), cartCount)

	_, span := tracer.Start(r.Context(), "cartAddHandler")
	defer span.End()
	// Add the current cartCount as an attribute
	span.SetAttributes(
		attribute.Int64("cartAddHandler.cartCount", cartCount),
	)

	message := fmt.Sprintf("Item added to cart. Number of items in cart: %d.", cartCount)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(message))
}

func cartRemoveHandler(w http.ResponseWriter, r *http.Request) {
	if cartCount != 0 {
		cartCount = cartCount - 1
	}
	itemGauge.Record(r.Context(), cartCount)

	_, span := tracer.Start(r.Context(), "cartRemoveHandler")
	defer span.End()
	// Add the current cartCount as an attribute
	span.SetAttributes(
		attribute.Int64("cartRemoveHandler.cartCount", cartCount),
	)

	message := fmt.Sprintf("Item removed from cart. Number of items in cart: %d.", cartCount)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(message))
}
