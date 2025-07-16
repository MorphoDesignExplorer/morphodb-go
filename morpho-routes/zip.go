package morphoroutes

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gabriel-vasile/mimetype"
)

type FileTuple struct {
	name string
	file *zip.File
}

func IsDir(filename string) bool {
	return filename[len(filename)-1] == os.PathSeparator
}

func MakeTree(multipartFile *multipart.FileHeader) (map[string]FileTuple, error) {
	files := make(map[string]FileTuple)

	fileObj, err := multipartFile.Open()
	if err != nil {
		return nil, NewServerError(err)
	}

	arc, err := zip.NewReader(fileObj, multipartFile.Size)
	if err != nil {
		return nil, NewServerError(err)
	}

	for _, f := range arc.File {
		files[f.Name] = FileTuple{
			name: f.Name,
			file: f,
		}
	}

	return files, nil
}

func unpackItem(file *zip.File, dirname string) (filename string, err error) {
	filename = "./" + path.Join(dirname, "solutions.db")
	fileHandle, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0777)
	defer func() {
		err = fileHandle.Close()
	}()

	if err != nil {
		return "", err
	}

	rc, err := file.Open()
	if err != nil {
		return "", err
	}

	contents, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	n, err := fileHandle.Write(contents)
	if n < len(contents) || err != nil {
		fmt.Println("could not write all the contents.")
		panic(err)
	}

	return filename, nil
}

type TempAsset struct {
	Tag        string
	File       string
	SolutionId string
}

func getAssets(tx *sql.Tx) ([]TempAsset, error) {
	assets := make([]TempAsset, 0)

	rows, err := tx.Query("SELECT file, tag, solution_id FROM asset")
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		tempAsset := TempAsset{}
		err = rows.Scan(&tempAsset.File, &tempAsset.Tag, &tempAsset.SolutionId)
		if err != nil {
			return nil, err
		}
		assets = append(assets, tempAsset)
	}

	return assets, nil
}

/*
Uncompresses a file within a zipped folder and uploads it to a local directory.

Returns the filepath where the uncompressed file can be found, along with an error if there's any.
*/
func unpackAndUploadToLocal(file *zip.File, name string) (string, error) {
	rc, err := file.Open()
	if err != nil {
		return "", NewServerError(err)
	}

	contents, err := io.ReadAll(rc)
	if err != nil {
		return "", NewServerError(err)
	}

	mime := mimetype.Detect(contents)

	if writeHandle, err := os.OpenFile(fmt.Sprintf("assets/%s%s", name, mime.Extension()), os.O_CREATE|os.O_RDWR, 0644); err == nil {
		n, err := writeHandle.Write(contents)
		if n != len(contents) || err != nil {
			return "", NewServerError(
				fmt.Errorf("could not write complete file: %w", err),
			)
		}
		return name + mime.Extension(), nil
	} else {
		return "", NewServerError(err)
	}
}

func unpackAndUploadToS3(file *zip.File, name string) (string, error) {
	rc, err := file.Open()
	if err != nil {
		return "", err
	}

	contents, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	mime := mimetype.Detect(contents)

	client, err := CreateS3Client()
	if err != nil {
		return "", nil
	}

	_, err = client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:      aws.String("morpho-images"),
		Key:         aws.String(fmt.Sprintf("assets/%s%s", name, mime.Extension())),
		Body:        bytes.NewReader(contents),
		ContentType: aws.String(mime.String()),
	})

	return fmt.Sprintf("assets/%s%s", name, mime.Extension()), err
}

// Local function for interop with uploaded database
func getMetadata(db *sql.DB, projectName string) (m Metadata, err error) {
	row := db.QueryRow("SELECT captions, human_name, slug, text FROM metadata WHERE project_name=?", projectName)
	err = row.Scan(&m.Captions, &m.HumanName, &m.Description.Slug, &m.Description.Text)
	return
}

func UploadProject(service Service, file *multipart.FileHeader) (projectName string, err error) {
	tree, err := MakeTree(file)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not open zip file for reading.", NewServerError(err)}
	}

	//
	// create local directory for importing the db file
	//

	// TODO create a random temp directory for file ops

	randDir := "./temp" + randString(8)

	err = os.Mkdir(randDir, 0777)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not create temporary working directory.", NewServerError(err)}
	}

	defer func() {
		removeErr := os.RemoveAll(randDir)
		if removeErr != nil {
			removeErr = APIError{http.StatusInternalServerError, "Could not clear temporary working directory.", NewServerError(err)}
		}

		if err == nil {
			err = removeErr
		}
	}()

	//
	// extract imported db into local directory
	//

	var solutionDbPath string = ""
	var prefix string = ""
	for key := range tree {
		if strings.Contains(key, "solutions.db") {
			solutionDbPath = key
			prefix = path.Dir(key)
		}
	}

	if solutionDbPath == "" {
		err = fmt.Errorf("Could not find solution DB within zip.")
		return "", APIError{http.StatusBadRequest, err.Error(), NewServerError(err)}
	}

	filename, err := unpackItem(tree[solutionDbPath].file, randDir)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not extract DB from zip file.", NewServerError(err)}
	}

	// get handles to temporary and permanent db

	tempdb, err := sql.Open(GetDriver(), fmt.Sprintf("file:%s", filename))
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not access imported database.", NewServerError(err)}
	}

	realdb, err := StartConn(service)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not access internal database.", NewServerError(err)}
	}

	//
	// start transactions
	//

	temptx, err := tempdb.Begin()
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not access imported database.", NewServerError(err)}
	}
	defer temptx.Rollback()

	realtx, err := realdb.Begin()
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not access internal database.", NewServerError(err)}
	}
	defer realtx.Rollback()

	//
	// insert project and associated solutions
	//

	projects, err := GetAllProjects(tempdb)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Querying imported database failed.", NewServerError(err)}
	}

	if err = projects[0].Create(realtx); err != nil {
		return "", APIError{http.StatusServiceUnavailable, "Could not insert project into database.", NewServerError(err)}
	}

	solutions, err := GetAllSolutions(temptx, projects[0].ProjectName)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Querying imported database failed.", NewServerError(err)}
	}

	solutionSet := SolutionSet(solutions)
	err = solutionSet.Create(realtx, projects[0].ProjectName)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Inserting records into internal database failed.", NewServerError(err)}
	}

	assets, err := getAssets(temptx)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Querying imported database failed.", NewServerError(err)}
	}

	//
	// insert assets
	//

	for _, asset := range assets {
		randomName := fmt.Sprintf("%s_%s", asset.SolutionId, randString(7))
		var s3Url string
		if service.ENVIRONMENT == "prod" {
			s3Url, err = unpackAndUploadToS3(tree[path.Join(prefix, strings.ReplaceAll(asset.File, "\\", string(filepath.Separator)))].file, randomName)
		} else {
			s3Url, err = unpackAndUploadToLocal(tree[path.Join(prefix, strings.ReplaceAll(asset.File, "\\", string(filepath.Separator)))].file, randomName)
		}
		if err != nil {
			return "", APIError{http.StatusInternalServerError, "Could not upload an asset.", NewServerError(err)}
		}

		a := Asset{Tag: asset.Tag, File: s3Url}
		if err = a.Create(realtx, asset.SolutionId); err != nil {
			return "", APIError{http.StatusInternalServerError, "Could not insert asset into the database.", NewServerError(err)}
		}
	}

	//
	// insert metadata
	//

	metadata, err := getMetadata(tempdb, projects[0].ProjectName)
	if err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not extract metadata from imported database.", NewServerError(err)}
	}

	if err = metadata.Create(realtx, projects[0].ProjectName); err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not insert imported metadata into internal database.", NewServerError(err)}
	}

	//
	// wrap up
	//

	if err = realtx.Commit(); err != nil {
		return "", APIError{http.StatusInternalServerError, "Could not commit transaction on internal database.", NewServerError(err)}
	}

	return projects[0].ProjectName, nil
}
