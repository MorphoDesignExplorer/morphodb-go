//go:generate ffjson $GOFILE

package morphoroutes

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"

	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	"github.com/pquerna/ffjson/ffjson"
)

// var DB_STRING string = fmt.Sprintf("file:%s?_journal:WAL", os.Getenv("DB_STRING"))

type Description struct {
	Slug string `json:"slug"`
	Text string `json:"text"`
}

type Metadata struct {
	Captions    *json.RawMessage `json:"captions"`
	Description Description      `json:"description"`
	HumanName   string           `json:"human_name"`
}

type Project struct {
	CreationDate     string           `json:"creation_date"`
	ProjectName      string           `json:"project_name"`
	VariableMetadata *json.RawMessage `json:"variable_metadata"`
	OutputMetadata   *json.RawMessage `json:"output_metadata"`
	Assets           *json.RawMessage `json:"assets"`
	Deleted          bool             `json:"deleted"`
	ProjectMetadata  Metadata         `json:"metadata"`
}

type Solution struct {
	Id              string           `json:"id"`
	ScopedId        string           `json:"scoped_id"`
	Parameter       *json.RawMessage `json:"parameters"`
	OutputParameter *json.RawMessage `json:"output_parameters"`
	Assets          []Asset          `json:"files"`
}

type Asset struct {
	Tag  string `json:"tag"`
	File string `json:"file"`
}

type ErrorMessage struct {
	Message string `json:"message"`
}

// Writes a 500 to the output stream.
//
// The calling route should return after invoking this function.
func HandleError(writer http.ResponseWriter) {
	writer.Header().Add("Content-Type", "application/json")
	writer.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(writer).Encode(ErrorMessage{"Internal Server Error"})
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
// dbString is the path to the SQLite database.
//
// Returns the set of projects fetched, or an error.
func GetProjects(variables map[string]string, dbString string) ([]Project, error) {
	broadQuery := "select creation_date, project.project_name, variable_metadata, output_metadata, assets, deleted, metadata.captions, metadata.slug, metadata.markdown, metadata.human_name FROM project, metadata where project.project_name = metadata.project_name;"
	constrictedQuery := "select creation_date, project.project_name, variable_metadata, output_metadata, assets, deleted, metadata.captions, metadata.slug, metadata.markdown, metadata.human_name FROM project, metadata WHERE project.project_name = metadata.project_name AND project.project_name = ?;"

	projectName, singularRequest := variables["project"]

	db, err := sql.Open("sqlite3", dbString)
	if err != nil {
		LogError(err)
		return nil, err
	}

	defer db.Close()

	var result *sql.Rows

	if singularRequest {
		result, err = db.Query(constrictedQuery, projectName)
		if err != nil {
			LogError(err)
			return nil, err
		}
	} else {
		result, err = db.Query(broadQuery)
		if err != nil {
			LogError(err)
			return nil, err
		}
	}
	defer result.Close()

	projects := make([]Project, 0)
	for result.Next() {
		var temp Project
		var variableMetadata, outputMetadata, assets, captions []byte
		var tempMetadata Metadata
		var tempDescription Description

		err = result.Scan(&temp.CreationDate, &temp.ProjectName, &variableMetadata, &outputMetadata, &assets, &temp.Deleted, &captions, &tempDescription.Slug, &tempDescription.Text, &tempMetadata.HumanName)
		if err != nil {
			LogError(err)
			return nil, err
		}

		tempMetadata.Captions = (*json.RawMessage)(&captions)
		temp.VariableMetadata = (*json.RawMessage)(&variableMetadata)
		temp.OutputMetadata = (*json.RawMessage)(&outputMetadata)
		temp.Assets = (*json.RawMessage)(&assets)
		tempMetadata.Description = tempDescription
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
// dbString is the path to the SQLite database.
//
// urlGenerator generates the file path prefix for the assets fetched.
//
// Returns the set of solutions, or an error.
func GetSolutions(variables map[string]string, dbString string, urlGenerator func(string) string) ([]Solution, error) {
	db, err := sql.Open("sqlite3", dbString)
	if err != nil {
		LogError(err)
		return nil, err
	}
	defer db.Close()

	projectName := variables["project"]
	solutionId, singularRequest := variables["solution"]

	broadQuery := "SELECT solution.id, solution.scoped_id, parameters, output_parameters, tag, file FROM solution, asset WHERE asset.solution_id = solution.id AND solution.project_name = ?"
	constrictedQuery := "SELECT solution.id, solution.scoped_id, parameters, output_parameters, tag, file FROM solution, asset WHERE asset.solution_id = solution.id AND solution.project_name = ? AND solution.id = ?"

	var result *sql.Rows

	if singularRequest {
		result, err = db.Query(constrictedQuery, projectName, solutionId)
		if err != nil {
			LogError(err)
			return nil, err
		}
		defer result.Close()
	} else {
		result, err = db.Query(broadQuery, projectName)
		if err != nil {
			return nil, err
		}
		defer result.Close()
	}

	solutions := make(map[string]Solution)
	for result.Next() {
		var tempSolution Solution
		var parameter, outputParameter []byte
		var fileTag, fileUri string

		err = result.Scan(&tempSolution.Id, &tempSolution.ScopedId, &parameter, &outputParameter, &fileTag, &fileUri)
		if err != nil {
			LogError(err)
			return nil, err
		}
		tempSolution.Parameter = (*json.RawMessage)(&parameter)
		tempSolution.OutputParameter = (*json.RawMessage)(&outputParameter)
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
		dbString := fmt.Sprintf("file:%s?_journal:WAL", config.DB_STRING)
		projectSet, err := GetProjects(variables, dbString)
		bytes, err := ffjson.Marshal(projectSet)
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
		return fmt.Sprintf("%s/%s/%s/", config.AWS_S3_ENDPOINT_URL, config.AWS_STORAGE_BUCKET_NAME, "media") + filename
	}

	return func(writer http.ResponseWriter, request *http.Request) {
		variables := mux.Vars(request) // map that has the key 'project' and may or may not have the key 'solution'
		dbString := fmt.Sprintf("file:%s?_journal:WAL", config.DB_STRING)
		solutionSet, err := GetSolutions(variables, dbString, urlGenerator)
		if err != nil {
			HandleError(writer)
		}

		bytes, err := ffjson.Marshal(solutionSet)
		if err != nil {
			HandleError(writer)
			return
		}

		GlobalCache.Cache(request.URL.Path, bytes)
		SuccessfulResponse(writer, request, &bytes)
	}
}
