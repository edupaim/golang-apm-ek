package main

import (
	"context"
	"fmt"
	"go.elastic.co/apm"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/jinzhu/gorm"
	"github.com/olivere/elastic/v7"
	log "github.com/sirupsen/logrus"
	"go.elastic.co/apm/module/apmgorilla"
	"go.elastic.co/apm/module/apmgorm"
	_ "go.elastic.co/apm/module/apmgorm/dialects/sqlite"
	"go.elastic.co/apm/module/apmlogrus"
	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/sohlich/elogrus.v7"
)

type Guest struct {
	gorm.Model
	Name string
}

var dbConn *gorm.DB

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
	log.SetLevel(log.DebugLevel)
}

func handler(w http.ResponseWriter, r *http.Request) {
	db := apmgorm.WithContext(r.Context(), dbConn)
	contextLog := log.WithFields(apmlogrus.TraceContext(r.Context()))
	name := getName(r)
	var guestPersisted Guest
	if !db.First(&guestPersisted, "name = ?", name).RecordNotFound() {
		db.Delete(&guestPersisted)
	}
	db.Create(&Guest{Name: name})
	_, err := w.Write([]byte(fmt.Sprintf("Hello, %s\n", name)))
	if err != nil {
		contextLog.Errorln(err.Error())
	}
}

func getName(r *http.Request) string {
	span, ctx := apm.StartSpan(r.Context(), "query.Get(\"name\")", "runtime.exec")
	contextLog := log.WithFields(apmlogrus.TraceContext(ctx))
	query := r.URL.Query()
	name := query.Get("name")
	if name == "" {
		name = "Guest"
	}
	contextLog.Debugln("received request for", name)
	span.End()
	return name
}

type NotFoundLogger struct{}

func (nfl *NotFoundLogger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	traceContextFields := apmlogrus.TraceContext(r.Context())
	log.WithFields(traceContextFields).Errorln("unknown route error")
	w.WriteHeader(http.StatusNotFound)
}

func main() {
	var err error
	dbConn, err = apmgorm.Open("sqlite3", "test.db")
	if err != nil {
		panic("failed to connect database")
	}
	defer dbConn.Close()

	// Migrate the schema
	dbConn.AutoMigrate(&Guest{})

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
	err := srv.Shutdown(ctx)
	if err != nil {
		log.Panicln(err.Error())
	}
	log.Println("Shutting down")
	os.Exit(0)
}
