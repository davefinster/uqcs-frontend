package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"net/http"

	"contrib.go.opencensus.io/exporter/stackdriver"
	pb "github.com/davefinster/uqcs-demo/frontend/api"
	"github.com/gin-gonic/gin"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"go.opencensus.io/trace"
	"google.golang.org/grpc"
)

type restCreateEvent struct {
	Title       string `json:"title" binding:"required"`
	Description string `json:"description"`
}

type restEvent struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description *string `json:"description,omitempty"`
}

func eventProtoToJSON(proto *pb.Event) *restEvent {
	jsonEvent := &restEvent{
		ID:    proto.GetId(),
		Title: proto.GetTitle(),
	}
	if len(proto.GetDescription()) > 0 {
		str := proto.GetDescription()
		jsonEvent.Description = &str
	}
	return jsonEvent
}

func sendStats(ctx context.Context, span *trace.Span, statusCode int, errText string, tStart time.Time, method, path string) {
	span.SetStatus(ochttp.TraceStatus(statusCode, errText))
	span.AddAttributes(trace.Int64Attribute(ochttp.StatusCodeAttribute, int64(statusCode)))
	m := []stats.Measurement{
		ochttp.ServerLatency.M(float64(time.Since(tStart)) / float64(time.Millisecond)),
	}
	tags := make([]tag.Mutator, 3)
	tags[0] = tag.Upsert(ochttp.StatusCode, strconv.Itoa(statusCode))
	tags[1] = tag.Upsert(ochttp.Method, method)
	tags[2] = tag.Upsert(ochttp.Path, path)
	stats.RecordWithTags(ctx, tags, m...)
}

func main() {
	project := os.Getenv("GCP_PROJECT_ID")

	exporter, err := stackdriver.NewExporter(stackdriver.Options{
		ProjectID:         project,
		MetricPrefix:      "uqcs",
		ReportingInterval: 10 * time.Second,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer exporter.Flush()
	if err := view.Register(ocgrpc.DefaultClientViews...); err != nil {
		log.Fatalf("Failed to register ocgrpc client views: %v", err)
	}
	trace.RegisterExporter(exporter)
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})
	// In our main, register ochttp Server views
	if err := view.Register(ochttp.DefaultServerViews...); err != nil {
		log.Fatalf("Failed to register server views for HTTP metrics: %v", err)
	}
	if err := exporter.StartMetricsExporter(); err != nil {
		log.Fatalf("Error starting metric exporter: %v", err)
	}
	defer exporter.StopMetricsExporter()

	serverAddr := "uqcs-grpc:10000"
	cc, err := grpc.Dial(serverAddr, grpc.WithInsecure(), grpc.WithStatsHandler(new(ocgrpc.ClientHandler)))
	if err != nil {
		log.Fatalf("fetchIt gRPC client failed to dial to server: %v", err)
	}
	fc := pb.NewEventBackendClient(cc)

	r := gin.Default()

	r.GET("/events", func(c *gin.Context) {
		tStart := time.Now()
		ctx, span := trace.StartSpan(c, "uqcs.frontend.http.GetEvents")

		defer span.End()
		resp, err := fc.GetEvents(ctx, &pb.GetEventsRequest{})
		if err != nil {
			errText := fmt.Sprintf("got error from backend: %s", err)
			sendStats(c, span, http.StatusInternalServerError, errText, tStart, "GET", "/events")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": errText,
			})
			return
		}
		jsonEvents := make([]*restEvent, len(resp.GetEvents()))
		for i, event := range resp.GetEvents() {
			jsonEvents[i] = eventProtoToJSON(event)
		}
		sendStats(c, span, http.StatusOK, "", tStart, "GET", "/events")
		c.JSON(200, jsonEvents)
	})

	r.POST("/events", func(c *gin.Context) {
		tStart := time.Now()
		ctx, span := trace.StartSpan(c, "uqcs.frontend.http.CreateEvent")
		defer span.End()
		var json restCreateEvent
		if err := c.ShouldBindJSON(&json); err != nil {
			sendStats(c, span, http.StatusBadRequest, err.Error(), tStart, "POST", "/events")
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		resp, err := fc.CreateEvent(ctx, &pb.CreateEventRequest{
			Event: &pb.Event{
				Title:       json.Title,
				Description: json.Description,
			},
		})
		if err != nil {
			errText := fmt.Sprintf("got error from backend: %s", err)
			sendStats(c, span, http.StatusBadRequest, errText, tStart, "POST", "/events")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": errText,
			})
			return
		}
		sendStats(c, span, http.StatusOK, "", tStart, "POST", "/events")
		c.JSON(200, eventProtoToJSON(resp.GetEvent()))
	})

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "okforsure",
		})
	})

	if os.Getenv("LOADER_VERIFY") != "" {
		r.GET(fmt.Sprintf("/%s.txt", os.Getenv("LOADER_VERIFY")), func(c *gin.Context) {
			c.String(200, os.Getenv("LOADER_VERIFY"))
		})
	}

	r.Run()
}
