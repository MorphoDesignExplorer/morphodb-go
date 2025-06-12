package morphoroutes

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"

	_ "github.com/glebarez/go-sqlite" // pure go driver for windows platforms
	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3" // go driver for linux platforms
)

type ErrorMessage struct {
	Message string `json:"message"`
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
	json.NewEncoder(writer).Encode(ErrorMessage{"Internal Server Error"})
}

// Writes a 500 to the output stream, with  a custom error message.
//
// The calling route should return after invoking this function.
//
// Parameters:
//
// writer: a handler to a ResponseWriter
//
// err: An error to be communicated with the user.
func HandleErrorWithMessage(writer http.ResponseWriter, err error) {
	writer.Header().Add("Content-Type", "application/json")
	writer.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(writer).Encode(ErrorMessage{err.Error()})
}

// Logs an error generated at a particular position to the logging module.
func LogError(err error) {
	programCounter, file, lineNumber, ok := runtime.Caller(1) // get information about caller
	if ok {
		log.Printf("[%s] \"%s\" --> %s:%d", runtime.FuncForPC(programCounter).Name(), err, file, lineNumber)
	}
}

// Writes headers to signify a successful response, and then writes the content to a response stream.
func SuccessfulResponse(writer http.ResponseWriter, request *http.Request, content *([]byte)) {
	GlobalCache.Cache(request.URL.Path, *content)
	writer.Header().Add("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	writer.Write(*content)
}

// Fetches all the projects or a singular project.
//
// Variables is a map that could contain the key project, which denotes the singular project to select. Do not provide the key if you need to fetch all the projects.
//
// config is the set of environment variables.
//
// Returns the set of projects fetched, or an error.
func GetProjects(variables map[string]string, config Config) ([]Project, error) {
	broadQuery := "select creation_date, project.project_name, variable_metadata, output_metadata, assets, deleted, metadata.captions, metadata.slug, metadata.markdown, metadata.human_name FROM project, metadata where project.project_name = metadata.project_name;"
	constrictedQuery := "select creation_date, project.project_name, variable_metadata, output_metadata, assets, deleted, metadata.captions, metadata.slug, metadata.markdown, metadata.human_name FROM project, metadata WHERE project.project_name = metadata.project_name AND project.project_name = ?;"

	projectName, singularRequest := variables["project"]

	db, err := StartConn(config)
	if err != nil {
		LogError(err)
		return nil, err
	}
	defer db.Close()

	var result *sql.Rows
	if singularRequest {
		result, err = db.Query(constrictedQuery, projectName)
	} else {
		result, err = db.Query(broadQuery)
	}

	if err != nil {
		LogError(err)
		return nil, err
	}
	defer result.Close()

	projects := make([]Project, 0)
	for result.Next() {
		var temp Project
		var tempMetadata Metadata

		err = result.Scan(&temp.CreationDate, &temp.ProjectName, &temp.VariableMetadata, &temp.OutputMetadata, &temp.Assets, &temp.Deleted, &tempMetadata.Captions, &tempMetadata.Description.Slug, &tempMetadata.Description.Text, &tempMetadata.HumanName)
		if err != nil {
			return nil, err
		}

		temp.ProjectMetadata = tempMetadata
		projects = append(projects, temp)
	}

	return projects, nil
}

// GET method that returns either a singular solution or all solutions under a particular project.
// Fetches all solutions or a single solution associated with a project.
//
// variables is a map that contains the project and solution key, where project should be filled and solution can be ommitted.
// the solution key can be omitted to fetch all solutions associated with a project.
//
// config is the set of environment variables.
//
// urlGenerator generates the file path prefix for the assets fetched.
//
// Returns the set of solutions, or an error.
func GetSolutions(variables map[string]string, config Config, urlGenerator func(string) string) ([]Solution, error) {
	broadQuery := "SELECT solution.id, solution.scoped_id, parameters, output_parameters, tag, file FROM solution, asset WHERE asset.solution_id = solution.id AND solution.project_name = ?"
	constrictedQuery := "SELECT solution.id, solution.scoped_id, parameters, output_parameters, tag, file FROM solution, asset WHERE asset.solution_id = solution.id AND solution.project_name = ? AND solution.id = ?"

	projectName := variables["project"]
	solutionId, singularRequest := variables["solution"]

	db, err := StartConn(config)
	if err != nil {
		LogError(err)
		return nil, err
	}
	defer db.Close()

	var result *sql.Rows
	if singularRequest {
		result, err = db.Query(constrictedQuery, projectName, solutionId)
	} else {
		result, err = db.Query(broadQuery, projectName)
	}
	if err != nil {
		LogError(err)
		return nil, err
	}
	defer result.Close()

	solutions := make(map[string]Solution)
	for result.Next() {
		var tempSolution Solution
		var fileTag, fileUri string

		err = result.Scan(&tempSolution.Id, &tempSolution.ScopedId, &tempSolution.Parameter, &tempSolution.OutputParameter, &fileTag, &fileUri)
		if err != nil {
			LogError(err)
			return nil, err
		}

		fileUri = urlGenerator(fileUri)

		if solution, ok := solutions[tempSolution.Id]; ok {
			solution.Assets = append(solution.Assets, Asset{Tag: fileTag, File: fileUri})
			solutions[tempSolution.Id] = solution
		} else {
			tempSolution.Assets = make([]Asset, 0)
			tempSolution.Assets = append(tempSolution.Assets, Asset{Tag: fileTag, File: fileUri})
			solutions[tempSolution.Id] = tempSolution
		}
	}

	solutionSet := make([]Solution, 0, len(solutions))
	for _, solution := range solutions {
		solutionSet = append(solutionSet, solution)
	}

	return solutionSet, nil
}

// GET method that returns either a singular project or all the projects.
//
// config is the set of environment variables needed to serve the request.
//
// Returns an HTTP handler for the GetProjects route.
func GetProjectsWrapper(config Config) func(http.ResponseWriter, *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		variables := mux.Vars(request) // map that may or may not have the key 'project'
		projectSet, err := GetProjects(variables, config)
		if err != nil {
			HandleError(writer)
			return
		}

		bytes, err := json.Marshal(projectSet)
		if err != nil {
			HandleError(writer)
			return
		}

		GlobalCache.Cache(request.URL.Path, bytes)
		SuccessfulResponse(writer, request, &bytes)
	}
}

// GET method that returns either a singular solution or all the solutions associated with a project.
//
// config is the set of environment variables needed to serve the request.
//
// Returns an HTTP handler for the GetSolutions route.
func GetSolutionsWrapper(config Config) func(http.ResponseWriter, *http.Request) {
	urlGenerator := func(filename string) string {
		return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", config.AWS_STORAGE_BUCKET_NAME, config.AWS_REGION, filename)
	}

	return func(writer http.ResponseWriter, request *http.Request) {
		variables := mux.Vars(request) // map that has the key 'project' and may or may not have the key 'solution'
		solutionSet, err := GetSolutions(variables, config, urlGenerator)
		if err != nil {
			HandleError(writer)
			return
		}

		bytes, err := json.Marshal(solutionSet)
		if err != nil {
			HandleError(writer)
			return
		}

		GlobalCache.Cache(request.URL.Path, bytes)
		SuccessfulResponse(writer, request, &bytes)
	}
}

func GetDocumentWrapper(config Config) func(http.ResponseWriter, *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		variables := mux.Vars(request)
		db, err := StartConn(config)
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		tx, err := db.Begin()
		if err != nil {
			LogError(err)
			HandleError(writer)
			return
		}

		var docBytes []byte

		if docIdOrSlug, ok := variables["idOrSlug"]; ok {
			doc, err := GetDocument(tx, docIdOrSlug)
			if err != nil {
				LogError(err)
				HandleError(writer)
				return
			}

			docBytes, err = json.Marshal(doc)
			if err != nil {
				LogError(err)
				HandleError(writer)
				return
			}
		} else {
			docs, err := GetAllDocuments(tx)
			if err != nil {
				LogError(err)
				HandleError(writer)
				return
			}

			docBytes, err = json.Marshal(docs)
			if err != nil {
				LogError(err)
				HandleError(writer)
				return
			}
		}

		SuccessfulResponse(writer, request, &docBytes)
		return
	}
}
