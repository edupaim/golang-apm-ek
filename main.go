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
	initializeAndAddElasticHookToLogrus()
	initializeAndAddApmHookToLogrus()
	initializeSqliteConn()
}

func initializeSqliteConn() {
	var err error
	dbConn, err = apmgorm.Open("sqlite3", "test.db?mode=memory")
	if err != nil {
		panic("failed to connect database")
	}
	// Migrate the schema
	dbConn.AutoMigrate(&Guest{})
}

func initializeAndAddApmHookToLogrus() {
	// apmlogrus.Hook will send "error", "panic", and "fatal" level log messages to Elastic APM.
	apmHook := &apmlogrus.Hook{}
	apmHook.LogLevels = append(apmlogrus.DefaultLogLevels, log.WarnLevel)
	log.AddHook(apmHook)
	log.SetLevel(log.DebugLevel)
}

func initializeAndAddElasticHookToLogrus() {
	client, err := elastic.NewClient(elastic.SetURL("http://localhost:9200"))
	if err != nil {
		// this fatal ensures that the application will not work if the elasticsearch service is not available.
		log.Panic(err)
	}
	elasticHook, err := elogrus.NewAsyncElasticHook(client, "localhost", log.DebugLevel, "golang-")
	if err != nil {
		// this fatal ensures that the application will not work if the elasticsearch service is not available.
		log.Panic(err)
	}
	log.AddHook(elasticHook)
}

func routeHttpHandler(w http.ResponseWriter, r *http.Request) {
	reqCtx := r.Context()
	name := getName(r, reqCtx)
	sqliteIterate(name, reqCtx)
	responseRequest(w, name, reqCtx)
}

func responseRequest(w http.ResponseWriter, name string, reqCtx context.Context) {
	span, ctx := apm.StartSpan(reqCtx, "responseRequest()", "runtime.responseHttp")
	defer span.End()
	contextLog := log.WithFields(apmlogrus.TraceContext(ctx))
	_, err := w.Write([]byte(fmt.Sprintf("hello, %s\n", name)))
	if err != nil {
		contextLog.Errorln(err.Error())
	}
}

func sqliteIterate(name string, ctx context.Context) {
	span, ctx := apm.StartSpan(ctx, "sqliteIterate()", "runtime.sqliteIterate")
	defer span.End()
	var guestPersisted Guest
	db := apmgorm.WithContext(ctx, dbConn)
	if !db.First(&guestPersisted, "name = ?", name).RecordNotFound() {
		db.Delete(&guestPersisted)
	}
	db.Create(&Guest{Name: name})
}

func getName(r *http.Request, reqCtx context.Context) string {
	span, ctx := apm.StartSpan(reqCtx, "getName()", "runtime.getQueryUrl")
	defer span.End()
	contextLog := log.WithFields(apmlogrus.TraceContext(ctx))
	query := r.URL.Query()
	name := query.Get("name")
	if name == "" {
		contextLog.Warningln("unknown guest")
		name = "guest"
	} else {
		contextLog.Debugln("received request for", name)
	}
	return name
}

type NotFoundLogger struct{}

func (nfl *NotFoundLogger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	traceContextFields := apmlogrus.TraceContext(r.Context())
	log.WithFields(traceContextFields).Errorln("unknown route error")
	w.WriteHeader(http.StatusNotFound)
}

func main() {
	// initialize router and route handlers
	r := mux.NewRouter()
	r.NotFoundHandler = &NotFoundLogger{}
	apmgorilla.Instrument(r)
	r.HandleFunc("/", routeHttpHandler)
	r.HandleFunc("/hi", routeHttpHandler)
	srv := &http.Server{
		Handler:      r,
		Addr:         ":8080",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// set env LOG_FILE_LOCATION to log file
	LogFileLocationEnv := os.Getenv("LOG_FILE_LOCATION")
	if LogFileLocationEnv != "" {
		log.SetOutput(&lumberjack.Logger{
			Filename:   LogFileLocationEnv,
			MaxSize:    5, // megabytes
			MaxBackups: 1,
			MaxAge:     1,    //days
			Compress:   true, // disabled by default
		})
	}

	// start server
	go func() {
		log.Println("starting server...")
		if err := srv.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()

	// graceful shutdown
	waitForShutdown(srv)
}

func waitForShutdown(srv *http.Server) {
	interruptChan := make(chan os.Signal, 1)
	signal.Notify(interruptChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	// block until we receive our signal.
	<-interruptChan

	// create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	err := srv.Shutdown(ctx)
	if err != nil {
		log.Panicln(err.Error())
	}
	if dbConn != nil {
		_ = dbConn.Close()
	}
	log.Println("Shutting down")
	os.Exit(0)
}
