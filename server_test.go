package main

import (
	"encoding/json"
	"fmt"
	morphoroutes "github.com/MorphoDesignExplorer/morphodb-go/morpho-routes"
	"github.com/gorilla/mux"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func init() {
	morphoroutes.GlobalCache = &morphoroutes.Cacher{}
	morphoroutes.GlobalCache.InitCache()
}

func getFileBytes(path string) ([]byte, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    bytes, err := io.ReadAll(file)
    if err != nil {
        return nil, err
    }
    return bytes, nil
}

func TestProjectEndpoint(t *testing.T) {
	t.Run("TestProjectEndpoint", func(t *testing.T) {
		config, err := morphoroutes.GetConfig()
		if err != nil {
			t.Errorf("error while assembling config: %s", err)
		}

		getProjects := morphoroutes.GetProjectsWrapper(config)

		request, _ := http.NewRequest(http.MethodGet, "/project/", nil)
		response := httptest.NewRecorder()

		getProjects(response, request)
		endpointOutput := response.Body.String()

        fileBytes, err := getFileBytes("./tests/project.json")
        if err != nil {
            t.Errorf("error while opening file: %s", err)
        }

		referenceOutput := string(fileBytes)

		if referenceOutput != endpointOutput {
			t.Errorf("got %d, want %d", len(endpointOutput), len(referenceOutput))
		}
	})
}

func TestSolutionEndpoint(t *testing.T) {
	t.Run("TestProjectModelEndpoint", func(t *testing.T) {
		config, err := morphoroutes.GetConfig()
		if err != nil {
			t.Errorf("error while assembling config: %s", err)
		}

        endpoints := []string{"GCGA_10", "GCGA_39"}

        for _, endpoint := range endpoints {
            request, _ := http.NewRequest(http.MethodGet, "project/{}/model/", nil)
            response := httptest.NewRecorder()
            variables := map[string]string{
                "project": endpoint,
            }

            request = mux.SetURLVars(request, variables)

            deserializeArray := func (b []byte) (slice []interface{}) {
                slice = make([]interface{}, 0)
                json.Unmarshal(b, &slice)
                return
            }

            getSolutions := morphoroutes.GetSolutionsWrapper(config)
            getSolutions(response, request)


            endpointArray := deserializeArray(response.Body.Bytes())

            fileBytes, err := getFileBytes(fmt.Sprintf("./tests/model_%s.json", endpoint))
            if err != nil {
                t.Errorf("error while opening file: %s", err)
            }

            referenceArray := deserializeArray(fileBytes)

            existence := make(map[string]bool)
            missingCount := 0
            for _, val := range referenceArray {
                existence[fmt.Sprintf("%q", val)] = true
            }

            for _, val := range endpointArray {
                if !existence[fmt.Sprintf("%q", val)] {
                    missingCount += 1
                }
            }

            if missingCount > 0 {
                t.Errorf("reference has %d records, endpoint got %d records. %d records did not match", len(referenceArray), len(endpointArray), missingCount)
            }
        }
	})
}
