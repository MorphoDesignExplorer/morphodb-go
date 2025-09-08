package morphoroutes

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime"
)

type APIMessage struct {
	Message string `json:"message"`
}

// Writes an error with a custom error code.
//
// The calling route should return after invoking this function.
//
// Parameters:
//
// writer: a handler to a ResponseWriter
//
// err: An error to be communicated with the user.
func HandleAPIError(writer http.ResponseWriter, request *http.Request, err APIError) {
	// write error message to console
	log.Printf("%s: Error Trace (%s) %s", request.URL.Path, err.Message, err.serverError.Error())

	// return the API error message message
	writer.Header().Add("Content-Type", "application/json")
	writer.WriteHeader(err.StatusCode)
	json.NewEncoder(writer).Encode(err)
}

// Writes a 500 to the output stream.
//
// The calling route should return after invoking this function.
//
// Parameters:
//
// writer: a handler to a Response Writer
func HandleError(writer http.ResponseWriter) {
	writer.Header().Add("Content-Type", "application/json")
	writer.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(writer).Encode(APIMessage{"Internal Server Error"})
}

// Error that contains metadata about the position at which the error occurred.
type ServerError struct {
	functionName string // name of the function in which the error was constructed
	location     string // location of the error, of format file:line_number
	item         error  // the encapsulated error
}

func (s ServerError) Error() string {
	return fmt.Sprintf("\n[%s] @ %s: %s", s.functionName, s.location, s.item)
}

func (s ServerError) Unwrap() (list []error) {
	list = make([]error, 0)

	currentItem := s.item

	serverError := &ServerError{}
	for errors.As(currentItem, serverError) {
		list = append(list, serverError)
		currentItem = serverError.item
	}

	if currentItem != nil {
		list = append(list, currentItem)
	}

	return list
}

func NewServerError(e error) ServerError {
	programCounter, file, lineNumber, _ := runtime.Caller(1) // get information about caller
	return ServerError{
		runtime.FuncForPC(programCounter).Name(),
		fmt.Sprintf("%s:%d", file, lineNumber),
		e,
	}
}

// Error that's used to communicate the type of malfunction to the user (anywhere between 4xx to 5xx)
type APIError struct {
	StatusCode  int         `json:"code"`    // The HTTP code to be used for the error
	Message     string      `json:"message"` // The error message
	serverError ServerError // The underlying server error being bubbled up
}

func (a APIError) Error() string {
	return fmt.Sprintf("\n%s\n%s", a.Message, &a.serverError)
}

func (a APIError) Unwrap() (list []error) {
	return a.serverError.Unwrap()
}

// Logs an error generated at a particular position to the logging module.
func LogError(err error) {
	programCounter, file, lineNumber, ok := runtime.Caller(1) // get information about caller
	if ok {
		log.Printf("[%s] \"%s\" --> %s:%d", runtime.FuncForPC(programCounter).Name(), err, file, lineNumber)
	}
}

func SuccessfulResponseJson(writer http.ResponseWriter, request *http.Request, message any) error {
	content, err := json.Marshal(message)
	if err != nil {
		return NewServerError(err)
	}

	if request.Method == "GET" {
		GlobalCache.Cache(request.URL.Path, content)
	}
	writer.Header().Add("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	writer.Write(content)

	return nil
}

// Writes headers to signify a successful response, and then writes the content to a response stream.
func SuccessfulResponse(writer http.ResponseWriter, request *http.Request, content []byte) {
	if request.Method == "GET" {
		GlobalCache.Cache(request.URL.Path, content)
	}
	writer.Header().Add("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	writer.Write(content)
}

func SuccessfulGzippedResponse(writer http.ResponseWriter, request *http.Request, content []byte, isJson bool) {
	if request.Method == "GET" {
		GlobalCache.Cache(request.URL.Path+"?"+request.URL.RawQuery, content)
	}
	if isJson {
		writer.Header().Add("Content-Type", "application/json")
	} else {
		writer.Header().Add("Content-Type", "text/csv")
	}
	writer.Header().Add("Content-Encoding", "gzip")
	gzipWriter := gzip.NewWriter(writer)
	writer.WriteHeader(http.StatusOK)
	gzipWriter.Write(content)
}

// Messages for some common errors.
const (
	OPEN_DB_ERROR        = "Could not access database."
	WRITE_DB_ERROR       = "Could not write to database."
	DELETE_DB_ERROR      = "Could not delete from database."
	JSON_MARSHAL_ERROR   = "Could not serialize document to JSON."
	JSON_UNMARSHAL_ERROR = "Could not decode JSON."
)

// A data type that encapsulates a route handler and its middleware.
type Endpoint struct {
	BaseHandler func(http.ResponseWriter, *http.Request) error
	Middleware  []func(http.HandlerFunc) http.HandlerFunc
}

func NewEndpoint(baseHandler func(http.ResponseWriter, *http.Request) error) *Endpoint {
	return &Endpoint{
		baseHandler,
		make([]func(http.HandlerFunc) http.HandlerFunc, 0),
	}
}

func (e *Endpoint) AddMiddleware(middleware func(http.HandlerFunc) http.HandlerFunc) *Endpoint {
	e.Middleware = append(e.Middleware, middleware)
	return e
}

// Build out a handler with the specified middleware and base handler.
func (e *Endpoint) Finalize() http.HandlerFunc {
	finalHandler := func(w http.ResponseWriter, r *http.Request) {
		err := e.BaseHandler(w, r)
		apiError := &APIError{}

		if err != nil {
			if errors.As(err, apiError) {
				HandleAPIError(w, r, *apiError)
			} else {
				// unknown error at this point
				LogError(err)
				HandleError(w)
			}
		}
	}

	for _, mw := range e.Middleware {
		finalHandler = mw(finalHandler)
	}

	return finalHandler
}
