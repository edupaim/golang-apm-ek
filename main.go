package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/olivere/elastic/v7"
	log "github.com/sirupsen/logrus"
	"go.elastic.co/apm/module/apmgorilla"
	"go.elastic.co/apm/module/apmlogrus"
	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/sohlich/elogrus.v7"
)

func init() {
	client, err := elastic.NewClient(elastic.SetURL("http://localhost:9200"))
	if err != nil {
		log.Panic(err)
	}
	hook, err := elogrus.NewAsyncElasticHook(client, "localhost", log.DebugLevel, "golang-")
	if err != nil {
		log.Panic(err)
	}
	// apmlogrus.Hook will send "error", "panic", and "fatal" level log messages to Elastic APM.
	log.AddHook(&apmlogrus.Hook{})
	log.AddHook(hook)
}

func handler(w http.ResponseWriter, r *http.Request) {
	traceContextFields := apmlogrus.TraceContext(r.Context())
	contextLog := log.WithFields(traceContextFields)
	query := r.URL.Query()
	name := query.Get("name")
	if name == "" {
		name = "Guest"
	}
	contextLog.Printf("Received request for %s\n", name)
	_, err := w.Write([]byte(fmt.Sprintf("Hello, %s\n", name)))
	if err != nil {
		contextLog.Errorln(err.Error())
	}
}

type NotFoundLogger struct{}

func (nfl *NotFoundLogger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	traceContextFields := apmlogrus.TraceContext(r.Context())
	log.WithFields(traceContextFields).Errorln("Not Found!")
	w.WriteHeader(http.StatusNotFound)
}

func main() {
	// Create Server and Route Handlers
	r := mux.NewRouter()

	r.NotFoundHandler = &NotFoundLogger{}

	apmgorilla.Instrument(r)

	r.HandleFunc("/", handler)

	r.HandleFunc("/hi", handler)

	srv := &http.Server{
		Handler:      r,
		Addr:         ":8080",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Configure Logging
	LOG_FILE_LOCATION := os.Getenv("LOG_FILE_LOCATION")
	if LOG_FILE_LOCATION != "" {
		log.SetOutput(&lumberjack.Logger{
			Filename:   LOG_FILE_LOCATION,
			MaxSize:    500, // megabytes
			MaxBackups: 3,
			MaxAge:     28,   //days
			Compress:   true, // disabled by default
		})
	}

	// Start Server
	go func() {
		log.Println("Starting Server")
		if err := srv.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()

	// Graceful Shutdown
	waitForShutdown(srv)
}

func waitForShutdown(srv *http.Server) {
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// Block until we receive our signal.
	<-interruptChan

	// Create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	srv.Shutdown(ctx)

	log.Println("Shutting down")
	os.Exit(0)
}
