package main

import (
	"encoding/json"
	"fmt"
	"log"
	morphoroutes "morphodb-go/morpho-routes"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

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

func main() {
	morphoroutes.GlobalCache = &morphoroutes.Cacher{}
	morphoroutes.GlobalCache.InitCache()

	config, err := morphoroutes.GetConfig()
	if err != nil {
		panic(err)
	}

	topRouter := mux.NewRouter()
	topRouter.Use(RouteLoggerMiddleware)

	getRouter := topRouter.PathPrefix("/project").Subrouter()
	getRouter.Use(FilterMethodsMiddleware([]string{"GET"}))
	getRouter.Use(morphoroutes.CacheMiddleware)
	getRouter.HandleFunc("/", morphoroutes.GetProjectsWrapper(config))
	getRouter.HandleFunc("/{project}/", morphoroutes.GetProjectsWrapper(config))
	getRouter.HandleFunc("/{project}/model/", morphoroutes.GetSolutionsWrapper(config))
	getRouter.HandleFunc("/{project}/model/{solution}/", morphoroutes.GetSolutionsWrapper(config))

	authRouter := topRouter.PathPrefix("/auth").Subrouter()
	authRouter.Use(FilterMethodsMiddleware([]string{"POST"}))
	authRouter.HandleFunc("/init/", morphoroutes.InitLogin)
	authRouter.HandleFunc("/verify/", morphoroutes.VerifyLogin)

	port := 8000
	log.Println("listening on port", port)
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(port), topRouter))
}
