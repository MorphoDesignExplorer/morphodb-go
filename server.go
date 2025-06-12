package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	morphoroutes "github.com/MorphoDesignExplorer/morphodb-go/morpho-routes"

	"github.com/gorilla/mux"
)

// Initialize the DB in the case that it doesn't exist.
func setupDB() error {
	config, err := morphoroutes.GetConfig()
	if err != nil {
		panic(err)
	}

	db, err := morphoroutes.StartConn(config)
	if err != nil {
		morphoroutes.LogError(err)
	}

	queries := []string{
		"CREATE TABLE IF NOT EXISTS project (creation_date date not null, project_name text primary key, variable_metadata jsonb not null, output_metadata jsonb not null, assets jsonb, deleted integer not null)",
		"CREATE TABLE IF NOT EXISTS document (id text primary key, slug text NOT NULL, text text NOT NULL)",
		"CREATE TABLE IF NOT EXISTS solution (id text primary key, parameters jsonb not null, output_parameters jsonb, project_name text not null, scoped_id integer, foreign key(project_name) references project(project_name));",
		"CREATE INDEX IF NOT EXISTS solution_to_project on solution(project_name);",
		"CREATE TABLE IF NOT EXISTS asset(id text, file text, tag text, solution_id integer, foreign key(solution_id) references solution(id), PRIMARY KEY (solution_id, tag));",
		"CREATE INDEX IF NOT EXISTS asset_id on asset(id)",
		"CREATE INDEX IF NOT EXISTS reverse_solution on asset(solution_id);",
		"CREATE TABLE IF NOT EXISTS metadata (project_name text primary key, captions jsonb, human_name text, slug text, markdown text, foreign key(project_name) references project(project_name));",
	}

	for _, query := range queries {
		_, err = db.Exec(query)
		if err != nil {
			morphoroutes.LogError(errors.New(query + err.Error()))
			return err
		}
	}

	err = morphoroutes.InitAuthDB(db)
	if err != nil {
		return err
	}

	return nil
}

// Middleware to log every request to stdout.
func RouteLoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s; response time: %dms\n", r.Method, r.URL.Path, time.Since(start).Milliseconds())
	})
}

/*
Middleware to allow only certain methods on a router. Best used within a subrouter.

methodsAllowed: list of methods allowed on the router
*/
func FilterMethodsMiddleware(methodsAllowed []string) func(http.Handler) http.Handler {
	methodMap := make(map[string]bool)
	for _, method := range methodsAllowed {
		methodMap[method] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := methodMap[r.Method]; ok {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Add("Content-Type", "application/json")
			w.Header().Add("Allow", strings.Join(methodsAllowed, ", "))
			w.WriteHeader(http.StatusMethodNotAllowed)
			json.NewEncoder(w).Encode(morphoroutes.ErrorMessage{Message: fmt.Sprintf("Method %s not allowed.", r.Method)})
		})
	}
}

func multiplexRoute(routes map[string]morphoroutes.HandlerFunc) morphoroutes.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		routes[r.Method](w, r)
	}
}

func SetupRouter() *mux.Router {
	morphoroutes.GlobalCache = &morphoroutes.Cacher{}
	morphoroutes.GlobalCache.InitCache()

	config, err := morphoroutes.GetConfig()
	if err != nil {
		panic(err)
	}

	topRouter := mux.NewRouter()
	topRouter.Use(RouteLoggerMiddleware)

	dataRouter := topRouter.PathPrefix("/project").Subrouter()
	dataRouter.Use(morphoroutes.CacheMiddleware)
	dataRouter.HandleFunc("/", multiplexRoute(
		map[string]morphoroutes.HandlerFunc{
			"GET":  morphoroutes.GetProjectsWrapper(config),
			"POST": morphoroutes.AuthenticatedMiddleware(morphoroutes.PostProject(config), morphoroutes.CAN_CREATE),
			"PUT":  morphoroutes.AuthenticatedMiddleware(morphoroutes.UpdateProjectMetadata(config), morphoroutes.CAN_UPDATE),
		},
	)).Methods("GET", "POST", "PUT")

	dataRouter.HandleFunc("/{project}/", morphoroutes.GetProjectsWrapper(config)).Methods("GET")
	dataRouter.HandleFunc("/{project}/model/", morphoroutes.GetSolutionsWrapper(config)).Methods("GET")
	dataRouter.HandleFunc("/{project}/model/{solution}/", multiplexRoute(
		map[string]morphoroutes.HandlerFunc{
			"GET":  morphoroutes.GetSolutionsWrapper(config),
			"POST": morphoroutes.AuthenticatedMiddleware(morphoroutes.PostAsset(config), morphoroutes.CAN_CREATE|morphoroutes.CAN_UPDATE),
		},
	)).Methods("GET", "POST")

	documentRouter := topRouter.PathPrefix("/document").Subrouter()
	documentRouter.HandleFunc("/", morphoroutes.GetDocumentWrapper(config)).Methods("GET")
	documentRouter.HandleFunc("/{idOrSlug}/", multiplexRoute(
		map[string]morphoroutes.HandlerFunc{
			"GET": morphoroutes.GetDocumentWrapper(config),
			"PUT": morphoroutes.PutDocument(config),
		},
	)).Methods("GET", "PUT")

	authRouter := topRouter.PathPrefix("/auth").Subrouter()
	authRouter.Use(FilterMethodsMiddleware([]string{"POST"}))
	authRouter.HandleFunc("/login/", morphoroutes.LoginHandler(config))
	authRouter.HandleFunc("/reset/", morphoroutes.ResetPasswordHandler(config))

	return topRouter
}

func main() {
	err := setupDB()
	if err != nil {
		panic(err)
	}

	topRouter := SetupRouter()
	port := 8000
	log.Println("listening on port", port)
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(port), topRouter))
}
