package morphoroutes

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"mime/multipart"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gabriel-vasile/mimetype"
	"github.com/google/uuid"
)

/*
	Project Methods
*/

/*
Gets all the projects available in the database.

Returns a list of Project structs and an error, if there's an issue during querying or unpacking results.
*/
func GetAllProjects(db *sql.DB) ([]Project, error) {
	rows, err := db.Query("SELECT creation_date, project_name, variable_metadata, output_metadata, assets, deleted FROM project")
	if err != nil {
		return []Project{}, NewServerError(err)
	}

	projects := make([]Project, 0)

	for rows.Next() {
		var p Project
		err = rows.Scan(&p.CreationDate, &p.ProjectName, &p.VariableMetadata, &p.OutputMetadata, &p.Assets, &p.Deleted)
		if err != nil {
			return []Project{}, NewServerError(err)
		}
		projects = append(projects, p)
	}

	return projects, nil
}

/*
Gets a particular project from the database.

Returns the project and an error.
*/
func GetProject(db *sql.DB, projectName string) (Project, error) {
	if row, err := db.Query(
		"SELECT creation_date, project_name, variable_metadata, output_metadata, assets, deleted FROM project WHERE project_name = ?",
		projectName,
	); err == nil {
		var p Project
		row.Next()
		row.Scan(&p.CreationDate, &p.ProjectName, &p.VariableMetadata, &p.OutputMetadata, &p.Assets, &p.Deleted)
		return p, err
	} else {
		return Project{}, err
	}
}

/*
Deletes a project.

db is the database object.

Returns an error if the database write fails.
*/
func (p *Project) Delete(tx *sql.Tx) error {
	_, err := tx.Exec("DELETE FROM asset WHERE asset.solution_id IN (SELECT solution.id FROM solution WHERE solution.project_name = ?)", p.ProjectName)
	if err != nil {
		return NewServerError(err)
	}

	_, err = tx.Exec("DELETE FROM solution WHERE project_name = ?", p.ProjectName)
	if err != nil {
		return NewServerError(err)
	}

	_, err = tx.Exec("DELETE FROM metadata WHERE project_name = ?", p.ProjectName)
	if err != nil {
		return NewServerError(err)
	}

	_, err = tx.Exec("DELETE FROM project WHERE project_name = ?", p.ProjectName)
	if err != nil {
		return NewServerError(err)
	}

	return nil
}

/*
Creates a project.

db is the database object.

Returns an error if either the object validation or the database write fails.
*/
func (p *Project) Create(tx *sql.Tx) error {
	if err := Validate(*p); err != nil {
		return err
	}

	vm, err := json.Marshal(p.VariableMetadata)
	if err != nil {
		return err
	}

	om, err := json.Marshal(p.OutputMetadata)
	if err != nil {
		return err
	}

	as, err := json.Marshal(p.Assets)
	if err != nil {
		return err
	}

	if _, err = tx.Exec(
		"INSERT INTO project (creation_date, project_name, variable_metadata, output_metadata, assets, deleted) VALUES (?, ?, ?, ?, ?, ?)",
		p.CreationDate,
		p.ProjectName,
		string(vm),
		string(om),
		string(as),
		0,
	); err != nil {
		return err
	}

	return nil
}

/*
Updates a project's metadata fields, with values from the object's current fields.
Make sure to only modify the unit fields when using this.

db is the database object.

Returns an error if either the object validation or the database write fails.
*/
func (p *Project) Update(tx *sql.Tx) error {
	vm, err := json.Marshal(p.VariableMetadata)
	if err != nil {
		return err
	}

	om, err := json.Marshal(p.OutputMetadata)
	if err != nil {
		return err
	}

	as, err := json.Marshal(p.Assets)
	if err != nil {
		return err
	}

	if _, err = tx.Exec(
		"UPDATE project SET variable_metadata = ?, output_metadata = ?, assets = ? WHERE project_name = ?",
		string(vm),
		string(om),
		string(as),
		p.ProjectName,
	); err != nil {
		return err
	}

	return nil
}

/*
	[]Solution Methods
*/

/*
 * Gets a singular solution by the solution id and the project.
 *
 * Takes tx: an SQL transaction, projectName: the name of the project, and solutionId: the id of a solution
 *
 * assetUrlGenerator is a pointer to a function that generates a url from an existing file uri. If you'd like to leave the fetched uri as it is, pass nil.
 *
 * Returns the fetched solution and an error, if there's any
 */
func GetSolution(tx *sql.Tx, projectName string, solutionId string, assetUrlGenerator *func(string) string) (Solution, error) {
	var tempSol Solution

	var urlGenerator func(string) string
	if assetUrlGenerator != nil {
		urlGenerator = *assetUrlGenerator
	} else {
		urlGenerator = func(fileuri string) string {
			return fileuri
		}
	}

	row := tx.QueryRow("SELECT id, scoped_id, parameters, output_parameters FROM solution WHERE project_name = ? AND id = ?", projectName, solutionId)
	if err := row.Scan(&tempSol.Id, &tempSol.ScopedId, &tempSol.Parameter, &tempSol.OutputParameter); err != nil {
		return Solution{}, NewServerError(err)
	} else {
		rows, err := tx.Query("SELECT asset.tag, asset.file FROM asset WHERE asset.solution_id = ?", tempSol.Id)
		if err != nil {
			return Solution{}, NewServerError(err)
		}

		for rows.Next() {
			var tempAsset Asset
			if err = rows.Scan(&tempAsset.Tag, &tempAsset.File); err != nil {
				return Solution{}, NewServerError(err)
			} else {
				tempAsset.File = urlGenerator(tempAsset.File)
				tempSol.Assets = append(tempSol.Assets, tempAsset)
			}
		}

		return tempSol, nil
	}
}

/*
 * A method to fetch a project's solutions and associated assets from a database.
 *
 * Takes tx, an SQL Transaction object and projectName, the name of the project.
 *
 * assetUrlGenerator is a pointer to a function that generates a url from an existing file uri. If you'd like to leave the fetched uri as it is, pass nil.
 *
 * Returns a slice of solutions and an error, if there's any.
 */
func GetAllSolutions(tx *sql.Tx, projectName string, assetUrlGenerator *func(string) string) ([]Solution, error) {
	var urlGenerator func(string) string
	if assetUrlGenerator != nil {
		urlGenerator = *assetUrlGenerator
	} else {
		urlGenerator = func(fileuri string) string {
			return fileuri
		}
	}

	solutions := make([]Solution, 0)
	rows, err := tx.Query("SELECT id, scoped_id, parameters, output_parameters FROM solution WHERE project_name = ?", projectName)
	if err != nil {
		return nil, NewServerError(err)
	}

	idsToOffset := make(map[string]int)

	for rows.Next() {
		var tempSolution Solution
		err = rows.Scan(&tempSolution.Id, &tempSolution.ScopedId, &tempSolution.Parameter, &tempSolution.OutputParameter)
		if err != nil {
			return nil, NewServerError(err)
		}
		solutions = append(solutions, tempSolution)
		idsToOffset[tempSolution.Id] = len(solutions) - 1
	}
	rows.Close()

	rows, err = tx.Query("SELECT asset.tag, asset.file, asset.solution_id FROM asset WHERE asset.solution_id in (SELECT id FROM solution WHERE project_name = ?)", projectName)
	if err != nil {
		return nil, NewServerError(err)
	}

	for rows.Next() {
		var tempAsset Asset
		var solutionId string
		err = rows.Scan(&tempAsset.Tag, &tempAsset.File, &solutionId)
		if err != nil {
			return nil, NewServerError(err)
		}

		tempAsset.File = urlGenerator(tempAsset.File)

		if offset, ok := idsToOffset[solutionId]; ok {
			solutions[offset].Assets = append(solutions[offset].Assets, tempAsset)
		}
	}

	return solutions, nil
}

/*
Saves a list of Solution objects under a projectName.

db is the database object.
projectName is the name of the project to associate the solutions with.

An error is returned if the object validation or the database write fails.
*/
func (s SolutionSet) Create(tx *sql.Tx, projectName string) error {
	if err := Validate(s); err != nil {
		return NewServerError(err)
	}

	stmt, err := tx.Prepare(
		"INSERT INTO solution (id, parameters, output_parameters, project_name, scoped_id) VALUES (?, ?, ?, ?, ?)",
	)
	if err != nil {
		return NewServerError(err)
	}
	defer stmt.Close()

	for _, solution := range s {
		if err := Validate(s); err != nil {
			return NewServerError(err)
		}

		iparam, err := json.Marshal(solution.Parameter)
		if err != nil {
			return NewServerError(err)
		}

		oparam, err := json.Marshal(solution.OutputParameter)
		if err != nil {
			return NewServerError(err)
		}

		scopedId, err := strconv.ParseInt(solution.ScopedId, 10, 32)
		if err != nil {
			return NewServerError(err)
		}

		_, err = stmt.Exec(
			solution.Id,
			string(iparam),
			string(oparam),
			projectName,
			scopedId,
		)
		if err != nil {
			return NewServerError(err)
		}
	}

	return nil
}

// Marshals a SolutionSet into a CSV string.
// If archive is set to true, the output is human readable.
func (s SolutionSet) CsvMarshal(archive bool) []byte {
	out := make([]byte, 0)

	headers := []string{"id", "scoped_id"}
	for key := range s[0].Parameter {
		if archive {
			headers = append(headers, key)
		} else {
			headers = append(headers, "parameters."+key)
		}
	}
	for key := range s[0].OutputParameter {
		if archive {
			headers = append(headers, key)
		} else {
			headers = append(headers, "output_parameters."+key)
		}
	}
	for _, asset := range s[0].Assets {
		if archive {
			headers = append(headers, asset.Tag)
		} else {
			headers = append(headers, "asset."+asset.Tag)
		}
	}

	out = append(out, []byte(strings.Join(headers, ",")+"\n")...)
	for _, solution := range s {
		row := []string{solution.Id, solution.ScopedId}
		for _, value := range solution.Parameter {
			row = append(row, strconv.FormatFloat(value, 'g', -1, 64))
		}
		for _, value := range solution.OutputParameter {
			row = append(row, strconv.FormatFloat(value, 'g', -1, 64))
		}
		for _, asset := range solution.Assets {
			row = append(row, asset.File)
		}
		out = append(out, []byte(strings.Join(row, ",")+"\n")...)
	}

	return out
}

/*
	Metadata Methods
*/

/*
Gets a metadata record from the database.

Returns an error if the database is unreachable or if the record does not exist.
*/
func GetMetadata(db *sql.DB, projectName string) (m Metadata, err error) {
	row := db.QueryRow("SELECT captions, human_name, slug, markdown FROM metadata WHERE project_name=?", projectName)
	err = row.Scan(&m.Captions, &m.HumanName, &m.Description.Slug, &m.Description.Text)
	return
}

/*
Creates a metadata object associated with a project.

db is the database object.

Returns an error if either the object validation or the database write fails.
*/
func (m Metadata) Create(tx *sql.Tx, projectName string) error {
	// if err := Validate(m); err != nil {
	// return err
	// }

	captions, err := json.Marshal(m.Captions)
	if err != nil {
		return err
	}

	_, err = tx.Exec(
		"INSERT INTO metadata (project_name, captions, human_name, slug, markdown) VALUES (?, ?, ?, ?, ?)",
		projectName,
		string(captions),
		m.HumanName,
		m.Description.Slug,
		m.Description.Text,
	)
	if err != nil {
		return err
	}

	return nil
}

/*
Updates the captions, human_name and markdown fields of a metadata object associated with a project, with the fields of the object currently.

db is the database object.

Returns an error if either the object validation or the database write fails.
*/
func (m Metadata) Update(tx *sql.Tx, projectName string) error {
	captions, err := json.Marshal(m.Captions)
	if err != nil {
		return err
	}

	_, err = tx.Exec(
		"UPDATE metadata SET captions = ?, human_name = ?, markdown = ? WHERE project_name = ?",
		string(captions),
		m.HumanName,
		m.Description.Text,
		projectName,
	)
	if err != nil {
		return err
	}

	return nil
}

/*
	Asset Methods
*/

/*
Creates and returns an s3 Client.

Returns an error if any step of the process fails.
*/
func CreateS3Client() (*s3.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(cfg)
	return client, nil
}

/*
 * Checks if a file exists at the location specified by name, in the morpho-images bucket.
 *
 * This method relies on a bucket being mounted through mountpoint-S3, with read and write permissions.
 *
 * Returns true if the file exists, else returns false.
 */
func CheckAssetExistenceS3(service Service, name string) bool {
	_, err := os.Stat(path.Join(service.S3_IMAGES, name))
	return err == nil
}

/*
 * Uploads asset to S3, through a mounted S3 bucket folder.
 *
 * service is the Server's Service object
 * file is an io.Reader object
 * name is the path of the new file within the bucket.
 *
 * This method relies on a bucket being mounted through mountpoint-S3, with read and write permissions.
 *
 * Returns an error if the new file can't be opened or written to.
 */
func UploadAssetMountpointS3(service Service, file io.Reader, name string) error {
	// the flags provided are the right incantations needed to perform a write on a new file
	handle, err := os.OpenFile(path.Join(service.S3_IMAGES, name), os.O_CREATE|os.O_TRUNC|os.O_EXCL|os.O_RDWR, 0644)
	if err != nil {
		return NewServerError(err)
	}
	defer handle.Close()

	if _, err = io.Copy(handle, file); err != nil {
		return NewServerError(err)
	}

	return nil
}

/*
Uploads assets to S3.

file is a ReadCloser interface.
name is the name the file must be saved under (without the extension).
ext is the extension of the file. Leave it empty for auto-detection.

This method does not use any AWS credentials, and relies on the instance having IAM policies that allow GET/PUT/DELETE on the morpho-images bucket.

Returns an error if any step of the process fails, and the file's extension.
*/
func uploadAssetS3(file io.ReadCloser, name, ext string) (string, error) {
	contents, err := io.ReadAll(file)
	if err != nil {
		return "", NewServerError(err)
	}

	mime := mimetype.Detect(contents)
	if ext == "" {
		ext = mime.Extension()
	}

	stream := bytes.NewReader(contents) // need a seekable buffer for s3 upload

	client, err := CreateS3Client()
	if err != nil {
		return "", NewServerError(err)
	}

	_, err = client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:        aws.String("morpho-images"),
		Key:           aws.String(path.Join("assets", name+ext)),
		Body:          stream,
		ContentType:   aws.String(mime.String()),
		ContentLength: aws.Int64(int64(len(contents))),
	})

	if err != nil {
		return mime.Extension(), NewServerError(err)
	} else {
		return mime.Extension(), nil
	}
}

/*
Uploads assets to a local folder. To be used for testing.

fileHeader is a file fetched from the multipart post form.
name is the name of the file on disk.

The problem with this method and the one above, is that there is no
mimetype checking really. While we could restrict this, it would make the tool
very unusable without a list of acceptable mimetypes.

Considering I'll be gone in a few months, it sounds like a bad idea to make users
specify MIME types to just upload a project. While I could allow some leeway
(allowing any image/x file on image/png, for example), that is time consuming
and I'm on a time crunch.

Hence,
TODO: Restrict MIME types sensibly.

Returns an error if any step of the process fails, and the file's extension.
*/
func uploadAssetsLocal(file io.ReadCloser, name, ext string) (string, error) {
	contents, err := io.ReadAll(file)
	if err != nil {
		return "", NewServerError(err)
	}

	mime := mimetype.Detect(contents)
	if ext == "" {
		ext = mime.Extension()
	}

	if writeHandle, err := os.OpenFile(fmt.Sprintf("assets/%s%s", name, ext), os.O_CREATE|os.O_RDWR, 0644); err == nil {
		n, err := writeHandle.Write(contents)
		if n != len(contents) || err != nil {
			return "", fmt.Errorf("could not write complete file: " + err.Error())
		}
		return ext, nil
	} else {
		return "", NewServerError(err)
	}
}

/*
Generates a random string with alphabetical characters.

length is the length of the string generated.
*/
func randString(length int) string {
	rand.New(rand.NewSource(time.Now().Unix()))
	randPart := []byte{}
	for range length {
		if rand.Intn(2) == 0 {
			randPart = append(randPart, byte(rand.Int31()%26+97))
		} else {
			randPart = append(randPart, byte(rand.Int31()%26+65))

		}
	}
	return string(randPart)
}

func (a *Asset) Create(tx *sql.Tx, solutionId string) error {
	auuid, err := uuid.NewRandom()
	if err != nil {
		return err
	}

	_, err = tx.Exec("INSERT INTO asset (id, file, tag, solution_id) VALUES (?, ?, ?, ?)", auuid, a.File, a.Tag, solutionId)
	if err != nil {
		return err
	}

	return nil
}

type Openable interface {
	OpenFile() (io.ReadCloser, error)
}

type OpenableZipFile zip.File

func (o *OpenableZipFile) OpenFile() (io.ReadCloser, error) {
	return (*zip.File)(o).Open()
}

type OpenableMultipartFile multipart.FileHeader

func (o *OpenableMultipartFile) OpenFile() (io.ReadCloser, error) {
	return (*multipart.FileHeader)(o).Open()
}

/*
Uploads the provided assets to S3 or the local filesystem and adds records into the database.

db is a database object.

filetags is a ProjectAssetField list which specifies the tags allowed.

solutionId specifies which solution the assets are going to be associated with

files is either a map of compressed zip files, or a map of multipart form files (each map key contains only uploaded file.)

Returns an error if any step of the process fails.
*/
func CreateAssets(tx *sql.Tx, filetags []ProjectAssetField, solution Solution, projectName string, files map[string]Openable) error {
	// Uploads a file associated with an asset to the storage bucket.

	tagMap := make(map[string]bool)
	for _, fileAsset := range filetags {
		tagMap[fileAsset.Tag] = true
	}

	// check if all the provided tags in the form exist. Terminate otherwise.
	for tag := range files {
		if _, ok := tagMap[tag]; !ok {
			delete(files, tag)
		}
	}

	for tag, handle := range files {
		auuid, err := uuid.NewRandom()
		if err != nil {
			return err
		}

		name := path.Join(projectName, tag, solution.ScopedId)

		config, err := StartService()
		if err != nil {
			return err
		}

		file, err := handle.OpenFile()
		if err != nil {
			return err
		}

		var extension string
		if config.ENVIRONMENT == "prod" {
			extension, err = uploadAssetS3(file, name, "")
			if err != nil {
				return err
			}
		} else {
			extension, err = uploadAssetsLocal(file, name, "")
			if err != nil {
				return err
			}
		}

		err = file.Close()
		if err != nil {
			return err
		}

		if _, err = tx.Exec(
			"INSERT INTO asset (id, file, tag, solution_id) VALUES (?, ?, ?, ?)",
			auuid.String(),
			path.Join("assets", name+extension),
			tag,
			solution.Id,
		); err != nil {
			return err
		}
	}

	return nil
}

/*
Document Methods
*/

// Gets all documents.
//
// Returns a slice of all the documents on the database.
// Returns an error if there is a transaction error.
func GetAllDocuments(tx *sql.Tx) (doc []Document, err error) {
	documents := make([]Document, 0)
	rows, err := tx.Query("SELECT id, slug, text, title, parent, timestamp FROM document")
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var tempDoc Document
		err = rows.Scan(&tempDoc.Id, &tempDoc.Slug, &tempDoc.Text, &tempDoc.Title, &tempDoc.Parent, &tempDoc.Timestamp)
		if err != nil {
			return nil, err
		}
		documents = append(documents, tempDoc)
	}
	return documents, nil
}

// Try to get a document by id. If that fails, try to get it by its slug.
//
// Return error if no Document is found. Else, return a Document.
func GetDocument(tx *sql.Tx, idOrSlug string) (doc Document, err error) {
	// first try
	row := tx.QueryRow("SELECT id, slug, text, title, parent, timestamp FROM document WHERE id=?", idOrSlug)
	err = row.Scan(&doc.Id, &doc.Slug, &doc.Text, &doc.Title, &doc.Parent, &doc.Timestamp)
	if err != nil {
		// second try
		row := tx.QueryRow("SELECT id, slug, text, title, parent, timestamp FROM document WHERE slug=?", idOrSlug)
		err = row.Scan(&doc.Id, &doc.Slug, &doc.Text, &doc.Title, &doc.Parent, &doc.Timestamp)
	}
	return
}

func (doc *Document) Create(tx *sql.Tx, slug, content, title, parent string) error {
	auuid, err := uuid.NewRandom()
	if err != nil {
		return err
	}

	_, err = tx.Exec("INSERT INTO document(id, slug, text, title, parent, timestamp) VALUES (?, ?, ?, ?, ?, date('now'))",
		auuid,
		slug,
		content,
		title,
		parent,
	)
	if err != nil {
		return err
	}

	return nil
}

func (doc *Document) Update(tx *sql.Tx, content, title, parent string) error {
	_, err := tx.Exec("UPDATE document SET text = ?, title = ?, parent = ? WHERE id = ?", content, title, parent, doc.Id)
	if err != nil {
		return err
	}

	return nil
}

func (doc *Document) Delete(tx *sql.Tx) error {
	_, err := tx.Exec("DELETE FROM document WHERE id = ?", doc.Id)
	if err != nil {
		return err
	}
	return nil
}
