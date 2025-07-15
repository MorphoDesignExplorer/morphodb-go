package main

import (
	"encoding/json"
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
func setupDB(service morphoroutes.Service) error {
	db, err := service.GetDB()
	if err != nil {
		return morphoroutes.NewServerError(err)
	}

	queries := []string{
		"CREATE TABLE IF NOT EXISTS project (creation_date date not null, project_name text primary key, variable_metadata jsonb not null, output_metadata jsonb not null, assets jsonb, deleted integer not null)",
		"CREATE TABLE IF NOT EXISTS document (id text primary key, slug text NOT NULL, text text NOT NULL, title text NOT NULL, parent text, timestamp date)",
		"CREATE TABLE IF NOT EXISTS solution (id text primary key, parameters jsonb not null, output_parameters jsonb, project_name text not null, scoped_id integer, foreign key(project_name) references project(project_name));",
		"CREATE INDEX IF NOT EXISTS solution_to_project on solution(project_name);",
		"CREATE TABLE IF NOT EXISTS asset(id text, file text, tag text, solution_id integer, foreign key(solution_id) references solution(id), PRIMARY KEY (solution_id, tag));",
		"CREATE INDEX IF NOT EXISTS asset_id on asset(id)",
		"CREATE INDEX IF NOT EXISTS reverse_solution on asset(solution_id);",
		"CREATE TABLE IF NOT EXISTS metadata (project_name text primary key, captions jsonb, human_name text, slug text, markdown text, foreign key(project_name) references project(project_name));",
		"INSERT OR IGNORE INTO document (id, slug, text, title, parent, timestamp) VALUES ('0f6edea0-498d-430c-bf25-6a7b93d30a9c', 'Front Matter', '', 'Front Matter', '', time('now'));",
	}

	for _, query := range queries {
		_, err = db.Exec(query)
		if err != nil {
			return morphoroutes.NewServerError(err)
		}
	}

	err = morphoroutes.InitAuthDB(db)
	if err != nil {
		return morphoroutes.NewServerError(err)
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
			json.NewEncoder(w).Encode(morphoroutes.APIMessage{Message: fmt.Sprintf("Method %s not allowed.", r.Method)})
		})
	}
}

func multiplexRoute(routes map[string]http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		routes[r.Method](w, r)
	}
}

func SetupRouter() *mux.Router {
	morphoroutes.GlobalCache = &morphoroutes.Cacher{}
	morphoroutes.GlobalCache.InitCache()

	// Get middleware into local scope for easier usage
	AuthMiddleware := morphoroutes.AuthenticatedMiddleware

	service, err := morphoroutes.StartService()
	if err != nil {
		panic(err)
	}

	topRouter := mux.NewRouter()
	topRouter.Use(RouteLoggerMiddleware)

	dataRouter := topRouter.PathPrefix("/project").Subrouter()
	dataRouter.Use(morphoroutes.CacheMiddleware)
	dataRouter.HandleFunc("/", multiplexRoute(
		map[string]http.HandlerFunc{
			"GET":  service.GetProjectEndpoint().Finalize(),
			"POST": service.PostProjectZip(), // TODO add authentication middleware
			"PUT":  service.UpdateProjectMetadata().AddMiddleware(AuthMiddleware(morphoroutes.CAN_UPDATE)).Finalize(),
		},
	)).Methods("GET", "POST", "PUT")

	dataRouter.HandleFunc("/{project}/", service.GetProjectEndpoint().Finalize()).Methods("GET", "POST")
	dataRouter.HandleFunc("/{project}/model/", service.GetSolutionEndpoint().Finalize()).Methods("GET")
	dataRouter.HandleFunc("/{project}/model/{solution}/", multiplexRoute(
		map[string]http.HandlerFunc{
			"GET":  service.GetSolutionEndpoint().Finalize(),
			"POST": service.PostAsset().AddMiddleware(AuthMiddleware(morphoroutes.CAN_CREATE | morphoroutes.CAN_UPDATE)).Finalize(),
		},
	)).Methods("GET", "POST")

	documentRouter := topRouter.PathPrefix("/document").Subrouter()
	documentRouter.HandleFunc("/", multiplexRoute(
		map[string]http.HandlerFunc{
			"GET":  service.GetDocumentEndpoint().Finalize(),
			"POST": service.PostDocument().AddMiddleware(AuthMiddleware(morphoroutes.CAN_CREATE | morphoroutes.CAN_UPDATE)).Finalize(),
		},
	)).Methods("GET", "POST")
	documentRouter.HandleFunc("/{idOrSlug}/", multiplexRoute(
		map[string]http.HandlerFunc{
			"GET":    service.GetDocumentEndpoint().Finalize(),
			"PUT":    service.PutDocument().AddMiddleware(AuthMiddleware(morphoroutes.CAN_CREATE | morphoroutes.CAN_UPDATE)).Finalize(),
			"DELETE": service.DeleteDocument().AddMiddleware(AuthMiddleware(morphoroutes.CAN_CREATE | morphoroutes.CAN_UPDATE)).Finalize(),
		},
	)).Methods("GET", "PUT", "DELETE")

	authRouter := topRouter.PathPrefix("/auth").Subrouter()
	authRouter.Use(FilterMethodsMiddleware([]string{"POST"}))
	authRouter.HandleFunc("/login/", service.LoginEndpoint().Finalize())
	authRouter.HandleFunc("/reset/", service.ResetPasswordEndpoint().Finalize())

	return topRouter
}

func main() {

	service, err := morphoroutes.StartService()
	if err != nil {
		log.Print(err)
		return
	}

	err = setupDB(service)
	if err != nil {
		log.Print(err)
		return
	}

	topRouter := SetupRouter()
	port := 8000
	log.Println("listening on port", port)
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(port), topRouter))
}
